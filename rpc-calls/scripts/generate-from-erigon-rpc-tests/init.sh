#!/usr/bin/env bash
set -euo pipefail

REPO="erigontech/rpc-tests"
UPSTREAM_PATH="integration/mainnet"

METHODS=(
  eth_call
  eth_getProof
  eth_estimateGas
  eth_getCode
  eth_getStorageAt
  debug_traceCall
  debug_traceTransaction
)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEST_ROOT="${SCRIPT_DIR}/../../sources/erigon-rpc-tests/${UPSTREAM_PATH}"

for method in "${METHODS[@]}"; do
  dest="${DEST_ROOT}/${method}"
  mkdir -p "${dest}"
  echo "[erigon-rpc-tests] fetching ${method}…"

  # --paginate handles dirs > 30 entries (e.g. debug_traceTransaction has 155)
  urls=$(gh api --paginate "repos/${REPO}/contents/${UPSTREAM_PATH}/${method}" \
    --jq '.[] | select(.type == "file") | .download_url')

  if [[ -z "${urls}" ]]; then
    echo "  (no files returned for ${method})" >&2
    exit 1
  fi

  while IFS= read -r url; do
    [[ -z "${url}" ]] && continue
    fname="${url##*/}"
    curl -fsSL "${url}" -o "${dest}/${fname}"
  done <<< "${urls}"

  echo "  $(ls "${dest}" | wc -l | tr -d ' ') files in ${dest}"
done

echo "[erigon-rpc-tests] done."
