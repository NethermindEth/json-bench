# Log and receipt workloads

`eth_getLogs` and receipt workloads over fixed mainnet blocks 25,390,001 to
25,406,300. Use a node that keeps receipts for that range. For the indexed
results, the node also needs a log index that covers the range.

Disable synchronization, and check that the index covers the whole range before
you time anything. A log index that is still being built makes the same query
take a different path.

## Benchmarks

| Config | Fixtures | Requests | Default load |
|---|---|---|---|
| `log-data-filtering.yaml` | `log-data-selective-1000.json`, `log-data-selective-10000.json`, `log-data-all-match-block.json` | 64 each | 1 rps, 128 VUs, 60 s |
| `log-data-all-match-control.yaml` | `log-data-all-match-block.json` | 64 | 100 rps, 128 VUs, 120 s |
| `log-data-huge.yaml` | `log-data-huge-indexed.json`, `log-data-huge-unfiltered.json` | 16 each | 1 rps, 128 VUs, 60 s |
| `materialized-log-responses.yaml` | `log-response-8-blocks.json`, `log-response-16-blocks.json` | 32 each | 5 rps, 128 VUs, 180 s |
| `log-data-receipts.yaml` | `log-data-receipts-block.json`, `log-data-receipts-tx.json` | 64 and 128 | 1 rps, 128 VUs, 60 s |

What each fixture asks for:

- **Selective scans.** WETH `Approval` logs for one fixed owner, an indexed topic.
  `log-data-selective-1000.json` covers 1,000-block ranges. `log-data-selective-10000.json`
  covers 10,000-block ranges. Few logs match, so most of the cost is finding the
  matching blocks.
- **All-match blocks.** Every log of one block, for 64 blocks from 25,400,000
  on. This measures the cost of reading and serializing logs, with no filtering.
- **Large responses.**
  - `log-data-huge-indexed.json`: USDT `Transfer` over 96 blocks, through the
    indexed path.
  - `log-data-huge-unfiltered.json`: every log over 10 blocks.
  - Each response holds 4,000 to 18,000 logs, which stays below a 20,000-log
    response cap.
- **Materialized responses.** Every log over 8 and 16 blocks from 25,400,000 on.
  This measures how the node builds and sends a large response.
- **Receipts.** `eth_getBlockReceipts` for 64 blocks and
  `eth_getTransactionReceipt` for 128 transactions in the same range.

## Parity

`config/compare/` has a full-response parity config for four of the workloads:
- `log-data-filtering.yaml`: 192 queries;
- `log-data-huge.yaml`: 32;
- `materialized-log-responses.yaml`: 64;
- `log-data-receipts.yaml`: 192.

Run parity before a timing comparison. A faster response that leaves out logs is
not a result.

## Reading the results

- Response size grows with the number of logs, so compare latency per fixture,
  not across fixtures.
- Some clients refuse ranges above a configured block or log cap. Count refusals
  as failures; they are not fast answers.
