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
	"unsafe"

	relay "github.com/metalabel/dfos/packages/dfos-web-relay-go"
)

const wakuTopic = "/dfos/1/operations/proto"

// WakuPeerClient implements relay.PeerClient using the Waku publish callback.
// It broadcasts new operations to the well-known DFOS content topic.
// Read-back (GetIdentityLog, GetContentLog, GetOperationLog) is not implemented
// because incoming operations arrive via dfos_ingest_operation called by the
// C++ message handler, not by polling.
type WakuPeerClient struct{}

func (w *WakuPeerClient) SubmitOperations(_ string, operations []string) error {
	if gWakuPublish == nil {
		return nil
	}
	cTopic := C.CString(wakuTopic)
	defer C.free(unsafe.Pointer(cTopic))

	for _, op := range operations {
		cPayload := C.CString(op)
		C.callWakuPublishPeer(gWakuPublish, cTopic, cPayload)
		C.free(unsafe.Pointer(cPayload))
	}
	return nil
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
