# DFOS + Logos Integration: POC Plan

## Goal

Demonstrate a working DFOS identity and content system running as a native Logos Basecamp module, using `delivery_module` (Waku) for proof plane gossip and `storage_module` (Codex) for content blob storage — all via standard Logos inter-module communication.

---

## Reusing the Existing DFOS Go Implementation

The DFOS monorepo (`github.com/metalabel/dfos`) contains two complete Go packages:

- **`dfos-protocol-go`** — full protocol library: Ed25519 signing, dag-cbor canonical encoding, CID derivation, identity chain verification, content chain verification, credentials, revocations
- **`dfos-web-relay-go`** — full relay: HTTP API, chain state management, SQLite and in-memory stores, peering, blob upload/download

**We do not reimplement any of this.** The Go packages are compiled as a C shared library (`go build -buildmode=c-shared`) and linked directly into the C++ Basecamp module — no subprocess, no bridge server, no IPC.

The two integration seams are wired via C function pointer callbacks passed from C++ into Go at initialisation:

```c
// Exported by the Go shared library
void dfos_init(WakuPublishFn waku_publish,
               CodexUploadFn codex_upload,
               CodexDownloadFn codex_download);

char* dfos_create_identity();
char* dfos_get_identity();
char* dfos_publish_post(const char* text);
char* dfos_get_feed(int limit);
void  dfos_ingest_operation(const char* jws_token);
void  dfos_free(char* ptr);
```

Go calls `waku_publish` when gossiping a signed operation. Go calls `codex_upload`/`codex_download` when storing or retrieving content blobs. C++ implements those callbacks using `delivery_module` and `storage_module` callModule IPC.

---

## DFOS Proof Plane is Permissionless

DFOS proof plane operations are self-authenticating — every operation is Ed25519 signed and CID-verified, so any receiver can verify independently without trusting the sender. All proof plane routes are unauthenticated by protocol design.

