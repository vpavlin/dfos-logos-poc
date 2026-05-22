#!/usr/bin/env bash
# Two-node Waku gossip integration test.
# Runs two separate logoscore instances (Alice and Bob) and verifies
# that Alice's published post reaches Bob via actual Waku gossip.
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
cleanup() {
    [ -n "$BOB_PID" ] && kill "$BOB_PID" 2>/dev/null || true
    [ -n "$BOB_PID" ] && wait "$BOB_PID" 2>/dev/null || true
}
trap cleanup EXIT

rm -rf /tmp/dfos-alice /tmp/dfos-bob
mkdir -p /tmp/dfos-alice /tmp/dfos-bob

# Bob's Waku config: fixed port so we can predict his address
cat > /tmp/bob-waku.json <<EOF
{"numShardsInNetwork": 1, "tcpPort": $BOB_PORT}
EOF

echo "=== Starting Bob (port $BOB_PORT) ==="
LD_LIBRARY_PATH="$MODULES:${LD_LIBRARY_PATH:-}" \
$LOGOSCORE \
  --modules-dir $MODULES \
  --mode 1 \
  --load-modules delivery_module,dfos_module \
  --call 'delivery_module.createNode(@/tmp/bob-waku.json)' \
  --call 'delivery_module.start()' \
  --call 'delivery_module.getNodeInfo(MyPeerId)' \
  --call 'delivery_module.getNodeInfo(MyMultiaddresses)' \
  --call 'dfos_module.start(/tmp/dfos-bob)' \
  --call 'dfos_module.createIdentity()' \
  > /tmp/bob.log 2>&1 &
BOB_PID=$!

# Wait for Bob's createIdentity to complete (last of 6 startup calls)
echo "Waiting for Bob to start (all 6 calls including createIdentity)..."
TIMEOUT=40
for i in $(seq 1 $TIMEOUT); do
    if grep -q 'did:dfos:' /tmp/bob.log 2>/dev/null; then
        break
    fi
    sleep 1
    if ! kill -0 "$BOB_PID" 2>/dev/null; then
        fail "Bob process died during startup"
        tail -30 /tmp/bob.log
        exit 1
    fi
done

# Extract Bob's peerId and multiaddr
BOB_PEERID=$(grep 'Method call successful. Result: 16U' /tmp/bob.log | head -1 \
    | sed 's/.*Result: //')
BOB_MULTIADDR=$(grep 'Method call successful. Result: /ip4' /tmp/bob.log | head -1 \
    | sed 's/.*Result: //' | awk '{print $1}')

if [ -z "$BOB_PEERID" ]; then
    fail "Could not extract Bob's peer ID"
    cat /tmp/bob.log
    exit 1
fi
# Use loopback for local connection
BOB_MULTIADDR_LOCAL="/ip4/127.0.0.1/tcp/$BOB_PORT/p2p/$BOB_PEERID"
echo "Bob peer ID: $BOB_PEERID"
echo "Bob multiaddr: $BOB_MULTIADDR_LOCAL"

# Check Bob got an identity
if grep -q 'did:dfos:' /tmp/bob.log; then
    pass "Bob created DID"
else
    fail "Bob did not create DID"
fi

# Alice's config: Bob as entry node, different port
cat > /tmp/alice-waku.json <<EOF
{"numShardsInNetwork": 1, "tcpPort": $ALICE_PORT, "entryNodes": ["$BOB_MULTIADDR_LOCAL"]}
EOF

echo ""
echo "=== Running Alice (port $ALICE_PORT, connecting to Bob) ==="
ALICE_OUT=$(LD_LIBRARY_PATH="$MODULES:${LD_LIBRARY_PATH:-}" \
$LOGOSCORE \
  --modules-dir $MODULES \
  --mode 1 \
  --load-modules delivery_module,dfos_module \
  --call 'delivery_module.createNode(@/tmp/alice-waku.json)' \
  --call 'delivery_module.start()' \
  --call 'dfos_module.start(/tmp/dfos-alice)' \
  --call 'dfos_module.createIdentity()' \
  --call 'dfos_module.publishPost(Hello Bob from Alice via real Waku!)' \
  --call 'dfos_module.getFeed(5)' \
  --quit-on-finish 2>&1)

ALICE_FEED=$(echo "$ALICE_OUT" | grep 'Result:.*contentId' | tail -1 | sed 's/.*Result: //') || true
if echo "$ALICE_FEED" | grep -q '"text":"Hello Bob'; then
    ALICE_TEXT=$(echo "$ALICE_FEED" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d[0]["text"])' 2>/dev/null) || ALICE_TEXT="$ALICE_FEED"
    pass "Alice sees her own post: $ALICE_TEXT"
else
    fail "Alice did not see her post in feed"
    echo "Alice output tail:"
    echo "$ALICE_OUT" | tail -10
fi

# Wait for Waku gossip to propagate to Bob
echo ""
echo "Waiting for Waku gossip to reach Bob (~10s)..."
sleep 10

# Check Bob's log for DFOS ingest events (proof-plane propagation)
echo ""
echo "=== Bob's log for DFOS ingest events ==="
grep -E "DfosModulePlugin|received op|ingest complete" /tmp/bob.log | head -20 || true

# Verify the operation was ingested via Waku gossip
if grep -q 'ingest complete.*new=1' /tmp/bob.log; then
    pass "Waku gossip: Bob's relay ingested Alice's content-create operation (new=1)"
else
    fail "Waku gossip: Bob did not receive Alice's operation"
    echo "Bob's ingest log:"
    grep -E "ingest|received op|messageReceived" /tmp/bob.log | head -10 || true
fi

# Note: getFeed returns empty for cross-node posts because the blob (post body) is
# only available on Alice's node. The proof-plane (JWS op) propagates via Waku;
# the blob plane requires a storageCID exchange mechanism (future work).
echo ""
echo "=== Cross-node blob retrieval check ==="
echo "  Gossip delivers the JWS operation (content chain). The blob"
echo "  is not auto-fetched (no storageCID exchange yet — future work)."
echo "  Bob's feed will show the post once blob retrieval is implemented."

echo ""
echo "=== Result ==="
echo "PASS: $PASS  FAIL: $FAIL"
[ $FAIL -eq 0 ]
