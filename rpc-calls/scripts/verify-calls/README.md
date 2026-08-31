# verify-calls

Replays generated `rpc-calls` JSONL fixtures against a live endpoint and reports
every request that errors or comes back empty.

A fixture a node cannot answer is not a benchmark input — it is a benchmark of
the error path, and it will quietly flatter whichever client rejects it fastest.
Run this after any generator, and after any hand-edit of a fixture file.

## Usage

```bash
go run ./rpc-calls/scripts/verify-calls \
  --rpc http://127.0.0.1:8545 \
  --input rpc-calls/contracts --suffix -gnosis
```

Exit status is `0` when everything passed, `1` when any fixture failed, and `2`
on a usage or I/O problem — so it drops into CI as-is.

## Flags

| flag | default | meaning |
| --- | --- | --- |
| `--rpc` | `http://127.0.0.1:8545` | endpoint to replay against |
| `--input` | `rpc-calls/contracts` | comma-separated files, directories or globs |
| `--suffix` | *(none)* | only files whose name contains this, e.g. `-gnosis` |
| `--concurrency` | `4` | in-flight requests |
| `--attempts` | `3` | attempts per request; only transport/decode faults retry |
| `--timeout` | `60s` | per-request timeout |
| `--allow-empty` | `false` | accept an empty `eth_call` result (`0x`) |
| `--max-report` | `25` | cap failures printed per file |

## What counts as a failure

- a transport error, a non-200 response, or an undecodable body
- a JSON-RPC `error` object
- a `null` result
- an `eth_call` returning `0x` — the address has no code, or the call reverted
  in a way the node reports as an empty return. Pass `--allow-empty` when a
  corpus intentionally probes missing state.

Transport and decode faults are retried before being reported: a node reached
through an SSH tunnel or a proxy will occasionally hand back a truncated body
under concurrency, and reporting that as a bad fixture would be a false
positive. RPC-level errors are the fixture's own fault and are never retried.

If a run over a tunnel still reports `decode:` failures on large responses
(`trace_*` especially), drop `--concurrency` to 1 and raise `--attempts` — the
tunnel, not the fixture, is the bottleneck.

## Caveat

Passing here means the endpoint answered — not that two clients agree on the
answer. For cross-client response equivalence use `runner compare` and the
configs under `config/compare/`.