This means gossip is simple: broadcast signed operations to a well-known Waku content topic (`/dfos/1/operations/proto`). Any Basecamp instance subscribed to that topic receives and verifies them locally. No peer configuration, no allowlists, no coordination — Waku's GossipSub handles propagation.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  Basecamp                                                      │
│                                                                │
│  ┌──────────────────────────┐                                 │
│  │   DFOS QML UI Plugin      │  (tab in MDI workspace)        │
│  └────────────┬─────────────┘                                 │
│               │ logos.callModule("dfos_module", ...)           │
│  ┌────────────▼──────────────────────────────────────────┐   │
│  │  DFOS C++ Core Module  (logos_host process)            │   │
│  │                                                        │   │
│  │  links dfos.so  (Go shared library)                    │   │
│  │                                                        │   │
│  │  on init:                                              │   │
│  │    dfos_init(waku_cb, codex_upload_cb,                 │   │
│  │              codex_download_cb)                        │   │
│  │                                                        │   │
│  │  callbacks implemented in C++:                         │   │
│  │    waku_cb         → delivery_module.send()            │   │
│  │    codex_upload_cb → storage_module.upload*()          │   │
│  │    codex_download_cb → storage_module.download*()      │   │
│  │                                                        │   │
│  │  on delivery_module.messageReceived:                   │   │
│  │    dfos_ingest_operation(jws_token)                    │   │
│  │                                                        │   │
│  │  Q_INVOKABLE → direct calls into dfos.so               │   │
│  └──────┬─────────────────┬──────────────────────────────┘   │
│         │ callModule IPC  │ callModule IPC                     │
│  ┌──────▼───────┐  ┌──────▼──────────┐                       │
│  │  delivery    │  │    storage       │                       │
│  │  _module     │  │    _module       │                       │
│  │  (Waku)      │  │    (Codex)       │                       │
│  └──────────────┘  └─────────────────┘                       │
└──────────────────────────────────────────────────────────────┘
```

### Data flow: publishing a post

1. User types text in QML, hits "Publish"
2. QML → `logos.callModule("dfos_module", "publishPost", [text])`
3. C++ → `dfos_publish_post(text)` (direct call into Go shared lib)
4. Go:
   a. Serialises post as `post/v1` JSON blob
   b. Calls `codex_upload_cb(blob)` → C++ → `storage_module.upload*()` → returns Codex CID
   c. Uses Codex CID as `documentCID`, signs content chain operation
   d. Ingests operation into local SQLite chain state
   e. Calls `waku_cb("/dfos/1/operations/proto", jws_token)` → C++ → `delivery_module.send()`
5. Returns content chain ID; C++ returns result to QML

### Data flow: receiving a post

1. `delivery_module` fires `messageReceived(hash, topic, payload_base64, timestamp)`
2. C++ decodes and calls `dfos_ingest_operation(jws_token)` directly into Go
3. Go: verifies Ed25519 + CID integrity, updates SQLite chain state
4. Next `getFeed()` call includes the new post; blob fetched from Codex on demand via callback

---

## Module APIs Used

### `delivery_module` (Waku)

```cpp
bool subscribe(const QString &contentTopic);
QExpected<QString> send(const QString &contentTopic, const QString &payload);
// event: messageReceived(hash, contentTopic, payload_base64, timestamp_ns)
```

Content topic: `/dfos/1/operations/proto`

### `storage_module` (Codex)

```cpp
StdLogosResult uploadInit(filename, chunkSize);       // → sessionId
StdLogosResult uploadChunk(sessionId, chunk_base64);
StdLogosResult uploadFinalize(sessionId);             // → CID
StdLogosResult downloadChunks(cid, local, chunkSize);
// events: storageUploadDone(cid), storageDownloadDone(data)
```

---

## Scope

### In scope
- Reuse `dfos-protocol-go` and `dfos-web-relay-go` via CGo shared library — no DFOS reimplementation
- Software Ed25519 key generation (Go `crypto/ed25519`)
- Identity chain genesis (DID creation)
- Content chain: create + update operations, `post/v1` schema
- Proof plane gossip via `delivery_module` (permissionless broadcast)
- Content blob storage via `storage_module`
- Basecamp module: identity view, compose, feed
- Two-instance local demo

### Out of scope
- Key rotation, identity recovery
- Credential delegation UI
- Following / social graph
- Encrypted content
- Nomos anchoring
- Countersignatures and beacons
- Keycard hardware signing (~2028)

---

## Phases

### Phase 0 — Go Shared Library: CGo Bindings

**Goal:** Build `dfos.so` from `dfos-protocol-go` + `dfos-web-relay-go` with a clean C API and callback-based integration seams for Waku and Codex.

**Tasks:**

1. Add a `capi/` package to wrap the Go code with `//export` directives:

   ```go
   // capi/dfos.go
   package main

   // typedef void (*WakuPublishFn)(const char* topic, const char* payload);
   // typedef const char* (*CodexUploadFn)(const char* data, int len);
   // typedef const char* (*CodexDownloadFn)(const char* cid);
   import "C"
   import (
       dfos "github.com/metalabel/dfos/packages/dfos-protocol-go"
       relay "github.com/metalabel/dfos/packages/dfos-web-relay-go"
       // ...
   )

   //export dfos_init
   func dfos_init(wakuFn C.WakuPublishFn, uploadFn C.CodexUploadFn, downloadFn C.CodexDownloadFn) { ... }

   //export dfos_create_identity
   func dfos_create_identity() *C.char { ... }

   //export dfos_publish_post
   func dfos_publish_post(text *C.char) *C.char { ... }

   //export dfos_get_feed
   func dfos_get_feed(limit C.int) *C.char { ... }

   //export dfos_ingest_operation
   func dfos_ingest_operation(jws *C.char) { ... }

   //export dfos_free
   func dfos_free(ptr *C.char) { C.free(unsafe.Pointer(ptr)) }

   func main() {} // required for c-shared
   ```

2. Implement `WakuStore` (embeds `SqliteStore`, overrides blob methods to use callbacks)
3. Build: `go build -buildmode=c-shared -o dfos.so ./capi/`
4. Verify with a standalone C test that calls all exported functions against stub callbacks

**Deliverable:** `dfos.so` + `dfos.h` that passes a C-level smoke test.

**Key CGo note:** Go callbacks into C (the `WakuPublishFn` etc.) must not block indefinitely and must not call back into Go. The Codex upload callback will be synchronous from Go's perspective — C++ handles the async Codex flow internally before returning the CID.

---

### Phase 1 — C++ Core Module

**Goal:** Basecamp core module that links `dfos.so`, implements the callbacks using `delivery_module` / `storage_module`, and exposes Q_INVOKABLE methods to Basecamp.

**Tasks:**

1. Scaffold `dfos_module` as a Logos core (universal) module:
   - `metadata.json`: deps `[delivery_module, storage_module]`
   - Link `dfos.so` in `CMakeLists.txt`

2. **Callback implementations:**

   ```cpp
   // waku callback — called from Go when gossiping an operation
   static void waku_publish_cb(const char* topic, const char* payload) {
       // delivery_module.send(topic, payload)
   }

   // codex upload — called from Go when storing a blob; returns CID as C string
   static const char* codex_upload_cb(const char* data, int len) {
       // storage_module.uploadInit → uploadChunk → uploadFinalize
       // block on storageUploadDone event, return CID
   }

   // codex download — called from Go when retrieving a blob; returns data
   static const char* codex_download_cb(const char* cid) {
       // storage_module.downloadChunks, block on storageDownloadDone, return data
   }
   ```

