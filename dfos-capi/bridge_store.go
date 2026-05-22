package main

/*
#include <stdlib.h>
typedef const char* (*CodexUploadFn)(const char* data, int len);
typedef const char* (*CodexDownloadFn)(const char* cid);

static const char* callCodexUploadBridge(CodexUploadFn fn, const char* data, int len) {
	return fn(data, len);
}
static const char* callCodexDownloadBridge(CodexDownloadFn fn, const char* cid) {
	return fn(cid);
}
*/
import "C"
import (
	"sync"
	"unsafe"

	relay "github.com/metalabel/dfos/packages/dfos-web-relay-go"
)

// BridgeStore embeds SQLiteStore and overrides PutBlob/GetBlob to use Codex.
// When the Codex callbacks are nil (pre-init), it falls back to SQLite storage.
type BridgeStore struct {
	*relay.SQLiteStore

	// Maps DFOS documentCID → Codex CID for blobs pre-uploaded before ingest.
	mu      sync.RWMutex
	cidMap  map[string]string // docCID → codexCID
}

func (b *BridgeStore) initCIDMap() {
	b.mu.Lock()
	if b.cidMap == nil {
		b.cidMap = make(map[string]string)
	}
	b.mu.Unlock()
}

// preUpload uploads a blob to Codex and caches the resulting Codex CID so
// that PutBlob (called later during ingest) stores the reference correctly.
func (b *BridgeStore) preUpload(docCID string, data []byte) {
	if gCodexUpload == nil || len(data) == 0 {
		return
	}
	b.initCIDMap()

	cData := C.CBytes(data)
	defer C.free(cData)
	cCID := C.callCodexUploadBridge(gCodexUpload, (*C.char)(cData), C.int(len(data)))
	if cCID == nil {
		return
	}
	codexCID := C.GoString(cCID)
	// The C++ callback owns the returned string — do not free it here.

	b.mu.Lock()
	b.cidMap[docCID] = codexCID
	b.mu.Unlock()
}

// PutBlob stores the Codex CID in SQLite instead of the raw blob.
// If Codex upload hasn't been pre-done, it uploads now.
func (b *BridgeStore) PutBlob(key relay.BlobKey, data []byte) error {
	if gCodexUpload == nil {
		return b.SQLiteStore.PutBlob(key, data)
	}

	b.initCIDMap()

	b.mu.RLock()
	codexCID, cached := b.cidMap[key.DocumentCID]
	b.mu.RUnlock()

	if !cached {
		// Upload now and cache the result.
		b.preUpload(key.DocumentCID, data)
		b.mu.RLock()
		codexCID = b.cidMap[key.DocumentCID]
		b.mu.RUnlock()
	}

	if codexCID == "" {
		// Codex upload failed; fall back to local SQLite.
		return b.SQLiteStore.PutBlob(key, data)
	}

	// Store the Codex CID bytes so GetBlob can retrieve it.
	return b.SQLiteStore.PutBlob(key, []byte(codexCID))
}

// GetBlob retrieves the blob from Codex, using the stored Codex CID as the key.
func (b *BridgeStore) GetBlob(key relay.BlobKey) ([]byte, error) {
	stored, err := b.SQLiteStore.GetBlob(key)
	if err != nil || len(stored) == 0 {
		return stored, err
	}

	if gCodexDownload == nil {
		return stored, nil
	}

	// stored is either raw data (Codex disabled) or a Codex CID string.
	// Heuristic: Codex CIDs start with "bafy" (CIDv1 base32). If stored
	// data starts with that prefix, treat it as a CID and download.
	if len(stored) > 4 && string(stored[:4]) == "bafy" {
		cCID := C.CString(string(stored))
		defer C.free(unsafe.Pointer(cCID))
		cData := C.callCodexDownloadBridge(gCodexDownload, cCID)
		if cData == nil {
			return nil, nil
		}
		// The C++ callback owns the returned memory; copy it.
		goData := []byte(C.GoString(cData))
		return goData, nil
	}

	return stored, nil
}
