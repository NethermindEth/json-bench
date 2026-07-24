#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Regenerate the ported Erigon compare configs under config/compare/erigon/.
# Clones erigontech/rpc-tests at a pinned ref and runs import_erigon_tests.py.
#
# Usage: scripts/import_erigon_tests.sh [network]   (default: mainnet)
set -euo pipefail

REF="${RPC_TESTS_REF:-214c13799371e832a90d92781f83b0fe2d143d68}"
NETWORK="${1:-mainnet}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Cloning erigontech/rpc-tests @ ${REF}..."
git clone -q --filter=blob:none --no-checkout https://github.com/erigontech/rpc-tests.git "$TMP/rpc-tests"
git -C "$TMP/rpc-tests" sparse-checkout set "integration/${NETWORK}"
# perf/ reports contain ':' in filenames (invalid on some filesystems); we only
# need integration/, so a subtree checkout avoids them.
git -C "$TMP/rpc-tests" -c core.protectNTFS=false checkout -q "$REF" -- "integration/${NETWORK}" \
  || git -C "$TMP/rpc-tests" -c core.protectNTFS=false checkout -q "$REF"

python3 "$HERE/scripts/import_erigon_tests.py" \
  --corpus "$TMP/rpc-tests/integration/${NETWORK}" \
  --network "$NETWORK" \
  --out "$HERE/config/compare/erigon" \
  --source-ref "$REF"
