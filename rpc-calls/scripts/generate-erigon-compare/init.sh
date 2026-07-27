#!/usr/bin/env bash
set -euo pipefail

# Fetch the erigontech/rpc-tests integration corpus that
# generate-erigon-compare consumes. Downloads the source tarball at a pinned
# ref and keeps only the integration/<network> subtree; the resolved commit
# SHA is recorded in SOURCE_REF next to the corpus so the generator can stamp
# it into the configs for provenance.
#
# Usage: init.sh [network]   (default: mainnet)
#        RPC_TESTS_REF=<ref> init.sh mainnet   # pull a different corpus

REPO="erigontech/rpc-tests"
REF="${RPC_TESTS_REF:-214c13799371e832a90d92781f83b0fe2d143d68}"
NETWORK="${1:-mainnet}"
UPSTREAM_PATH="integration/${NETWORK}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEST_ROOT="${SCRIPT_DIR}/../../sources/erigon-rpc-tests"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Resolve the ref to a full commit SHA for provenance (best-effort).
RESOLVED="$(gh api "repos/${REPO}/commits/${REF}" --jq .sha 2>/dev/null || echo "$REF")"

echo "[erigon-compare] fetching ${REPO}@${REF} (${UPSTREAM_PATH} only)…"
curl -fsSL "https://codeload.github.com/${REPO}/tar.gz/${REF}" -o "$TMP/src.tgz"

# Extract into a throwaway dir (removed on exit), then copy out only the
# subtree we need. The tarball wraps everything in a single rpc-tests-<ref>/
# directory, so --strip-components=1 lifts the corpus to integration/<network>.
mkdir -p "$TMP/extract"
tar -xzf "$TMP/src.tgz" -C "$TMP/extract" --strip-components=1

if [[ ! -d "$TMP/extract/${UPSTREAM_PATH}" ]]; then
  echo "[erigon-compare] error: ${UPSTREAM_PATH} not found in tarball" >&2
  exit 1
fi

rm -rf "${DEST_ROOT:?}/${UPSTREAM_PATH}"
mkdir -p "${DEST_ROOT}/integration"
cp -R "$TMP/extract/${UPSTREAM_PATH}" "${DEST_ROOT}/${UPSTREAM_PATH}"
printf '%s\n' "$RESOLVED" > "${DEST_ROOT}/SOURCE_REF"

echo "[erigon-compare] $(find "${DEST_ROOT}/${UPSTREAM_PATH}" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ') methods into ${DEST_ROOT}/${UPSTREAM_PATH}"
echo "[erigon-compare] SOURCE_REF=${RESOLVED}"