3. **Module init:** call `dfos_init(waku_publish_cb, codex_upload_cb, codex_download_cb)`, subscribe to `/dfos/1/operations/proto` via `delivery_module`

4. **Incoming message handler:** connect `delivery_module.messageReceived` signal → decode base64 → `dfos_ingest_operation(jws)`

5. **Q_INVOKABLE methods** — thin wrappers around the Go C API:
   ```cpp
   QString createIdentity()          { return QString(dfos_create_identity()); }
   QString getIdentity()             { return QString(dfos_get_identity()); }
   QString publishPost(QString text) { return QString(dfos_publish_post(text.toUtf8())); }
   QString getFeed(int limit)        { return QString(dfos_get_feed(limit)); }
   ```

6. Smoke test via `logoscore`: `createIdentity()` → DID returned; `publishPost("hello")` → blob arrives in `storage_module`, op arrives in `delivery_module`.

**Deliverable:** Installable `.lgx` (no UI). Full round-trip verified via `logoscore`.

---

### Phase 2 — QML UI

**Goal:** Usable Basecamp tab for identity management, composing posts, and reading a feed.

**Tasks:**

1. Scaffold `dfos_ui` QML plugin targeting `dfos_module`

2. **Identity view:** DID display (truncated + copy), "Create Identity" button (disabled if exists), status row (delivery / storage connected)

3. **Compose view:** `TextArea` with 500-char limit + counter, "Publish" button, inline confirmation with content chain ID

4. **Feed view:** `ListView` of posts (author DID, text, relative timestamp), "Refresh" + auto-refresh every 30s

**Deliverable:** Complete `.lgx` (core + UI). Full user flow works from the Basecamp tab.

---

### Phase 3 — Two-Instance Demo

**Goal:** Two Basecamp instances exchange posts over the Logos network.

**Demo flow:**
1. Instance A: "Create Identity" → `did:dfos:abc...`
2. Instance A: publish "Hello from DFOS on Logos"
3. Instance B: refresh feed → sees the post
4. Instance B: create identity, publish reply
5. Instance A: refresh → sees reply

**Deliverable:** Screen recording + reproduction steps in `README.md`.

---

## Repository Structure

```
dfos-logos-poc/
├── dfos-capi/                    # Phase 0: CGo C API wrapper
│   ├── dfos.go                   # //export functions
│   ├── waku_store.go             # WakuStore (callback-backed blob store)
│   └── go.mod
├── dfos-module/                  # Phase 1: C++ Basecamp core module
│   ├── include/dfos_module.h
│   ├── src/
│   │   ├── dfos_module.cpp       # Q_INVOKABLE wrappers
│   │   └── callbacks.cpp         # waku/codex C callback implementations
│   ├── metadata.json
│   └── CMakeLists.txt
├── dfos-ui/                      # Phase 2: QML UI plugin
│   ├── Main.qml
│   ├── IdentityView.qml
│   ├── ComposeView.qml
│   ├── FeedView.qml
│   └── metadata.json
├── WRITEUP.md
├── POC_PLAN.md
└── README.md
```

---

## Resolved Design Points

1. **`delivery_module` event wiring** — `logosAPI->getClient("delivery_module")` → `requestObject()` → `onEvent(obj, "messageReceived", lambda)`. Event name is `messageReceived`, payload is `data[2]` (base64). `invokeRemoteMethod` for `subscribe` and `send`. Check whether `connectionStateChanged` must be awaited before subscribing.

2. **CGo `.so` linking** — no special "library module" type in Logos. The `logos-calc-module` tutorial shows the pattern: include the C header, link the `.so` in `CMakeLists.txt`, call C functions directly from Q_INVOKABLE methods. Same approach for the Go shared library.

## Remaining Open Questions

1. **`storage_module` sync wrapper:** upload/download are async (event-driven). The CGo callback must return synchronously. Implement a blocking wrapper in C++ (condition variable) that waits for `storageUploadDone` / `storageDownloadDone` before returning the CID/data to Go.

2. **CGo and Qt event loop:** `dfos_publish_post` is called from the Qt thread (via Q_INVOKABLE). Blocking CGo calls (waiting on Codex) must not block the Qt main thread — run Q_INVOKABLEs on a worker thread or use `QtConcurrent::run`.

---

## Success Criteria

- [ ] No DFOS protocol reimplementation — `dfos-protocol-go` and `dfos-web-relay-go` reused via CGo
- [ ] Two Basecamp instances exchange posts via `delivery_module` and `storage_module`
- [ ] Content blobs resolve from Codex by `documentCID`
- [ ] DFOS identity chains verify correctly on both instances
- [ ] No external infrastructure required — everything runs within Basecamp
- [ ] Demo is screencast-able in under 3 minutes
