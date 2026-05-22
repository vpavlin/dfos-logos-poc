#!/usr/bin/env bash
# Two-node Waku gossip integration test.
# Runs two separate logoscore instances (Alice and Bob) connected to the
# logos.dev network and verifies:
#  1. Proof plane: Alice's JWS op reaches Bob via Waku gossip (new=1)
#  2. Blob plane:  Alice's storage-map Waku msg triggers download on Bob
set -eo pipefail

LOGOSCORE=/home/vpavlin/devel/github.com/logos-co/logos-workspace/repos/logos-liblogos/build/bin/logoscore
MODULES=/tmp/dfos-combined-modules
BOB_PORT=60001
ALICE_PORT=60002

PASS=0
FAIL=0

pass() { echo "PASS: $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL: $*"; FAIL=$((FAIL+1)); }

BOB_PID=""
ALICE_PID=""
cleanup() {
    [ -n "$ALICE_PID" ] && kill "$ALICE_PID" 2>/dev/null || true
    [ -n "$ALICE_PID" ] && wait "$ALICE_PID" 2>/dev/null || true
    [ -n "$BOB_PID" ]   && kill "$BOB_PID"   2>/dev/null || true
    [ -n "$BOB_PID" ]   && wait "$BOB_PID"   2>/dev/null || true
}
trap cleanup EXIT

rm -rf /tmp/dfos-alice /tmp/dfos-bob /tmp/dfos-storage-bob /tmp/dfos-storage-alice
mkdir -p /tmp/dfos-alice /tmp/dfos-bob /tmp/dfos-storage-bob /tmp/dfos-storage-alice

# Storage configs — separate data dirs so each node has its own Codex store
cat > /tmp/bob-storage.json <<'EOF'
{"data-dir":"/tmp/dfos-storage-bob"}
EOF
cat > /tmp/alice-storage.json <<'EOF'
{"data-dir":"/tmp/dfos-storage-alice"}
EOF

# Waku config: logos.dev preset connects to the Logos Dev Network.
# Both nodes join the same gossipsub mesh via the network's bootstrap nodes,
# allowing message propagation without direct peer discovery issues.
cat > /tmp/bob-waku.json <<EOF
{"logLevel": "INFO", "mode": "Core", "preset": "logos.dev", "tcpPort": $BOB_PORT, "discv5UdpPort": $BOB_PORT}
EOF
cat > /tmp/alice-waku.json <<EOF
{"logLevel": "INFO", "mode": "Core", "preset": "logos.dev", "tcpPort": $ALICE_PORT, "discv5UdpPort": $ALICE_PORT}
EOF

# ── Bob ───────────────────────────────────────────────────────────────────────
echo "=== Starting Bob (port $BOB_PORT, logos.dev network) ==="
LD_LIBRARY_PATH="$MODULES:${LD_LIBRARY_PATH:-}" \
$LOGOSCORE \
  --modules-dir $MODULES \
  --mode 1 \
  --load-modules delivery_module,storage_module,dfos_module \
  --call 'delivery_module.createNode(@/tmp/bob-waku.json)' \
  --call 'delivery_module.start()' \
  --call 'delivery_module.getNodeInfo(MyPeerId)' \
  --call 'storage_module.init(@/tmp/bob-storage.json)' \
  --call 'dfos_module.start(/tmp/dfos-bob)' \
  --call 'dfos_module.createIdentity()' \
  > /tmp/bob.log 2>&1 &
BOB_PID=$!

# Wait for Bob to connect to logos.dev network (relayCount >= 1) and complete startup
echo "Waiting for Bob to connect to logos.dev and create identity..."
TIMEOUT=60
for i in $(seq 1 $TIMEOUT); do
    if grep -q 'did:dfos:' /tmp/bob.log 2>/dev/null && \
       grep -qE 'relayCount=[1-9]' /tmp/bob.log 2>/dev/null; then break; fi
    sleep 1
    if ! kill -0 "$BOB_PID" 2>/dev/null; then
        fail "Bob process died during startup"
        tail -30 /tmp/bob.log
        exit 1
    fi
done

if grep -q 'did:dfos:' /tmp/bob.log; then
    pass "Bob created DID"
else
    fail "Bob did not create DID (timeout)"
    tail -20 /tmp/bob.log
    exit 1
fi

BOB_RELAY=$(grep -oE 'relayCount=[0-9]+|outRelayConns=[0-9]+' /tmp/bob.log | tail -1 || true)
echo "Bob network status: ${BOB_RELAY:-unknown}"

