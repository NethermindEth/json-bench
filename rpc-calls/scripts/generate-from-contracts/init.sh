#!/usr/bin/env bash
set -euo pipefail

# Fetch contract ABIs into the gitignored cache directory. Resolves single-hop
# proxies automatically so the cached ABI matches the executable code, while the
# cache key stays the contract's address-of-record (the YAML `address`).
#
# Per-chain ABI provider:
#   chain 1   (mainnet) — Etherscan v2, needs <repo root>/etherscan_api_key
#   chain 100 (gnosis)  — Gnosis Blockscout, keyless
#
# Requirements: curl, jq, yq (Mike Farah's go-yq v4).
# The Etherscan key is never echoed.

REFRESH=0
CONFIG_ARG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --refresh) REFRESH=1 ;;
    --config) CONFIG_ARG="$2"; shift ;;
    *) echo "[generate-from-contracts] unknown argument: $1" >&2; exit 1 ;;
  esac
  shift
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
CONFIG="${CONFIG_ARG:-${SCRIPT_DIR}/contracts.yaml}"
CACHE_DIR="${REPO_ROOT}/rpc-calls/sources/contract-abis"
KEY_FILE="${REPO_ROOT}/etherscan_api_key"

for tool in curl jq yq; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "[generate-from-contracts] missing required tool: ${tool}" >&2
    exit 1
  fi
done

if [[ ! -f "${CONFIG}" ]]; then
  echo "[generate-from-contracts] missing config ${CONFIG}" >&2
  exit 1
fi

ETHERSCAN_API_KEY=""
require_etherscan_key() {
  if [[ -n "${ETHERSCAN_API_KEY}" ]]; then
    return
  fi
  if [[ ! -f "${KEY_FILE}" ]]; then
    echo "[generate-from-contracts] missing ${KEY_FILE} (create it with your Etherscan API key)" >&2
    exit 1
  fi
  ETHERSCAN_API_KEY="$(cat "${KEY_FILE}")"
  if [[ -z "${ETHERSCAN_API_KEY}" ]]; then
    echo "[generate-from-contracts] ${KEY_FILE} is empty" >&2
    exit 1
  fi
}

mkdir -p "${CACHE_DIR}"

API="https://api.etherscan.io/v2/api"
BLOCKSCOUT_GNOSIS="https://gnosis.blockscout.com/api/v2/smart-contracts"

# Returns the response body. Fails loud (with the key REDACTED) on transport
# error or non-1 Etherscan status.
etherscan_call() {
  local action="$1"
  local address="$2"
  local resp
  resp="$(curl -fsS -G "${API}" \
    --data-urlencode "chainid=1" \
    --data-urlencode "module=contract" \
    --data-urlencode "action=${action}" \
    --data-urlencode "address=${address}" \
    --data-urlencode "apikey=${ETHERSCAN_API_KEY}")" || {
      echo "[generate-from-contracts] HTTP error calling ${action} for ${address}" >&2
      return 1
    }
  local status
  status="$(printf '%s' "${resp}" | jq -r '.status')"
  if [[ "${status}" != "1" ]]; then
    local message result
    message="$(printf '%s' "${resp}" | jq -r '.message')"
    result="$(printf '%s' "${resp}" | jq -r 'if (.result|type)=="string" then .result else "<object>" end')"
    echo "[generate-from-contracts] Etherscan ${action} failed for ${address}: ${message} — ${result}" >&2
    return 1
  fi
  printf '%s' "${resp}"
}

blockscout_call() {
  local address="$1"
  local resp
  resp="$(curl -fsSL -m 30 "${BLOCKSCOUT_GNOSIS}/${address}")" || {
    echo "[generate-from-contracts] HTTP error calling Blockscout for ${address}" >&2
    return 1
  }
  # Unverified contracts come back as a well-formed record with `abi: null`.
  if [[ "$(printf '%s' "${resp}" | jq -r '(.abi | type) == "array"')" != "true" ]]; then
    echo "[generate-from-contracts] Blockscout has no verified ABI for ${address}" >&2
    return 1
  fi
  printf '%s' "${resp}"
}

resolve_impl() {
  local chain="$1" address="$2"
  local resp impl
  if [[ "${chain}" == "100" ]]; then
    resp="$(blockscout_call "${address}")"
    impl="$(printf '%s' "${resp}" | jq -r '.implementations[0].address_hash // .implementations[0].address // ""')"
  else
    require_etherscan_key
    resp="$(etherscan_call getsourcecode "${address}")"
    impl="$(printf '%s' "${resp}" | jq -r '.result[0].Implementation // ""')"
  fi
  if [[ -n "${impl}" && "${impl}" != "null" ]]; then
    printf '%s' "${impl}"
  else
    printf '%s' "${address}"
  fi
}

fetch_abi() {
  local chain="$1" address="$2"
  if [[ "${chain}" == "100" ]]; then
    blockscout_call "${address}" | jq '.abi'
    return
  fi
  require_etherscan_key
  local resp
  resp="$(etherscan_call getabi "${address}")"
  # result is a JSON-encoded string containing the ABI array. Re-parse and
  # re-emit it as a pretty-printed JSON document so the cache is human-readable.
  printf '%s' "${resp}" | jq -r '.result' | jq '.'
}

count="$(yq '.contracts | length' "${CONFIG}")"
echo "[generate-from-contracts] resolving ${count} contracts"

for ((i=0; i<count; i++)); do
  name="$(yq ".contracts[${i}].name" "${CONFIG}")"
  addr="$(yq ".contracts[${i}].address" "${CONFIG}")"
  abi_override="$(yq ".contracts[${i}].abi_address // \"\"" "${CONFIG}")"
  chain="$(yq ".contracts[${i}].chain_id // 1" "${CONFIG}")"

  addr_lc="$(printf '%s' "${addr}" | tr '[:upper:]' '[:lower:]')"
  cache_file="${CACHE_DIR}/${addr_lc}.json"

  if [[ "${REFRESH}" -eq 1 && -f "${cache_file}" ]]; then
    rm -f "${cache_file}"
  fi

  if [[ -f "${cache_file}" ]]; then
    echo "  [skip] ${name} (cached)"
    continue
  fi

  if [[ -n "${abi_override}" ]]; then
    resolved="${abi_override}"
    echo "  [pin]  ${name} ${addr} → ${resolved}"
  else
    resolved="$(resolve_impl "${chain}" "${addr}")"
    sleep 0.25
    if [[ "${resolved}" != "${addr}" ]]; then
      echo "  [proxy] ${name} ${addr} → ${resolved}"
    else
      echo "  [fetch] ${name} ${addr}"
    fi
  fi

  fetch_abi "${chain}" "${resolved}" > "${cache_file}"
  sleep 0.25
done

echo "[generate-from-contracts] done — cached ${count} ABIs under ${CACHE_DIR}"
