package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	dfos "github.com/metalabel/dfos/packages/dfos-protocol-go"
	relay "github.com/metalabel/dfos/packages/dfos-web-relay-go"
)

// FeedPost is a single post in the feed response.
type FeedPost struct {
	ContentID  string `json:"contentId"`
	CreatorDID string `json:"creatorDID"`
	Text       string `json:"text"`
	CreatedAt  string `json:"createdAt"`
}

// storageCIDEntry maps a DFOS documentCID to its storage_module CID.
type storageCIDEntry struct {
	StorageCID string
	CreatorDID string
}

// Agent holds all per-instance state: relay, keys, identity.
type Agent struct {
	relay      *relay.Relay
	store      *BridgeStore
	dataDir    string
	priv       ed25519.PrivateKey
	did        string
	keyID      string
	storageMu  sync.RWMutex
	storageCIDs map[string]storageCIDEntry // docCID → {storageCID, creatorDID}
}

// keyFile is persisted to disk to survive restarts.
type keyFile struct {
	Seed  []byte `json:"seed"` // ed25519 seed (32 bytes)
	DID   string `json:"did"`
	KeyID string `json:"keyID"`
}

func newAgent(dataDir string) (*Agent, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	sqlStore, err := relay.NewSQLiteStore(filepath.Join(dataDir, "dfos.db"))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	store := &BridgeStore{SQLiteStore: sqlStore}

	// A single dummy peer entry so gossipOps fires; WakuPeerClient ignores the URL.
	falseBool := false
	peers := []relay.PeerConfig{
		{URL: "waku://broadcast", ReadThrough: &falseBool, Sync: &falseBool},
	}

	wakuClient := &WakuPeerClient{}
	r, err := relay.NewRelay(relay.RelayOptions{
		Store:      store,
		PeerClient: wakuClient,
		Peers:      peers,
	})
	if err != nil {
		return nil, fmt.Errorf("create relay: %w", err)
	}

	agent := &Agent{relay: r, store: store, dataDir: dataDir}
	wakuClient.startRetryLoop()

	// Load persisted key if present.
	keyPath := filepath.Join(dataDir, "key.json")
	if raw, err := os.ReadFile(keyPath); err == nil {
		var kf keyFile
		if json.Unmarshal(raw, &kf) == nil && len(kf.Seed) == ed25519.SeedSize {
			agent.priv = ed25519.NewKeyFromSeed(kf.Seed)
			agent.did = kf.DID
			agent.keyID = kf.KeyID
		}
	}

	return agent, nil
}

func (a *Agent) createIdentity() (string, error) {
	if a.did != "" {
		return a.did, nil
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}

	keyID := "key-0"
	mk := dfos.NewMultikeyPublicKey(keyID, pub)

	jwsToken, did, _, err := dfos.SignIdentityCreate(
		[]dfos.MultikeyPublicKey{mk}, // controllerKeys
		[]dfos.MultikeyPublicKey{mk}, // authKeys
		[]dfos.MultikeyPublicKey{mk}, // assertKeys
		keyID,
		priv,
	)
	if err != nil {
		return "", fmt.Errorf("sign identity create: %w", err)
	}

	results := a.relay.Ingest([]string{jwsToken})
	if len(results) == 0 || results[0].Status == "rejected" {
		msg := "unknown"
		if len(results) > 0 {
			msg = results[0].Error
		}
		return "", fmt.Errorf("ingest identity: %s", msg)
	}

	a.priv = priv
	a.did = did
	a.keyID = keyID

	// Persist key material.
	kf := keyFile{
		Seed:  priv.Seed(),
		DID:   did,
		KeyID: keyID,
	}
	raw, _ := json.Marshal(kf)
	// Ignore write error — identity is already in SQLite.
	_ = os.WriteFile(filepath.Join(a.dataDir, "key.json"), raw, 0600)

	return did, nil
}

