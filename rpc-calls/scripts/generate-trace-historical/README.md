# generate-trace-historical

Mints the historical tracing corpus from a live node, over a block range that
node can actually answer.

The checked-in corpora under `rpc-calls/trace-historical/` pin block ranges deep
in mainnet history, so they need a node that carries that history. A node
brought up by syncing to the tip and keeping history from there on has no state
below its floor: every one of those fixtures fails there, correctly, and the run
measures nothing. This generator finds the floor itself and samples above it.

## Quick start

```bash
go run ./rpc-calls/scripts/generate-trace-historical \
  --rpc http://127.0.0.1:8545 --write-config config/benchmark/trace-mine.yaml

go run ./runner benchmark \
  --config config/benchmark/trace-mine.yaml --clients <your clients.yaml>
```

Run it against the node you are about to benchmark. With `--write-config` it
writes the benchmark config as well, already pointing at the corpus it just
minted, so nothing is edited by hand. Without it, point a copy of
`config/benchmark/trace-transaction-historical.yaml` at the directory it
reports.

## Flags

| flag | default | meaning |
| --- | --- | --- |
| `--rpc` | `http://127.0.0.1:8545` | endpoint to mint the corpus from |
| `--output-dir` | `rpc-calls/trace-historical` | parent directory; the corpus lands in `blocks-<lowest>-<highest>` under it |
| `--blocks` | `20` | blocks to sample, one request per block per family |
| `--min-tx` | `50` | skip blocks with fewer transactions than this |
| `--head-lag` | `256` | stay this far behind head so the fixtures survive reorgs |
| `--from` | `0` | lowest block to sample; `0` discovers the node's floor |
| `--to` | `0` | highest block to sample; `0` uses head minus `--head-lag` |
| `--seed` | `1` | PRNG seed, so the same node and range mint the same corpus |
| `--write-config` | `` | also write a benchmark config pointing at the corpus |
| `--client` | `nethermind` | client name the written config benchmarks; must match an entry in your `clients.yaml` |
| `--rps` | `1` | offered rate per call; a comma-separated list writes one config per rate |
| `--vus` | `16` | concurrent virtual users the written config allows |
| `--duration` | `600s` | how long the written config runs each rate |
| `--timeout` | `120s` | per-request timeout |
| `--attempts` | `4` | attempts per request; only transport faults are retried |

## Throughput

One rate measures latency under a fixed offered load. To find where a node
saturates, pass several and run them in turn:

```bash
go run ./rpc-calls/scripts/generate-trace-historical \
  --rpc http://127.0.0.1:8545 --rps 10,25,50,100,200 --duration 60s \
  --write-config config/benchmark/trace-mine.yaml

for config in config/benchmark/trace-mine-rps*.yaml; do
  go run ./runner benchmark --config "$config" --clients <your clients.yaml>
done
```

Each rate gets its own config, suffixed with it, since the rate is the only
thing that differs between the runs being compared. Saturation shows up as the
first rate where errors appear or p99 leaves the trend.

Raise `--vus` alongside the rate when the calls are slow: k6 cannot offer more
requests per second than its virtual users can hold open, so a whole-block trace
taking a second caps at one request per second per user however high the rate is
set. The reported request rate, not the configured one, is what the node served.

## Output

Five files, the same names and request shapes as the checked-in corpus:

| file | request |
| --- | --- |
| `debug_traceTransaction-callTracer.jsonl` | the block's last transaction |
| `debug_traceTransaction-prestateTracer.jsonl` | the same transaction |
| `trace_transaction.jsonl` | the same transaction |
| `trace_replayTransaction-trace-stateDiff.jsonl` | the same transaction, `["trace","stateDiff"]` |
| `debug_traceBlockByHash-callTracer.jsonl` | the whole block, as the control |

The last transaction of a block is the worst case for a node that has to replay
the transactions ahead of the one asked for, which is what makes it the request
worth measuring.

## How the floor is found

One `eth_getBalance` at block 1. A full archive answers it and the search stops
there. A node keeping a window answers only above its floor, so the search
falls back to a binary search between block 1 and the sampling ceiling, about
twenty-five probes.
