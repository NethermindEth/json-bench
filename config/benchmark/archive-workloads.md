# Archive workloads

Historical reads on a mainnet archive. The corpus under `rpc-calls/archive/`
comes from `rpc-calls/scripts/generate-archive/`. Its README lists the heights,
contracts and storage keys.

| Config | Corpus | Default load |
|---|---|---|
| `archive-proofs.yaml` | `proofs-spread.jsonl` (weight 1), `proofs-random.jsonl` (weight 4) | 20 rps, 16 VUs, 300 s |
| `archive-state-reads.yaml` | `state-reads.jsonl` | 50 rps, 16 VUs, 300 s |
| `archive-calls.yaml` | `calls.jsonl` | 50 rps, 8 VUs, 300 s |
| `archive-traces.yaml` | `trace-blocks.jsonl` | 1 rps, 8 VUs, 900 s |
| `archive-mixed.yaml` | proofs 20, state reads 40, `eth_call` 40 | 100 rps, 32 VUs, 300 s |

## Before a run

- Run `go run ./rpc-calls/scripts/verify-calls --rpc <node> --input rpc-calls/archive`
  against the node. It must pass. An archive that is missing part of the history
  answers with errors, and those errors are fast.
- Record the client version, the hardware, and the history and proof settings of
  the node.

## Cold and warm

Archive latency depends mostly on whether the data is in memory. Report the two
cases apart.

1. **Cold.**
   - Stop other load and drop the OS page cache on the node host
     (`sync; echo 3 > /proc/sys/vm/drop_caches`). Restart the client if it keeps
     its own cache.
   - Then run once at a low rate, so that each request is close to its first
     touch. For example, run `archive-proofs.yaml` at 1 rps for as many seconds
     as the corpus has requests.
2. **Warm.** Run the same config again right away, at the same rate.
3. **Capacity.** Raise the rate step by step. Report the highest step that
   still answers at least 99% of the requests and achieves at least 90% of the
   offered rate. Report the latency at that step.

The runner samples requests at random with replacement. A cold pass is therefore
close to, but not exactly, one touch per request. Use the same `seed` for every
client you compare.

## Reading the results

- Compare the same corpus, seed and pass on every client. A cold number against
  a warm number measures the cache, not the client.
- Count every refusal as a failure. Examples are a proof window that is not
  configured, or a range limit. A refused request is not a fast answer.
- Check full-response parity on a sample before you compare timings. A proof
  must verify against the block's state root.
