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

## Curated Gnosis Chain contracts

- Source: in-tree (`scripts/generate-from-contracts/contracts-gnosis.yaml`)
- ABI provenance: Gnosis Blockscout
  (`gnosis.blockscout.com/api/v2/smart-contracts/<address>`), which needs no API
  key and reports proxy implementations in an `implementations` array. `init.sh`
  resolves single-hop proxies through it, which matters more on Gnosis than on
  mainnet: most ERC-20s there are Omnibridge-minted `PermittableToken` proxies
  that expose nothing useful at the proxy address.
- License: the contract list is our own curation under the repo's license;
  ABIs are public bytecode metadata published by the contract authors.
- Generator: `scripts/generate-from-contracts/` (same binary; select the network
  with `--config`)
- Extraction: ABI JSON only, into the same gitignored cache.
- Selection: the set is deliberately *not* a translation of the mainnet list.
  Candidates came from ranking Gnosis contracts by log volume over sampled block
  windows, which surfaces protocols with no mainnet counterpart in this corpus —
  ERC-4337 (`EntryPoint` v0.7 is the single busiest log source on the chain),
  Circles, HOPR, POAP, Monerium EURe, sDAI, the AMB/Omnibridge/xDAI bridge
  contracts and the GBC deposit contract.
- Verification: every address, method and argument was replayed against a Gnosis
  archive node via `scripts/verify-calls/`. Account-shaped arguments were taken
  from live state (top token holders, Aave suppliers, Safe owners, `latestRound()`
  ids, Balancer pool ids observed in `Swap` events) so the calls read real
  storage instead of empty slots.

## Live chain sampling

- Source: an archive JSON-RPC endpoint for the target network.
- Generator: `scripts/generate-from-chain/`
- Covers the families the geth `cmd/workload` corpora provide for mainnet only —
  blocks, receipts, transactions, logs, traces and historical state reads — for
  networks those corpora do not include.
- Extraction: block hashes, transaction hashes, log addresses and topics are
  read back from the endpoint during generation; nothing is guessed. `eth_getLogs`
  windows are probed against the node because its block-range and response-size
  caps are configuration that cannot be read over RPC.
- Checked-in outputs: `*-gnosis.jsonl` at the `rpc-calls/` top level, plus the
  `eth_getProof` fixtures under `rpc-calls/proofs/`. Proofs are held apart on
  purpose: `runner compare` drops `eth_getProof` from corpus mode, and a
  flat-state archive serves proofs only at head while a trie archive serves them
  at any height — so the head-pinned and history-pinned sets are separate files
  and must not be replayed as one set.

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

## Verification

`scripts/verify-calls/` replays any of the above corpora against a live endpoint
and fails on RPC errors, null results and empty `eth_call` returns. Run it after
regenerating a corpus — the mainnet fixtures need a mainnet archive, the
`-gnosis` fixtures a Gnosis archive.
