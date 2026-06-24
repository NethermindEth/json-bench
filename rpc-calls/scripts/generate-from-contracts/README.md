# generate-from-contracts

Mints `<slug>-mainnet.jsonl` per well-known mainnet contract. All calldata is
ABI-encoded via `go-ethereum/accounts/abi` against ABIs fetched from
Etherscan v2 — never hand-rolled. Every emitted call targets `latest`.

## Prerequisites

- `curl`, `jq`, `yq` (Mike Farah's go-yq v4)
- An Etherscan API key at `<repo root>/etherscan_api_key` (gitignored). Free
  tier is fine; ~60 requests per full run.

## Quick start

From the repository root:

```bash
bash rpc-calls/scripts/generate-from-contracts/init.sh
go run ./rpc-calls/scripts/generate-from-contracts
```

`init.sh` populates `rpc-calls/sources/contract-abis/<address>.json` for each
contract in `contracts.yaml` (gitignored). For single-hop proxies (USDC,
stETH, Aave aTokens) it resolves the implementation via Etherscan's
`getsourcecode` `Implementation` field and stores the impl's ABI keyed under
the proxy address.

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

shuffled per-contract before writing. Output files are truncated on every run.

## Adding contracts

Append an entry to `contracts.yaml` with `name`, `address`, `category`, and a
`calls:` list of `{method, args}` items. Run `bash init.sh` to cache the new
ABI, then `go run .` to emit the bucket. The generator validates every call
against the loaded ABI before writing — typos in method names, arity
mismatches, and bad arg types fail loud.

## Limitations (v1)

- Single-hop proxy resolution only. Multi-hop chains need an explicit
  `abi_address` in YAML.
- Supported arg types: `address`, `bool`, `string`, `intN`, `uintN`, `bytesN`.
  Tuples, dynamic arrays, and `bytes` (dynamic) are not supported and fail
  loudly if a YAML call requires them.
- `auto_expand: true` is reserved in the schema but not implemented.
- `chain_id != 1` is rejected.

## Context

See [`docs/research/benchmark-data-sources.md`](../../../docs/research/benchmark-data-sources.md)
for the eth_call coverage gap this addresses, and `rpc-calls/SOURCES.md` for
provenance.