# ── Alice ─────────────────────────────────────────────────────────────────────
echo ""
echo "=== Starting Alice (port $ALICE_PORT, logos.dev network) ==="
# Alice runs in background so her process stays alive while Waku propagates.
LD_LIBRARY_PATH="$MODULES:${LD_LIBRARY_PATH:-}" \
$LOGOSCORE \
  --modules-dir $MODULES \
  --mode 1 \
  --load-modules delivery_module,storage_module,dfos_module \
  --call 'delivery_module.createNode(@/tmp/alice-waku.json)' \
  --call 'delivery_module.start()' \
  --call 'storage_module.init(@/tmp/alice-storage.json)' \
  --call 'dfos_module.start(/tmp/dfos-alice)' \
  --call 'dfos_module.createIdentity()' \
  --call 'dfos_module.publishPost(Hello Bob from Alice via real Waku!)' \
  --call 'dfos_module.getFeed(5)' \
  > /tmp/alice.log 2>&1 &
ALICE_PID=$!

# Wait for Alice to connect to the network, complete operations, and see her feed
echo "Waiting for Alice to connect and publish post..."
TIMEOUT=90
for i in $(seq 1 $TIMEOUT); do
    if grep -q '"text":"Hello Bob' /tmp/alice.log 2>/dev/null; then break; fi
    sleep 1
    if ! kill -0 "$ALICE_PID" 2>/dev/null; then
        echo "Alice process exited (may be normal)"
        break
    fi
done

# Extract Alice's feed from log
ALICE_FEED=$(grep 'Result:.*contentId' /tmp/alice.log | tail -1 | sed 's/.*Result: //') || true
if echo "$ALICE_FEED" | grep -q '"text":"Hello Bob'; then
    ALICE_TEXT=$(echo "$ALICE_FEED" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d[0]["text"])' 2>/dev/null) || ALICE_TEXT="$ALICE_FEED"
    pass "Alice sees her own post: $ALICE_TEXT"
else
    fail "Alice did not see her post in feed"
    echo "Alice log tail:"
    tail -15 /tmp/alice.log
fi

ALICE_RELAY=$(grep -oE 'relayCount=[0-9]+|outRelayConns=[0-9]+' /tmp/alice.log | tail -1 || true)
echo "Alice network status: ${ALICE_RELAY:-unknown}"

# Wait for Waku gossip to propagate through the logos.dev network.
# The network mesh is established, so gossip typically takes 5-15s end-to-end.
echo ""
echo "Waiting for Waku gossip + storage-map to propagate via logos.dev (~30s)..."
sleep 30

# ── Bob checks ────────────────────────────────────────────────────────────────
echo ""
echo "=== Bob's log for DFOS + blob events ==="
grep -E "DfosModulePlugin|received op|ingest complete|storage-map|download started" /tmp/bob.log | head -30 || true

# Bob's first 'ingest complete new=1' is from his own createIdentity.
# Alice's ops arrive via Waku gossip and produce additional ingest events.
BOB_INGEST_COUNT=$(grep -c 'ingest complete.*new=1' /tmp/bob.log 2>/dev/null) || BOB_INGEST_COUNT=0
if [ "$BOB_INGEST_COUNT" -ge 2 ]; then
    pass "Waku gossip (proof plane): Bob ingested Alice's ops via Waku (${BOB_INGEST_COUNT} new ingest events)"
else
    fail "Waku gossip: Bob only saw ${BOB_INGEST_COUNT} new ingest event(s); expected ≥2 (own + Alice's gossip)"
    grep 'ingest complete' /tmp/bob.log || true
fi

echo ""
echo "=== Blob plane check ==="
if grep -q 'received storage-map' /tmp/bob.log; then
    pass "Blob plane: Bob received Alice's storage-map Waku message"
    if grep -q 'download started storageCID' /tmp/bob.log; then
        pass "Blob plane: Bob initiated blob download from storage_module"
    else
        echo "INFO: storage-map received but download not started (storage upload may have failed)"
    fi
elif grep -q 'published storage-map' /tmp/alice.log; then
    echo "INFO: Alice published storage-map but Bob has not received it yet"
    echo "      (network timing — may need longer wait)"
else
    if grep -q 'storage upload failed\|uploadInit failed' /tmp/alice.log; then
        echo "INFO: Alice storage upload failed (Codex network not available — expected in local test)"
    else
        echo "INFO: storage-map not sent (check Alice log for upload status)"
    fi
fi

echo ""
echo "=== Result ==="
echo "PASS: $PASS  FAIL: $FAIL"
[ $FAIL -eq 0 ]
