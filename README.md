# DFOS + Logos Basecamp Integration POC

Demonstrates DFOS identity and content-chain protocol running inside
Logos Basecamp as a native core module + optional QML UI.

## Architecture

```
Logos Basecamp (host)
├── delivery_module  ← existing Waku transport (Logos)
├── storage_module   ← existing Codex blob storage (Logos)  [Phase 1.5]
├── dfos_module      ← Phase 1: Go DFOS protocol via CGo shared library
└── dfos_ui          ← Phase 2: Qt/QML feed + compose UI (IComponent)
         │
         ▼
    dfos-capi/dfos.so   ← Phase 0: CGo c-shared library
         │
         ▼
dfos-protocol-go + dfos-web-relay-go  (vendored from metalabel/dfos)
```

**Two planes of propagation:**
- **Proof plane** (JWS operations) — travels via Waku gossip (`delivery_module`). Confirmed working across nodes.
- **Blob plane** (post content) — stored locally; cross-node retrieval requires a `documentCID → storageCID` mapping exchange (future work).

## Phases

### Phase 0 — `dfos-capi` CGo shared library

Wraps `dfos-protocol-go` (identity / content-chain protocol) and
`dfos-web-relay-go` (relay + SQLite store) behind a plain-C ABI:

| Export | Description |
|--------|-------------|
| `dfos_init(dataDir, wakuFn, uploadFn, downloadFn)` | Start relay, wire callbacks |
| `dfos_create_identity()` | Generate Ed25519 key, create DID, persist |
| `dfos_get_identity()` | Return current DID JSON |
| `dfos_publish_post(text)` | Sign + ingest content-create op |
| `dfos_get_feed(limit)` | Return JSON array of posts |
| `dfos_ingest_operation(jws)` | Feed a raw JWS token into the relay |
| `dfos_free(ptr)` | Release a C string returned by the library |

Build:
```bash
cd dfos-capi
make          # produces dfos.so + dfos.h
```

### Phase 1 — `dfos-module` Logos core module

Qt plugin (`PluginInterface`) that wraps `dfos.so`.  Wires the
`delivery_module` `messageReceived` event for incoming Waku gossip, and
calls `delivery_module.send()` via the `wakuPublishCb` static callback
for outgoing gossip.

Build:
```bash
mkdir /tmp/dfos-module-build && cd /tmp/dfos-module-build
cmake \
  -DLOGOS_CPP_SDK_ROOT=.../logos-cpp-sdk \
  -DLOGOS_LIBLOGOS_ROOT=.../logos-liblogos \
  -DCPP_GENERATOR=.../logos-cpp-generator \
  /path/to/dfos-module
make -j$(nproc)
```

Validate (logoscore headless):
```bash
# Write waku config to file — logoscore strips quotes from inline JSON args
echo '{"numShardsInNetwork": 1}' > /tmp/waku-config.json

logoscore --modules-dir /tmp/combined-modules --mode 1 \
  --load-modules delivery_module,dfos_module \
  --call 'delivery_module.createNode(@/tmp/waku-config.json)' \
  --call 'delivery_module.start()' \
  --call 'dfos_module.start(/tmp/dfos-data)' \
  --call 'dfos_module.createIdentity()' \
  --call 'dfos_module.publishPost(Hello from DFOS on Logos!)' \
  --call 'dfos_module.getFeed(10)' \
  --quit-on-finish
```

Expected output (last call):
```json
[{"contentId":"...","creatorDID":"did:dfos:...","text":"Hello from DFOS on Logos!","createdAt":"..."}]
```

### Phase 2 — `dfos-ui` Qt/QML UI plugin

IComponent plugin that hosts a QML feed+compose view backed by
`DfosBackend` which calls `dfos_module` via `LogosAPIClient`.

Build:
```bash
mkdir /tmp/dfos-ui-build && cd /tmp/dfos-ui-build
cmake -DLOGOS_CPP_SDK_ROOT=... -DLOGOS_LIBLOGOS_ROOT=... -DCPP_GENERATOR=... \
  /path/to/dfos-ui
make -j$(nproc)
```

Headless validation:
```bash
QT_QPA_PLATFORM=offscreen ./test_load ./modules/dfos_ui.so
# → PASS: dfos_ui plugin loaded and validated
```

To display in Basecamp: install `dfos_ui.so` + `metadata.json` to
`$LOGOS_DATA_DIR/Dev/plugins/dfos_ui/` and restart Basecamp.

### Phase 1.5 — `storage_module` blob storage integration

Extends `dfos_module` to use Logos `storage_module` (Codex-backed) for
blob storage instead of local-only SQLite.  Two new C exports bridge the
Go relay to the C++ storage pipeline:

| Export | Description |
|--------|-------------|
| `dfos_get_blob_for_content(contentId)` | Returns `{docCID, creatorDID, data}` JSON for upload |
| `dfos_put_blob_for_content(creatorDID, docCID, data)` | Persists a downloaded blob into the local relay store |

