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

//export dfos_free
func dfos_free(ptr *C.char) {
	C.free(unsafe.Pointer(ptr))
}

func main() {}
