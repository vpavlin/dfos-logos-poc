# DFOS + Logos Basecamp Integration POC

Demonstrates DFOS identity and content-chain protocol running inside
Logos Basecamp as a native core module + optional QML UI.

## Architecture

```
Logos Basecamp (host)
├── delivery_module  ← existing Waku transport (Logos)
├── dfos_module      ← Phase 1: Go DFOS protocol via CGo shared library
└── dfos_ui          ← Phase 2: Qt/QML feed + compose UI (IComponent)
         │
         ▼
    dfos-capi/dfos.so   ← Phase 0: CGo c-shared library
         │
         ▼
dfos-protocol-go + dfos-web-relay-go  (vendored from metalabel/dfos)
```

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

## Key findings

| Finding | Details |
|---------|---------|
| `logoscore` strips quotes from inline JSON | Pass Waku config via `@file` |
| DFOS blobs must be JSON not CBOR | `publishPost` stores `json.Marshal(doc)`, not `dfos.DocumentCID` bytes |
| `SubscriptionManager requires AutoSharding` | Pass `{"numShardsInNetwork":1}` to `createNode` |
| `dfos.so` SONAME | Needs `patchelf --set-soname` + `--replace-needed` for `$ORIGIN` RPATH |
| `onEvent` signature | Workspace SDK has 3-param form `(LogosObject*, name, callback)` |
| Dummy peer entry needed | `relay.PeerConfig` with any URL so `gossipOps` fires |