The C++ plugin side:
- After `publishPost`, spawns a worker thread calling `asyncStoreBlob` → `uploadBlobToStorage`.
- `uploadBlobToStorage` calls `storage_module.uploadInit` → `uploadChunk` → `uploadFinalize` (all via `LogosAPIClient::invokeRemoteMethod`).
- `downloadBlobFromStorage` calls `storage_module.downloadChunks`, tracks the async `storageDownloadProgress` + `storageDownloadDone` events keyed by `sessionId`.

**Key bugs found and fixed during this phase** (documented here for future integrators):

| Bug | Root cause | Fix |
|-----|-----------|-----|
| `LogosAPI not available` in callMethod | `DfosModulePlugin` declared a private `LogosAPI* logosAPI = nullptr` that shadowed the `PluginInterface` base class field. `initLogos()` wrote to the derived-class copy; the framework checked the base-class copy (which stayed `nullptr`). | Removed the private member; use the inherited `PluginInterface::logosAPI` directly. |
| `uploadInit failed: ""` despite success in debug | `storage_module` uses the "universal interface" type, whose generated `logos_provider_dispatch.cpp` serializes `StdLogosResult` to a JSON **string** before returning. Calling `result.value<LogosResult>()` silently returns a default-constructed `{success=false}` struct. | Parse the returned `QVariant` as a JSON string: `QJsonDocument::fromJson(v.toString().toUtf8()).object()`. |
| Download event handler never fired | `m_pendingDownloads` was keyed by `storageCID`, but `storageDownloadDone` provides `sessionId`. | Call `downloadChunks` first, extract `sessionId` from its JSON response, register `m_pendingDownloads[sessionId]`. |

### Phase 3 — two-agent gossip integration test

Go test in `dfos-capi/` that creates two independent Agent instances
(Alice and Bob), has Alice publish a post, captures the JWS tokens
that `gossipOps` would push to Waku, and feeds them into Bob's
`relay.Ingest()` (the same path the `delivery_module messageReceived`
handler takes in production).

```bash
cd dfos-capi
go test -v -run TestTwoAgentGossip
```

Result:
```
Alice DID: did:dfos:v6ed86627e7n4vzfa84vz6
Bob   DID: did:dfos:z924ava7a69cv348977t84
Bob sees: "Hello Bob, from Alice over DFOS!"
Bob's feed total=2  alice=1  bob=1
PASS
```

### Phase 4 — two-node Waku gossip integration test

End-to-end test using two real `logoscore` processes (Bob in background,
Alice in foreground) connected via Waku gossip.

```bash
bash scripts/two_node_waku_test.sh
```

What it validates:
1. Bob starts with a fixed TCP port (`60001`) and creates a DID.
2. Alice starts on port `60002` with Bob's multiaddr as `entryNodes`, creates a DID, publishes a post, and confirms she sees her own post in the feed.
3. After 10 s of gossip settling, Bob's log is checked for `ingest complete ... new=1` — proving the JWS operation propagated across nodes via real Waku.

Result:
```
PASS: 3  FAIL: 0
```

Confirmed outputs:
```
PASS: Bob created DID
PASS: Alice sees her own post: Hello Bob from Alice via real Waku!
PASS: Waku gossip: Bob's relay ingested Alice's content-create operation (new=1)
```

Bob's feed remains empty (expected): the blob is not auto-fetched because
no `documentCID → storageCID` exchange exists yet. This is the next
planned milestone.

## Key findings

| Finding | Details |
|---------|---------|
| `logoscore` strips quotes from inline JSON | Pass Waku config via `@file` |
| DFOS blobs must be JSON not CBOR | `publishPost` stores `json.Marshal(doc)`, not `dfos.DocumentCID` bytes |
| `SubscriptionManager requires AutoSharding` | Pass `{"numShardsInNetwork":1}` to `createNode` |
| `dfos.so` SONAME | Needs `patchelf --set-soname` + `--replace-needed` for `$ORIGIN` RPATH |
| `onEvent` signature | Workspace SDK has 3-param form `(LogosObject*, name, callback)` |
| Dummy peer entry needed | `relay.PeerConfig` with any URL so `gossipOps` fires |
| Universal vs QtProvider interface | Universal-interface modules (storage_module) return `QVariant(QString)` containing serialized `StdLogosResult` JSON — never `QVariant(LogosResult)`. Always parse with `fromJson(v.toString().toUtf8())`. |
| Private member shadowing PluginInterface | Declaring `LogosAPI* logosAPI` in the plugin class shadows the base class field the framework checks. Use the inherited field only. |
| sessionId vs storageCID tracking | `storage_module` download events are keyed by `sessionId` (returned by `downloadChunks`), not by `storageCID`. |
| Proof plane vs blob plane | Waku gossip propagates JWS operations (proof plane). Blobs require a separate `documentCID → storageCID` mapping exchange. |
