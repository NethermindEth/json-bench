# Upstream sources

External corpora that feed `rpc-calls/` via the `scripts/generate-from-*/`
generators. The downloaded payloads land under `rpc-calls/sources/` (gitignored)
and the generated `*.jsonl` files are checked in.

## erigontech/rpc-tests

- Repo: <https://github.com/erigontech/rpc-tests>
- License: Apache-2.0
- Generator: `scripts/generate-from-erigon-rpc-tests/`
- Extraction: data only (the `integration/mainnet/<method>/test_*.json`
  fixtures); no upstream code is vendored or executed.
