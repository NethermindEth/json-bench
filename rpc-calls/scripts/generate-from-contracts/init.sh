#!/usr/bin/env bash
set -euo pipefail

# Fetch contract ABIs from Etherscan v2 into the gitignored cache directory.
# Resolves single-hop proxies automatically via the getsourcecode endpoint's
# Implementation field. The cache key is always the contract's address-of-record
# (the YAML `address`), regardless of whether resolution happened.
#
# Requirements: curl, jq, yq (Mike Farah's go-yq v4).
# Auth: reads the API key from <repo root>/etherscan_api_key — the file is
# gitignored. The key is never echoed.

REFRESH=0
if [[ "${1:-}" == "--refresh" ]]; then
  REFRESH=1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
CONFIG="${SCRIPT_DIR}/contracts.yaml"
CACHE_DIR="${REPO_ROOT}/rpc-calls/sources/contract-abis"
KEY_FILE="${REPO_ROOT}/etherscan_api_key"

for tool in curl jq yq; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "[generate-from-contracts] missing required tool: ${tool}" >&2
    exit 1
  fi
done

if [[ ! -f "${KEY_FILE}" ]]; then
  echo "[generate-from-contracts] missing ${KEY_FILE} (create it with your Etherscan API key)" >&2
  exit 1
fi
ETHERSCAN_API_KEY="$(cat "${KEY_FILE}")"
if [[ -z "${ETHERSCAN_API_KEY}" ]]; then
  echo "[generate-from-contracts] ${KEY_FILE} is empty" >&2
  exit 1
fi

mkdir -p "${CACHE_DIR}"

API="https://api.etherscan.io/v2/api"

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

resolve_impl() {
  local address="$1"
  local resp impl
  resp="$(etherscan_call getsourcecode "${address}")"
  impl="$(printf '%s' "${resp}" | jq -r '.result[0].Implementation // ""')"
  if [[ -n "${impl}" && "${impl}" != "null" ]]; then
    printf '%s' "${impl}"
  else
    printf '%s' "${address}"
  fi
}

fetch_abi() {
  local address="$1"
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
    resolved="$(resolve_impl "${addr}")"
    sleep 0.25
    if [[ "${resolved}" != "${addr}" ]]; then
      echo "  [proxy] ${name} ${addr} → ${resolved}"
    else
      echo "  [fetch] ${name} ${addr}"
    fi
  fi

  fetch_abi "${resolved}" > "${cache_file}"
  sleep 0.25
done

echo "[generate-from-contracts] done — cached ${count} ABIs under ${CACHE_DIR}"
