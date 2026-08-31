# generate-from-contracts

Mints `<slug>-<network>.jsonl` per well-known contract. All calldata is
ABI-encoded via `go-ethereum/accounts/abi` against ABIs fetched from a block
explorer — never hand-rolled. Every emitted call targets `latest`.

Two networks are wired up, each with its own config file:

| network | chain_id | config | ABI provider |
| --- | --- | --- | --- |
| mainnet | 1 | `contracts.yaml` | Etherscan v2 (API key required) |
| gnosis | 100 | `contracts-gnosis.yaml` | Gnosis Blockscout (keyless) |

## Prerequisites

- `curl`, `jq`, `yq` (Mike Farah's go-yq v4)
- For mainnet only: an Etherscan API key at `<repo root>/etherscan_api_key`
  (gitignored). Free tier is fine; ~60 requests per full run. The Gnosis path
  needs no key.

## Quick start

From the repository root:

```bash
# mainnet
bash rpc-calls/scripts/generate-from-contracts/init.sh
go run ./rpc-calls/scripts/generate-from-contracts

# gnosis
bash rpc-calls/scripts/generate-from-contracts/init.sh \
  --config rpc-calls/scripts/generate-from-contracts/contracts-gnosis.yaml
go run ./rpc-calls/scripts/generate-from-contracts \
  --config rpc-calls/scripts/generate-from-contracts/contracts-gnosis.yaml
```

Then check the result against a node of that network:

```bash
go run ./rpc-calls/scripts/verify-calls --rpc <endpoint> \
  --input rpc-calls/contracts --suffix -gnosis
```

`init.sh` populates `rpc-calls/sources/contract-abis/<address>.json` for each
contract in the config (gitignored). For single-hop proxies (mainnet USDC,
stETH, Aave aTokens; almost every bridged Gnosis ERC-20) it resolves the
implementation — Etherscan's `getsourcecode` `Implementation` field on mainnet,
Blockscout's `implementations` array on Gnosis — and stores the impl's ABI keyed
under the proxy address.

`go run .` produces one JSONL per contract under `rpc-calls/contracts/`.

## Flags

| flag | default | meaning |
| --- | --- | --- |
| `--config` | `rpc-calls/scripts/generate-from-contracts/contracts.yaml` | YAML source of truth |
| `--output-dir` | `rpc-calls/contracts/` | per-contract JSONL destination |
| `--abi-cache` | `rpc-calls/sources/contract-abis` | ABI cache populated by `init.sh` |
| `--max-per-contract` | `0` | per-contract cap after shuffle; `0` = unlimited |
| `--contracts` | *(all)* | comma-separated slug whitelist |

`init.sh --refresh` deletes cached ABI files before re-fetching — useful
when an implementation behind a proxy has rotated.

## Output

Each emitted line is

```json
{"method":"eth_call","params":[{"to":"0x...","data":"0x..."},"latest"]}
```

shuffled per-contract before writing. The filename suffix comes from the
contract's `chain_id` (1 → `-mainnet`, 100 → `-gnosis`). Output files are
truncated on every run.

## Adding contracts

Append an entry to the network's YAML with `name`, `address`, `category`, and a
`calls:` list of `{method, args}` items (plus `chain_id: 100` for Gnosis). Run
`bash init.sh --config <that file>` to cache the new ABI, then `go run .` to
emit the bucket. The generator validates every call against the loaded ABI
before writing — typos in method names, arity mismatches, and bad arg types
fail loud.

The ABI check only proves a call is *encodable*. Whether it also *works* is a
question about chain state — an argument naming an account with no position, a
Chainlink round that never happened, a pool id that was never registered — so
finish by running `verify-calls` against a node and fixing anything it reports.
The shipped Gnosis set was curated that way: candidate methods were enumerated
from each cached ABI, every zero-argument view was replayed against an archive
node, and arguments were taken from live state (top token holders, Aave
suppliers, Safe owners, `latestRound()` ids, Balancer pool ids seen in Swap
events) rather than guessed.

## Adding a network

1. Add the chain id to `networkSuffix` in `main.go`.
2. Teach `init.sh` where that chain's ABIs come from (`resolve_impl` and
   `fetch_abi` both switch on `chain_id`).
3. Write a `contracts-<network>.yaml`.

## Limitations (v1)

- Single-hop proxy resolution only. Multi-hop chains need an explicit
  `abi_address` in YAML.
- Supported arg types: `address`, `bool`, `string`, `intN`, `uintN`, `bytesN`.
  Tuples, dynamic arrays, and `bytes` (dynamic) are not supported and fail
  loudly if a YAML call requires them.
- `auto_expand: true` is reserved in the schema but not implemented.
- Only the chain ids in `networkSuffix` are accepted.

## Context

See [`docs/research/benchmark-data-sources.md`](../../../docs/research/benchmark-data-sources.md)
for the eth_call coverage gap this addresses, and `rpc-calls/SOURCES.md` for
provenance.
