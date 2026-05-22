// Two-agent end-to-end integration test.
// Simulates the Waku gossip path: Alice publishes a post, the JWS operation
// is forwarded to Bob's relay via dfos_ingest_operation (what the
// delivery_module messageReceived handler does in production).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	relay "github.com/metalabel/dfos/packages/dfos-web-relay-go"
)

// newTestAgent builds an Agent without the C callback layer.
func newTestAgent(t *testing.T, dir string) *Agent {
	t.Helper()
	a, err := newAgent(dir)
	if err != nil {
		t.Fatalf("newAgent(%s): %v", dir, err)
	}
	return a
}

// captureNextOp wraps the relay's PeerClient so we can intercept the JWS
// tokens that gossipOps would push to Waku.
type capturingPeerClient struct {
	ops []string // captured JWS tokens
}

func (c *capturingPeerClient) SubmitOperations(_ string, ops []string) error {
	c.ops = append(c.ops, ops...)
	return nil
}
func (c *capturingPeerClient) GetIdentityLog(_, _, _ string, _ int) (*relay.PeerLogPage, error) {
	return &relay.PeerLogPage{}, nil
}
func (c *capturingPeerClient) GetContentLog(_, _, _ string, _ int) (*relay.PeerLogPage, error) {
	return &relay.PeerLogPage{}, nil
}
func (c *capturingPeerClient) GetOperationLog(_ string, _ string, _ int) (*relay.PeerLogPage, error) {
	return &relay.PeerLogPage{}, nil
}

func newCaptureAgent(t *testing.T, dir string) (*Agent, *capturingPeerClient) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	sqlStore, err := relay.NewSQLiteStore(filepath.Join(dir, "dfos.db"))
	if err != nil {
		t.Fatalf("SQLiteStore: %v", err)
	}
	store := &BridgeStore{SQLiteStore: sqlStore}

	cap := &capturingPeerClient{}
	falseBool := false
	peers := []relay.PeerConfig{
		{URL: "waku://broadcast", ReadThrough: &falseBool, Sync: &falseBool},
	}
	r, err := relay.NewRelay(relay.RelayOptions{
		Store:      store,
		PeerClient: cap,
		Peers:      peers,
	})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	a := &Agent{relay: r, store: store, dataDir: dir}
	return a, cap
}

func TestTwoAgentGossip(t *testing.T) {
	base := t.TempDir()
	aliceDir := filepath.Join(base, "alice")
	bobDir := filepath.Join(base, "bob")

	// ── Alice setup ───────────────────────────────────────────────────────────
	alice, aliceCap := newCaptureAgent(t, aliceDir)

	aliceDID, err := alice.createIdentity()
	if err != nil {
		t.Fatalf("alice.createIdentity: %v", err)
	}
	t.Logf("Alice DID: %s", aliceDID)

	alicePost, err := alice.publishPost("Hello Bob, from Alice over DFOS!")
	if err != nil {
		t.Fatalf("alice.publishPost: %v", err)
	}
	t.Logf("Alice content ID: %s", alicePost)

	// Verify the gossip captured Alice's identity + content operations.
	if len(aliceCap.ops) == 0 {
		t.Fatal("expected captured gossip operations, got none")
	}
	t.Logf("Alice gossipped %d operation(s)", len(aliceCap.ops))

	// ── Bob setup ─────────────────────────────────────────────────────────────
	bob, _ := newCaptureAgent(t, bobDir)

	bobDID, err := bob.createIdentity()
	if err != nil {
		t.Fatalf("bob.createIdentity: %v", err)
	}
	t.Logf("Bob   DID: %s", bobDID)

	// ── Gossip Alice's ops into Bob (simulates delivery_module messageReceived) ─
	t.Logf("Feeding %d op(s) from Alice to Bob ...", len(aliceCap.ops))
	results := bob.relay.Ingest(aliceCap.ops)
	for i, r := range results {
		if r.Status == "rejected" {
			t.Errorf("Bob Ingest op[%d] rejected: %s", i, r.Error)
		} else {
			t.Logf("Bob Ingest op[%d]: %s", i, r.Status)
		}
	}

	// Bob also needs Alice's blobs (in production these arrive via Codex or
	// the content plane; for the test we copy them from Alice's store).
	aliceFeed, err := alice.getFeed(10)
	if err != nil {
		t.Fatalf("alice.getFeed: %v", err)
	}
	for _, post := range aliceFeed {
		if post.CreatorDID != aliceDID {
			continue
		}
		// Fetch blob from Alice and store in Bob.
		blobKey := relay.BlobKey{CreatorDID: aliceDID, DocumentCID: post.ContentID}
		// ContentID is the chain ID, not the documentCID — we need to walk the feed result.
		// Feed post.ContentID is the contentChain ID; we need the documentCID.
		// Re-read from Alice's store by scanning her chains.
		aliceChains, _ := alice.store.ListContentChains()
		for _, chain := range aliceChains {
			if chain.ContentID == post.ContentID && chain.State.CurrentDocumentCID != nil {
				docCID := *chain.State.CurrentDocumentCID
				blob, bErr := alice.store.GetBlob(relay.BlobKey{
					CreatorDID:  aliceDID,
					DocumentCID: docCID,
				})
				if bErr == nil && len(blob) > 0 {
					bob.store.PutBlob(relay.BlobKey{
						CreatorDID:  aliceDID,
						DocumentCID: docCID,
					}, blob)
					t.Logf("Copied blob docCID=%s (%d bytes) from Alice to Bob", docCID, len(blob))
					_ = blobKey
				}
			}
		}
	}

	// ── Verify Bob's feed contains Alice's post ───────────────────────────────
	bobFeed, err := bob.getFeed(20)
	if err != nil {
		t.Fatalf("bob.getFeed: %v", err)
	}

	var found bool
	for _, post := range bobFeed {
		raw, _ := json.Marshal(post)
		t.Logf("Bob sees post: %s", raw)
		if post.CreatorDID == aliceDID {
			found = true
		}
	}

	if !found {
		t.Errorf("Bob's feed does not contain Alice's post (expected creatorDID=%s)", aliceDID)
	} else {
		fmt.Printf("\n✓ Bob's feed contains Alice's post (DID %s)\n", aliceDID)
	}

	// Sanity: Bob's own posts are separate.
	bobPost, err := bob.publishPost("Hello Alice, I got your message!")
	if err != nil {
		t.Fatalf("bob.publishPost: %v", err)
	}
	t.Logf("Bob content ID: %s", bobPost)

	bobFeed2, _ := bob.getFeed(20)
	var bobPostCount int
	for _, p := range bobFeed2 {
		if p.CreatorDID == bobDID {
			bobPostCount++
		}
	}
	if bobPostCount == 0 {
		t.Error("Bob's feed doesn't contain his own post")
	}
	t.Logf("Bob's feed total=%d alice=%d bob=%d",
		len(bobFeed2),
		func() int {
			n := 0
			for _, p := range bobFeed2 {
				if p.CreatorDID == aliceDID {
					n++
				}
			}
			return n
		}(),
		bobPostCount,
	)
}
