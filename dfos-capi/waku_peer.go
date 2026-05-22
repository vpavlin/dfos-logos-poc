package main

/*
#include <stdlib.h>
typedef void (*WakuPublishFn)(const char* topic, const char* payload);

static void callWakuPublishPeer(WakuPublishFn fn, const char* topic, const char* payload) {
	fn(topic, payload);
}
*/
import "C"
import (
	"sync"
	"time"
	"unsafe"

	relay "github.com/metalabel/dfos/packages/dfos-web-relay-go"
)

const wakuTopic = "/dfos/1/operations/proto"

// maxPendingOps caps the ops buffer to the most recent N operations.
const maxPendingOps = 50

// WakuPeerClient implements relay.PeerClient using the Waku publish callback.
// It broadcasts new operations to the well-known DFOS content topic.
//
// Because the gossipsub mesh may not be established at publish time (the
// heartbeat fires ~1 s after peer connection), SubmitOperations buffers ops
// and a retry goroutine re-broadcasts them every 5 s.  Bob's relay
// deduplicates repeated ops (status = "duplicate"), so retrying is safe.
type WakuPeerClient struct {
	mu         sync.Mutex
	pendingOps []string
}

func (w *WakuPeerClient) SubmitOperations(_ string, operations []string) error {
	// Buffer for retry and publish immediately.
	w.mu.Lock()
	w.pendingOps = append(w.pendingOps, operations...)
	if len(w.pendingOps) > maxPendingOps {
		w.pendingOps = w.pendingOps[len(w.pendingOps)-maxPendingOps:]
	}
	w.mu.Unlock()

	w.publish(operations)
	return nil
}

func (w *WakuPeerClient) publish(ops []string) {
	if gWakuPublish == nil || len(ops) == 0 {
		return
	}
	cTopic := C.CString(wakuTopic)
	defer C.free(unsafe.Pointer(cTopic))
	for _, op := range ops {
		cPayload := C.CString(op)
		C.callWakuPublishPeer(gWakuPublish, cTopic, cPayload)
		C.free(unsafe.Pointer(cPayload))
	}
}

// startRetryLoop re-publishes buffered ops every 5 s so that ops sent before
// the gossipsub mesh formed still reach connected peers.
func (w *WakuPeerClient) startRetryLoop() {
	go func() {
		// Give gossipsub time to form the mesh before the first retry.
		time.Sleep(3 * time.Second)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			w.mu.Lock()
			ops := make([]string, len(w.pendingOps))
			copy(ops, w.pendingOps)
			w.mu.Unlock()
			w.publish(ops)
		}
	}()
}

func (w *WakuPeerClient) GetIdentityLog(_, _ string, _ string, _ int) (*relay.PeerLogPage, error) {
	return &relay.PeerLogPage{}, nil
}

func (w *WakuPeerClient) GetContentLog(_, _ string, _ string, _ int) (*relay.PeerLogPage, error) {
	return &relay.PeerLogPage{}, nil
}

func (w *WakuPeerClient) GetOperationLog(_ string, _ string, _ int) (*relay.PeerLogPage, error) {
	return &relay.PeerLogPage{}, nil
}
