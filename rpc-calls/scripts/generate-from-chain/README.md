# generate-from-chain

Samples a live archive endpoint and mints the fixture families that are *not*
`eth_call`: blocks, receipts, transactions, logs, traces and historical state
reads.

The mainnet equivalents of these files come from geth's `cmd/workload` corpora
(see `generate-from-history`, `-filter`, `-traces`). Those corpora exist only
for mainnet, so any other network has to derive its inputs from the chain
itself — which is what this generator does.

## Quick start

From the repository root, pointed at a **Gnosis archive** node:

```bash
go run ./rpc-calls/scripts/generate-from-chain \
  --rpc http://127.0.0.1:8545 --network gnosis --samples 250

go run ./rpc-calls/scripts/verify-calls \
  --rpc http://127.0.0.1:8545 --input rpc-calls --suffix -gnosis
```

## Flags

| flag | default | meaning |
| --- | --- | --- |
| `--rpc` | `http://127.0.0.1:8545` | archive endpoint to sample |
| `--network` | `gnosis` | filename suffix |
| `--output-dir` | `rpc-calls/` | destination for `<family>-<network>.jsonl` |
| `--samples` | `200` | blocks to sample |
| `--oldest-block` | `1` | lowest height to sample from |
| `--head-lag` | `128` | stay this far behind head so fixtures survive reorgs |
| `--log-window` | `1000` | ceiling on `eth_getLogs` range width |
| `--seed` | `0` | PRNG seed; `0` picks one from the clock |
| `--trace` | `true` | emit `trace_*` fixtures |
| `--proof` | `true` | emit `eth_getProof` fixtures into `--proof-output-dir` |
| `--proof-output-dir` | `rpc-calls/proofs/` | separate destination for the proof fixtures |
| `--proof-slots` | `3` | populated storage slots to prove per account |

## Output

| file | methods |
| --- | --- |
| `historical-blocks-<net>.jsonl` | `eth_getBlockByNumber`, `eth_getBlockByHash`, `eth_getBlockTransactionCountByNumber` |
| `eth_getBlockReceipts-<net>.jsonl` | `eth_getBlockReceipts` |
| `transactions-<net>.jsonl` | `eth_getTransactionByHash`, `eth_getTransactionReceipt` |
| `get-logs-<net>.jsonl` | `eth_getLogs` — unfiltered windows, by address, by address+topic0 |
| `eth_getCode-<net>.jsonl` | `eth_getCode` at a historical height |
| `eth_getStorageAt-<net>.jsonl` | `eth_getStorageAt` at a historical height |
| `eth_getBalance-<net>.jsonl` | `eth_getBalance` at a historical height |
| `eth_getTransactionCount-<net>.jsonl` | `eth_getTransactionCount` at a historical height |
| `traces-<net>.jsonl` | `trace_block`, `trace_replayBlockTransactions`, `trace_transaction` |

`eth_getProof` is written **outside** `--output-dir`, under `rpc-calls/proofs/`:

| file | contents |
| --- | --- |
| `eth_getProof-<net>-latest.jsonl` | head-targeted; any node can answer these |
| `eth_getProof-<net>.jsonl` | pinned heights; needs a **trie** archive |

## Why proofs are kept separate

Three reasons, and they compound:

1. **`runner compare` excludes `eth_getProof` from corpus mode** entirely — a
   node is not obliged to store proofs. A proof fixture sitting in the main
   corpus directory is silently dropped from every comparison, so keeping it
   there just inflates the file count without being tested.
2. **The two shapes are not interchangeable.** A flat-state archive answers
   proofs only at the head (`State proofs at historical block N are not
   supported`); a trie archive answers them at any height. Mixing head-pinned
   and history-pinned proofs in one file guarantees that one node fails half of
   them for a reason that is configuration, not correctness.
3. **Head-targeted proofs are not reproducible.** They read whatever state is
   current, so two nodes a block apart legitimately disagree.

The generator probes the endpoint before deciding: it issues one historical
`eth_getProof` and only emits the pinned set if that succeeds. Storage keys are
chosen by scanning slots for a non-zero value, because proving an empty slot
returns an exclusion proof that barely touches the storage trie — the populated
slots are where the work is.

To compare proofs across nodes, use a curated config rather than `--from-jsonl`:

```bash
go run ./runner compare --config config/compare/gnosis-proofs.yaml \
  --clients <clients.yaml> --client-refs trie_archive,flat_history
```

## How correctness is enforced

Every identifier in the output came back from the endpoint during generation:
block hashes and transaction hashes are read out of sampled blocks, log
addresses and topics are read out of real logs, and `eth_getCode` is checked
before an address is used as a state-read target. Nothing is guessed.

Node-side limits are discovered rather than assumed. `eth_getLogs` is capped in
two independent ways that are pure node configuration — a maximum block range
and a maximum number of logs per response — and neither is readable over RPC.
So the generator widens an unfiltered scan from a single block until the node
refuses and keeps the last width that worked, and it replays every
address/topic query before emitting it. A query the node rejects is dropped
with a warning instead of being written out.

Two families are pinned rather than sampled across history:

- **`eth_getProof` targets `latest` only.** A flat-state archive keeps the
  state trie for the head and answers `State proofs at historical block N are
  not supported` for anything older.
- **Block sampling is biased towards older heights** (square-law). A uniform
  draw over a 48M-block chain would put nearly every sample in the recent,
  cheaply-indexed range.

`debug_*` fixtures are deliberately absent: the namespace has to be enabled on
the endpoint, and it is off by default on the nodes this was built against.
When it is available, `generate-from-erigon-rpc-tests` is the source for those.

## Caveat: corpus mode mixes networks

`runner compare --from-jsonl ./rpc-calls` recurses the whole corpus directory
and has no network filter, so it will pick up `-mainnet` and `-gnosis` fixtures
in the same run. Point `--from-jsonl` at a directory holding only one network's
files, or pass an explicit `--config`.

## Idempotency

Output files are truncated on each run. Pass `--seed` for a reproducible
sample; without it each run draws fresh blocks.
