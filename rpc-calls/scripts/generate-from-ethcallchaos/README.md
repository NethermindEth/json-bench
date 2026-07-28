# generate-from-ethcallchaos

Selects a diverse, worst-case `eth_call` regression set from an **EthCallChaos**
fuzzing corpus. EthCallChaos is an out-of-tree, coverage-guided fuzzer that
evolves expensive `eth_call` / Multicall3 payloads and stores each candidate,
with its measured fitness, in a SQLite corpus DB.

This generator reads that DB (read-only) and, for each behavioural/shape
category, keeps the top-N slowest cases by fitness — so a sweep replays a
diverse mix (single-contract families plus multicall fan-out buckets) instead of
the fitness-monopolising all-multicall head.

## Prerequisites

- Python 3 (standard library only — `sqlite3`, `json`).
- An EthCallChaos corpus DB. It is produced out-of-tree and is **not** vendored;
  drop it under `rpc-calls/sources/ethcallchaos/` (that `sources/` tree is
  gitignored).

## Quick start

From the repository root:

```bash
python3 rpc-calls/scripts/generate-from-ethcallchaos/main.py \
  rpc-calls/sources/ethcallchaos/corpus.db \
  rpc-calls/ \
  3
```

## Usage

```
main.py <corpus.db> <output-dir> [n-per-category]
```

| positional | default | meaning |
| --- | --- | --- |
| `<corpus.db>` | *(required)* | EthCallChaos SQLite corpus (`test_cases` table) |
| `<output-dir>` | *(required)* | where the outputs are written |
| `[n-per-category]` | `3` | top-N slowest cases kept per category |

## Categories

Cases are labelled on behavioural / shape axes (multi-label — a case is the
"worst" in every dimension it belongs to), covering the axes the fuzzer's
mutators actually move rather than protocol family:

- `overall/worst`
- `single/worst`, `single/high-gas`, `single/low-gas`, `single/large-calldata`,
  `single/value-bearing`
- `multicall/worst`, `multicall/{small,med,large,huge}`, `multicall/high-gas`

## Output

Two files are written into `<output-dir>`:

- `ethcallchaos-percategory-scenarios.jsonl` — one line per selected scenario:
  `{"name","category","rank","fitness","request"}`, where `request` is the
  reconstructed `eth_call` (Multicall3 subcalls re-encoded via `aggregate3`).
- `ethcallchaos-percategory.yaml` — a json-bench benchmark config with one named
  `eth_call` per scenario (per-name k6 metrics). Move it to
  `config/benchmark/` for use.

Both files are truncated on each run.

## Checked-in data

`ethcallchaos-percategory-scenarios.jsonl` is the checked-in product of this
generator. The sibling `ethcallchaos-corpus.jsonl`, `ethcallchaos-heavy.jsonl`,
and `ethcallchaos-heavy2.jsonl` are direct exports of corpus slices from the
same fuzzer and are **not** reproduced by this script — see
[`rpc-calls/SOURCES.md`](../../SOURCES.md) for their provenance.
