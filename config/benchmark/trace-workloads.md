# Trace workloads

Seven tracing configurations. Three run against the head block and need no
history. Four are pinned to mainnet block 25,490,000.

## Head block

- `debug-trace-head.yaml`: 15 curated `debug_traceCall` requests from
  `rpc-calls/debug_traceCall-mainnet-latest.jsonl`, replayed against `latest`. They
  cover the default struct logger, `callTracer` with `onlyTopCall` on and off, and
  `prestateTracer`. Default 50 rps, 64 VUs, 120 s.
- `debug-trace-block-head.yaml`: whole-block tracing of `latest`.
  - `debug_traceBlockByNumber` with `callTracer` and with `prestateTracer`.
  - `trace_block` and `trace_replayBlockTransactions`. Clients without the
    `trace` namespace skip these two.

  Default 2 rps, 16 VUs, 120 s.

Both run on a pruned or snap-synced node.

## Pinned to block 25,490,000

These need the state of the blocks they name: a mainnet snapshot whose head is
25,490,000, or an archive. Disable synchronization, so that the pinned blocks stay
inside the node's servable window. A snapshot node often reprocesses only its last
blocks at startup. Check that every block in the range answers before you time
anything.

- `trace-transaction-prefix-25490000.yaml`: one block with 319 transactions. It
  traces the transactions at indexes 0, 159 and 318 with four methods:
  `debug_traceTransaction` (`callTracer`), `debug_traceTransaction`
  (`prestateTracer`), `trace_transaction` and `trace_replayTransaction`.
  - Each position and method is reported on its own, so the cost of replaying the
    preceding transactions shows directly.
  - Default 1 rps, 600 s.
- `trace-whole-block-25490000.yaml`: blocks 25,489,981 to 25,490,000, each traced
  with `debug_traceBlockByNumber` (`callTracer`) and with `trace_block`. Default
  2 rps, 64 VUs, 60 s.
- `trace-transaction-pool-calltracer-latency-25490000.yaml` and
  `trace-transaction-pool-calltracer-saturation-25490000.yaml`: the same 200
  transactions, `debug_traceTransaction` with `callTracer`.
  - The transactions come from blocks 25,489,901 to 25,490,000: 20 blocks, 10
    transactions per block, at index deciles.
  - The first file measures latency at 10 rps (64 VUs, 60 s).
  - The second offers 1,000 rps (512 VUs, 30 s) to find where the node saturates.
- `trace-transaction-pool-structlog-saturation-25490000.yaml`: the same pool with
  the default struct logger, at 1,000 rps.

## Reading the results

- Transaction tracing replays the transactions before the target, so latency
  grows with the transaction index. Compare positions, not only means.
- At saturation, record achieved rps and failures with the latency. A node that
  refuses requests can show a low latency for the requests it answers.
- Check that the responses are the same across clients before you compare
  timings. Some clients add fields, which changes the response size.