func (a *Agent) publishPost(text string) (string, error) {
	if a.did == "" {
		return "", fmt.Errorf("no identity; call dfos_create_identity first")
	}

	doc := map[string]any{
		"type":      "post/v1",
		"text":      text,
		"createdAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	}

	// Compute the dag-cbor CID (the DFOS documentCID), but store JSON bytes
	// as the blob because the feed reader uses json.Unmarshal.
	docCID, cborBytes, err := dfos.DocumentCID(doc)
	if err != nil {
		return "", fmt.Errorf("document CID: %w", err)
	}
	jsonBytes, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal doc: %w", err)
	}

	// Upload blob to Codex; BridgeStore.PutBlob is called later, but we need
	// the DFOS CID for the content chain operation first.
	if gCodexUpload != nil {
		a.store.preUpload(docCID, cborBytes)
	}

	// kid format for a non-genesis op: "did:dfos:xxx#key-0"
	kid := a.did + "#" + a.keyID

	jwsToken, contentID, _, err := dfos.SignContentCreate(a.did, docCID, kid, "", a.priv)
	if err != nil {
		return "", fmt.Errorf("sign content create: %w", err)
	}

	// Ingest locally; gossip fires automatically via WakuPeerClient.
	results := a.relay.Ingest([]string{jwsToken})
	if len(results) == 0 || results[0].Status == "rejected" {
		msg := "unknown"
		if len(results) > 0 {
			msg = results[0].Error
		}
		return "", fmt.Errorf("ingest content: %s", msg)
	}

	// Store JSON blob (not CBOR) so GetBlob can be deserialized by feed readers.
	blobData := jsonBytes
	if gCodexUpload != nil {
		blobData = cborBytes // Codex flow uses CBOR bytes for hashing integrity
	}
	a.store.PutBlob(relay.BlobKey{CreatorDID: a.did, DocumentCID: docCID}, blobData)

	return contentID, nil
}

// BlobInfo is returned by getBlobForContent and passed to the C++ storage bridge.
type BlobInfo struct {
	DocCID     string `json:"docCID"`
	CreatorDID string `json:"creatorDID"`
	Data       string `json:"data"` // raw UTF-8 blob bytes
}

func (a *Agent) getBlobForContent(contentID string) (*BlobInfo, error) {
	chains, err := a.store.ListContentChains()
	if err != nil {
		return nil, err
	}
	for _, chain := range chains {
		if chain.ContentID != contentID {
			continue
		}
		if chain.State.CurrentDocumentCID == nil {
			return nil, nil
		}
		docCID := *chain.State.CurrentDocumentCID
		blob, err := a.store.GetBlob(relay.BlobKey{
			CreatorDID:  chain.State.CreatorDID,
			DocumentCID: docCID,
		})
		if err != nil || len(blob) == 0 {
			return nil, err
		}
		return &BlobInfo{
			DocCID:     docCID,
			CreatorDID: chain.State.CreatorDID,
			Data:       string(blob),
		}, nil
	}
	return nil, nil
}

func (a *Agent) getFeed(limit int) ([]FeedPost, error) {
	chains, err := a.store.ListContentChains()
	if err != nil {
		return nil, err
	}

	var posts []FeedPost
	for _, chain := range chains {
		if chain.State.IsDeleted || chain.State.CurrentDocumentCID == nil {
			continue
		}
		docCID := *chain.State.CurrentDocumentCID
		blob, err := a.store.GetBlob(relay.BlobKey{
			CreatorDID:  chain.State.CreatorDID,
			DocumentCID: docCID,
		})
		if err != nil || len(blob) == 0 {
			continue
		}

		var doc map[string]any
		if err := json.Unmarshal(blob, &doc); err != nil {
			continue
		}
		docType, _ := doc["type"].(string)
		if docType != "post/v1" {
			continue
		}
		text, _ := doc["text"].(string)
		createdAt, _ := doc["createdAt"].(string)

		posts = append(posts, FeedPost{
			ContentID:  chain.ContentID,
			CreatorDID: chain.State.CreatorDID,
			Text:       text,
			CreatedAt:  createdAt,
		})

		if limit > 0 && len(posts) >= limit {
			break
		}
	}

	if posts == nil {
		posts = []FeedPost{}
	}
	return posts, nil
}

func (a *Agent) setStorageCID(docCID, storageCID, creatorDID string) {
	a.storageMu.Lock()
	defer a.storageMu.Unlock()
	if a.storageCIDs == nil {
		a.storageCIDs = make(map[string]storageCIDEntry)
	}
	a.storageCIDs[docCID] = storageCIDEntry{StorageCID: storageCID, CreatorDID: creatorDID}
}

func (a *Agent) getStorageCID(docCID string) *storageCIDEntry {
	a.storageMu.RLock()
	defer a.storageMu.RUnlock()
	if a.storageCIDs == nil {
		return nil
	}
	e, ok := a.storageCIDs[docCID]
	if !ok {
		return nil
	}
	return &e
}
