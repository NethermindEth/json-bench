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

## Curated mainnet contracts

- Source: in-tree (`scripts/generate-from-contracts/contracts.yaml`)
- ABI provenance: Etherscan v2 (`api.etherscan.io/v2/api`, `chainid=1`).
  `init.sh` resolves single-hop proxies via the `getsourcecode`
  `Implementation` field so the cached ABI corresponds to the executable
  code at the contract's address.
- License: the contract list is our own curation under the repo's license;
  ABIs are public bytecode metadata published by the contract authors.
- Generator: `scripts/generate-from-contracts/`
- Extraction: ABI JSON only (cached under `rpc-calls/sources/contract-abis/`,
  gitignored); no contract source code is vendored.
