#!/usr/bin/env bash
# Probe two Ethereum JSON-RPC endpoints before a correctness comparison.
#
# Usage: probe-nodes.sh <baseline-url> <candidate-url> [margin-blocks]
#
# Checks reachability, chainId agreement, both heads, sync state and client
# versions; then proposes a static historical pin block at/below the lower head
# (minus a margin, rounded down to a round number) and verifies BOTH nodes
# return the same block hash there. Prints a PIN_BLOCK line to use with
# --block-override. Exits non-zero if the nodes are not comparable.
set -uo pipefail

BASE="${1:?usage: probe-nodes.sh <baseline-url> <candidate-url> [margin-blocks]}"
CAND="${2:?usage: probe-nodes.sh <baseline-url> <candidate-url> [margin-blocks]}"
MARGIN="${3:-5000}"   # min blocks below the lower head, before rounding

rpc() { # $1 url  $2 method  $3 params-json — retries transient blips (nodes throttle bursts)
  local attempt out
  for attempt in 1 2 3 4 5; do
    out=$(curl -s -m 20 -X POST -H 'content-type: application/json' \
      --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$2\",\"params\":${3:-[]}}" "$1")
    if [ -n "$out" ]; then echo "$out"; return 0; fi
    sleep $(( attempt ))
  done
  return 1
}
result() { python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('result','')) if 'error' not in d else sys.exit('ERR '+str(d['error']))" 2>/dev/null; }

fail() { echo "NOT COMPARABLE: $*" >&2; exit 1; }

echo "== reachability / identity =="
for label in BASE CAND; do
  url="${!label}"
  cid=$(rpc "$url" eth_chainId | result)   || fail "$label ($url) unreachable or errored on eth_chainId"
  ver=$(rpc "$url" web3_clientVersion | result)
  syncing=$(rpc "$url" eth_syncing | result)
  head_hex=$(rpc "$url" eth_blockNumber | result) || fail "$label ($url) errored on eth_blockNumber"
  printf "  %-4s %s\n       chainId=%s head=%s (%d) syncing=%s\n       version=%s\n" \
    "$label" "$url" "$cid" "$head_hex" "$((head_hex))" "$syncing" "$ver"
  eval "${label}_CID=$cid"; eval "${label}_HEAD=$((head_hex))"
done

[ "$BASE_CID" = "$CAND_CID" ] || fail "chainId mismatch: baseline=$BASE_CID candidate=$CAND_CID (different networks)"

LOWER=$(( BASE_HEAD < CAND_HEAD ? BASE_HEAD : CAND_HEAD ))
echo "== pin-block selection =="
echo "  lower head = $LOWER"
TARGET=$(( LOWER - MARGIN ))
[ "$TARGET" -gt 0 ] || fail "lower head $LOWER is below the $MARGIN-block margin; nodes too shallow to pick a historical block"

# Round down to a 'nice' boundary for readability: 1,000,000 if there's room,
# else 100,000, else 1,000.
for step in 1000000 100000 1000; do
  if [ "$TARGET" -ge "$step" ]; then PIN=$(( (TARGET / step) * step )); break; fi
done
PIN_HEX=$(printf '0x%x' "$PIN")
echo "  proposed pin block = $PIN ($PIN_HEX), i.e. $((LOWER - PIN)) blocks below the lower head"

echo "== verifying both nodes agree at the pin block =="
BH_BASE=$(rpc "$BASE" eth_getBlockByNumber "[\"$PIN_HEX\",false]" | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',{}).get('hash',''))" 2>/dev/null)
BH_CAND=$(rpc "$CAND" eth_getBlockByNumber "[\"$PIN_HEX\",false]" | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',{}).get('hash',''))" 2>/dev/null)
echo "  baseline  hash = ${BH_BASE:-<none>}"
echo "  candidate hash = ${BH_CAND:-<none>}"
[ -n "$BH_BASE" ] && [ -n "$BH_CAND" ] || fail "one node did not return block $PIN_HEX"
[ "$BH_BASE" = "$BH_CAND" ] && echo "  OK: identical block hash" \
  || fail "block-hash mismatch at $PIN_HEX — the nodes disagree on history; investigate before comparing"

echo
echo "PIN_BLOCK=$PIN_HEX"
echo "Use: --block-override $PIN_HEX   (and --skip-above-head to drop calls above block $LOWER)"
