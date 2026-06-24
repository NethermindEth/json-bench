# generate-from-erigon-rpc-tests

Mints `<method>-mainnet.jsonl` (and `<method>-mainnet-latest.jsonl`) fixtures
from the upstream [`erigontech/rpc-tests`](https://github.com/erigontech/rpc-tests)
`integration/mainnet/` corpus. Pure pass-through: one upstream method → one
output bucket of the same name.

## Quick start

From the repository root:

```bash
bash rpc-calls/scripts/generate-from-erigon-rpc-tests/init.sh
go run ./rpc-calls/scripts/generate-from-erigon-rpc-tests
```

`init.sh` downloads only the per-method JSON files this script consumes (no
full clone), depositing them under `rpc-calls/sources/erigon-rpc-tests/`. That
`sources/` tree is gitignored.

The flag defaults below assume the project root as the working directory; pass
`--source` and `--output-dir` explicitly to run from somewhere else.

## Flags

| flag | default | meaning |
| --- | --- | --- |
| `--source` | `rpc-calls/sources/erigon-rpc-tests/integration/mainnet` | upstream corpus root |
| `--output-dir` | `rpc-calls/` | where to write `<method>-mainnet[-latest].jsonl` |
| `--methods` | *(all subdirs of source)* | comma-separated method whitelist |
| `--max-per-method` | `0` | per-bucket cap; `0` = unlimited |

## Bucketing

Each upstream fixture is routed by `metadata.latest`:

- `metadata.latest == true` → `<method>-mainnet-latest.jsonl` (re-targeted to
  `latest` at run time by the consumer).
- anything else → `<method>-mainnet.jsonl` (pinned block height in `params`).

Output lines are `{"method": "...", "params": [...]}`; everything else from the
upstream fixture (response, test description, metadata) is discarded.

## Idempotency

Output files are truncated on each run. `init.sh` re-fetches into the same
`sources/` tree, so re-running gives you a fresh build from current upstream.

## Context

See [`docs/research/benchmark-data-sources.md`](../../../docs/research/benchmark-data-sources.md)
for why this source was picked and how it fits with the other corpora.
