package main

/*
#include <stdlib.h>
#include <stdint.h>

typedef void (*WakuPublishFn)(const char* topic, const char* payload);
typedef const char* (*CodexUploadFn)(const char* data, int len);
typedef const char* (*CodexDownloadFn)(const char* cid);

static void callWakuPublish(WakuPublishFn fn, const char* topic, const char* payload) {
	fn(topic, payload);
}
static const char* callCodexUpload(CodexUploadFn fn, const char* data, int len) {
	return fn(data, len);
}
static const char* callCodexDownload(CodexDownloadFn fn, const char* cid) {
	return fn(cid);
}
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"unsafe"

	relay "github.com/metalabel/dfos/packages/dfos-web-relay-go"
)

var (
	gWakuPublish   C.WakuPublishFn
	gCodexUpload   C.CodexUploadFn
	gCodexDownload C.CodexDownloadFn
	gAgent         *Agent
)

//export dfos_init
func dfos_init(dataDir *C.char, wakuFn C.WakuPublishFn, uploadFn C.CodexUploadFn, downloadFn C.CodexDownloadFn) *C.char {
	gWakuPublish = wakuFn
	gCodexUpload = uploadFn
	gCodexDownload = downloadFn

	agent, err := newAgent(C.GoString(dataDir))
	if err != nil {
		return C.CString(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	gAgent = agent
	return nil
}

//export dfos_create_identity
func dfos_create_identity() *C.char {
	if gAgent == nil {
		return C.CString(`{"error":"not initialized"}`)
	}
	did, err := gAgent.createIdentity()
	if err != nil {
		return C.CString(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return C.CString(fmt.Sprintf(`{"did":%q}`, did))
}

//export dfos_get_identity
func dfos_get_identity() *C.char {
	if gAgent == nil || gAgent.did == "" {
		return C.CString(`{"did":null}`)
	}
	return C.CString(fmt.Sprintf(`{"did":%q}`, gAgent.did))
}

//export dfos_publish_post
func dfos_publish_post(text *C.char) *C.char {
	if gAgent == nil {
		return C.CString(`{"error":"not initialized"}`)
	}
	contentID, err := gAgent.publishPost(C.GoString(text))
	if err != nil {
		return C.CString(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return C.CString(fmt.Sprintf(`{"contentId":%q}`, contentID))
}

//export dfos_get_feed
func dfos_get_feed(limit C.int) *C.char {
	if gAgent == nil {
		return C.CString(`[]`)
	}
	feed, err := gAgent.getFeed(int(limit))
	if err != nil {
		return C.CString(`[]`)
	}
	data, err := json.Marshal(feed)
	if err != nil {
		return C.CString(`[]`)
	}
	return C.CString(string(data))
}

//export dfos_ingest_operation
func dfos_ingest_operation(jws *C.char) {
	if gAgent == nil {
		return
	}
	gAgent.relay.Ingest([]string{C.GoString(jws)})
}

// dfos_get_blob_for_content returns the blob for a published content item.
// Result JSON: {"docCID":"...","creatorDID":"...","data":"<raw utf-8 blob>"}
// Returns nil when the content or its blob cannot be found.
//
//export dfos_get_blob_for_content
func dfos_get_blob_for_content(contentID *C.char) *C.char {
	if gAgent == nil {
		return nil
	}
	result, err := gAgent.getBlobForContent(C.GoString(contentID))
	if err != nil || result == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil
	}
	return C.CString(string(data))
}

// dfos_put_blob_for_content stores a downloaded blob in the local relay store.
// data must be the raw UTF-8 blob bytes (not base64).
//
//export dfos_put_blob_for_content
func dfos_put_blob_for_content(creatorDID *C.char, docCID *C.char, data *C.char) {
	if gAgent == nil {
		return
	}
	relay_key := relay.BlobKey{
		CreatorDID:  C.GoString(creatorDID),
		DocumentCID: C.GoString(docCID),
	}
	_ = gAgent.store.SQLiteStore.PutBlob(relay_key, []byte(C.GoString(data)))
}

//export dfos_free
func dfos_free(ptr *C.char) {
	C.free(unsafe.Pointer(ptr))
}

func main() {}
