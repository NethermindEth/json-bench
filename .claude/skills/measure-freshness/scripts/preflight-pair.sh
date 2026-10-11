#!/usr/bin/env bash
# Readiness check for one EL(+CL) pair before `runner freshness probe`; exits non-zero on any FAIL.
# Usage: preflight-pair.sh <el-rpc-url> [beacon-url]
set -uo pipefail

EL="${1:?usage: preflight-pair.sh <el-rpc-url> [beacon-url]}"
CL="${2:-}"
HISTORY=0x0000F90827F1C53a10cb7A02335B175320002935
FAILS=0

ok()   { echo "  OK   $*"; }
warn() { echo "  WARN $*"; }
bad()  { echo "  FAIL $*"; FAILS=$((FAILS + 1)); }

rpc() {
  curl -s -m 10 -X POST -H 'content-type: application/json' \
    --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$1\",\"params\":${2:-[]}}" "$EL"
}
# Prints the result as JSON, or exits 1 printing the error.
result() { python3 -c 'import sys,json
try: d=json.load(sys.stdin)
except Exception: print("no/invalid response"); sys.exit(1)
if "error" in d: print(json.dumps(d["error"])); sys.exit(1)
print(json.dumps(d.get("result")))'; }
field() { python3 -c "import sys,json;print((json.load(sys.stdin) or {}).get('$1',''))"; }

echo "== execution client $EL =="
if cid=$(rpc eth_chainId | result); then ok "chainId $(python3 -c "print(int($cid,16))")"; else bad "eth_chainId: $cid"; echo "FAILS=$FAILS"; exit 1; fi
ver=$(rpc web3_clientVersion | result) && ok "version $ver" || warn "web3_clientVersion: $ver"

syncing=$(rpc eth_syncing | result)
[ "$syncing" = "false" ] && ok "eth_syncing false" || bad "eth_syncing = $syncing"

if head=$(rpc eth_getBlockByNumber '["latest",false]' | result); then
  num=$(echo "$head" | field number); ts=$(echo "$head" | field timestamp); parent=$(echo "$head" | field parentHash)
  age=$(( $(date +%s) - $(printf '%d' "$ts") ))
  [ "$age" -le 60 ] && ok "head $((num)) is ${age}s old" || bad "head $((num)) is ${age}s old (not following the chain)"
else
  bad "latest block: $head"; echo "FAILS=$FAILS"; exit 1
fi

code=$(rpc eth_getCode "[\"$HISTORY\",\"latest\"]" | result)
if [ "${#code}" -gt 6 ]; then ok "EIP-2935 history contract present"; else bad "EIP-2935 history contract missing (state probes unsupported): $code"; fi

data=$(python3 -c "print('0x'+(($num)-1).to_bytes(32,'big').hex())")
canary=$(rpc eth_call "[{\"from\":\"0x0000000000000000000000000000000000000000\",\"to\":\"$HISTORY\",\"gas\":\"0x186a0\",\"data\":\"$data\"},\"$num\"]" | result)
if [ "$(echo "$canary" | tr -d '"' | tr A-F a-f)" = "$(echo "$parent" | tr A-F a-f)" ]; then
  ok "EIP-2935 canary at head returns the parent hash"
else
  bad "EIP-2935 canary returned $canary, expected $parent"
fi

logs=$(rpc eth_getLogs "[{\"fromBlock\":\"$num\",\"toBlock\":\"$num\"}]" | result) \
  && ok "eth_getLogs whole block: $(echo "$logs" | python3 -c 'import sys,json;print(len(json.load(sys.stdin)))') logs" \
  || bad "eth_getLogs whole block: $logs"

future=$(printf '0x%x' $(( num + 10000 )))
nr=$(rpc eth_call "[{\"to\":\"$HISTORY\",\"data\":\"$data\"},\"$future\"]")
echo "  INFO not-ready shape for eth_call at head+10000: $nr"

if [ -n "$CL" ]; then
  echo "== consensus client $CL =="
  if s=$(curl -s -m 10 "$CL/eth/v1/node/syncing"); then
    for f in is_syncing is_optimistic el_offline; do
      v=$(echo "$s" | python3 -c "import sys,json;d=json.load(sys.stdin).get('data',{});print(d.get('$f','<missing>'))" 2>/dev/null)
      case "$v" in
        False) ok "$f false" ;;
        '<missing>') warn "$f not reported (recorded as unknown)" ;;
        *) bad "$f = $v" ;;
      esac
    done
  else
    bad "beacon /eth/v1/node/syncing unreachable"
  fi
  v=$(curl -s -m 10 "$CL/eth/v1/node/version" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["version"])' 2>/dev/null) \
    && ok "version $v" || warn "beacon version unavailable"
  sps=$(curl -s -m 10 "$CL/eth/v1/config/spec" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["SECONDS_PER_SLOT"])' 2>/dev/null) \
    && ok "SECONDS_PER_SLOT $sps" || warn "beacon spec unavailable (set chain.slot_duration_seconds if the chain has no preset)"
else
  warn "no beacon URL: no CL health check, no CL events in timelines"
fi

echo "== clock of this machine =="
if command -v chronyc >/dev/null 2>&1; then
  chronyc -c tracking 2>/dev/null | python3 -c 'import sys
f=sys.stdin.read().strip().split(",")
if len(f)<14: print("  WARN chronyc output unreadable"); sys.exit()
err=(abs(float(f[4]))+float(f[10])/2+float(f[11]))*1000
print(("  OK  " if f[13].strip()!="Not synchronised" else "  FAIL")+" chrony error bound %.2f ms (leap: %s)"%(err,f[13].strip()))'
elif command -v timedatectl >/dev/null 2>&1; then
  s=$(timedatectl show -p NTPSynchronized --value)
  [ "$s" = "yes" ] && warn "NTP synchronised but no error estimate (probe will use clock.error_budget_ms)" || bad "NTP not synchronised"
else
  warn "no chronyc/timedatectl: probe falls back to clock.error_budget_ms"
fi

echo
[ "$FAILS" -eq 0 ] && echo "READY" || echo "NOT READY: $FAILS failing check(s)"
exit $(( FAILS > 0 ))
