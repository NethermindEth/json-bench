# generate-erigon-compare

Ports the upstream [`erigontech/rpc-tests`](https://github.com/erigontech/rpc-tests)
`integration/<network>/` corpus into json-bench `compare` configs under
`config/compare/erigon/`.

Unlike the sibling [`generate-from-erigon-rpc-tests`](../generate-from-erigon-rpc-tests)
(which mints `*.jsonl` load-test fixtures for a handful of methods), this
generator produces the **differential** `compare` suite: every request in the
corpus, retargeted so it runs on any synced node.

## Quick start

From the repository root:

```bash
bash rpc-calls/scripts/generate-erigon-compare/init.sh mainnet
go run ./rpc-calls/scripts/generate-erigon-compare \
  --source-ref "$(cat rpc-calls/sources/erigon-rpc-tests/SOURCE_REF)"
```

`init.sh` sparse-clones only `integration/<network>` at a pinned ref into
`rpc-calls/sources/erigon-rpc-tests/` (gitignored) and records the resolved
commit SHA in `SOURCE_REF`. Pass that SHA via `--source-ref` so it is stamped
into the generated descriptions and manifest.

## What it does

`compare` is differential — each client's response is diffed against the
others, not against a golden file — so only the requests are kept; the
Erigon-captured expected responses are dropped. Every test's block reference is
rewritten to a runnable target (`latest` by default) so the whole suite runs
against a normal synced node with no archive dependency:

- block-number / block-tag arguments → the target (per-method arg map);
- `eth_getLogs` `fromBlock`/`toBlock` → the target;
- requests that only *fetch* immutable data by hash (`eth_getBlockByHash`,
  receipts, …) are kept as-is;
- requests that *replay* a fixed point by hash (`trace_transaction`,
  `debug_traceTransaction`, `debug_traceBlockByHash`, …) have no block argument
  to retarget and are **dropped** (tallied in the manifest).

## Flags

| flag | default | meaning |
| --- | --- | --- |
| `--source` | `rpc-calls/sources/erigon-rpc-tests/integration/mainnet` | upstream corpus root |
| `--out` | `config/compare/erigon` | output directory |
| `--network` | `mainnet` | name used in output file names and descriptions |
| `--target-block` | `latest` | block tag/number every test is retargeted to |
| `--source-ref` | *(empty)* | upstream git ref recorded for provenance |

## Output buckets

| file | contents |
| --- | --- |
| `erigon-<network>.yaml` | core standard `eth_`/`debug_` methods — compare cleanly across clients |
| `erigon-<network>-divergent.yaml` | non-standard namespaces / node-local / oracle methods (`erigon_`/`ots_`/`parity_`/`engine_`/`admin_`/`txpool_`/`trace_`) — informational, noisy cross-client |
| `erigon-<network>-rules.yaml` | damps benign cross-client differences (ignore `totalDifficulty`, compare error codes only) |
| `MANIFEST-<network>.md` | request counts per bucket and the list of dropped methods |

Duplicate `(method, params)` requests are collapsed within each bucket. The
generated files are checked in, so they are usable without regenerating.

## Context

See [`../../SOURCES.md`](../../SOURCES.md) for the upstream provenance and
[`config/compare/erigon/README.md`](../../../config/compare/erigon/README.md)
for how to run the suite.
