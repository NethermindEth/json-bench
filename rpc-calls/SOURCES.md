# Upstream sources

External corpora that feed `rpc-calls/` via the `scripts/generate-from-*/`
generators. The downloaded payloads land under `rpc-calls/sources/` (gitignored)
and the generated `*.jsonl` files are checked in.

## erigontech/rpc-tests

- Repo: <https://github.com/erigontech/rpc-tests>
- License: Apache-2.0
- Generators:
  - `scripts/generate-from-erigon-rpc-tests/` — `*.jsonl` load-test fixtures
    for a curated subset of methods.
  - `scripts/generate-erigon-compare/` — differential `compare` configs under
    `config/compare/erigon/` covering the whole corpus, retargeted to a
    runnable block.
- Extraction: data only (the `integration/<network>/<method>/test_*.json`
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

## EthCallChaos fuzzing corpus

- Source: EthCallChaos — an out-of-tree, coverage-guided `eth_call` fuzzer that
  evolves worst-case `eth_call` / Multicall3 payloads and records each candidate
  (with its measured fitness) in a SQLite corpus DB.
- License: payloads are synthetic fuzzer output, not third-party content.
- Generator: `scripts/generate-from-ethcallchaos/`
- Corpus: the SQLite DB is produced out-of-tree and is not vendored; drop it
  under `rpc-calls/sources/ethcallchaos/` (gitignored). The generator reads the
  `test_cases` table read-only.
- Checked-in outputs:
  - `ethcallchaos-percategory-scenarios.jsonl` — top-N slowest cases per
    behavioural/shape category, selected by the generator.
  - `ethcallchaos-corpus.jsonl`, `ethcallchaos-heavy.jsonl`,
    `ethcallchaos-heavy2.jsonl` — direct exports of corpus slices from the same
    fuzzer (data only; not reproduced by the in-tree generator).
