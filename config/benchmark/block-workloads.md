# Block workloads

`eth_getBlockByNumber` workloads pinned to a mainnet snapshot whose head is
25,490,000. Disable synchronization, so that the block ages stay fixed relative
to the head.

## Pools

Each pool offers its blocks at random, at a high rate, to find where the node
saturates. Each runs for 30 s with 512 VUs.

| Config | Blocks | `fullTx` | Offered rps |
|---|---|---|---|
| `get-block-pool-hashes-25490000.yaml` | 200 | `false` | 4,000 |
| `get-block-pool-full-25490000.yaml` | the same 200 | `true` | 1,000 |
| `get-block-pool-mixed-25490000.yaml` | both pools, 400 calls | both | 4,000 |
| `get-block-pool-window-hashes-25490000.yaml` | 200 from 25,482,600-25,490,000 | `false` | 4,000 |

The 200 blocks of the hashes and full pools cover four ages:
- 40 in the last 160 blocks;
- 40 in 25,480,000-25,489,840;
- 60 in 25,000,000-25,479,999;
- 60 in 24,610,000-24,999,999.

The window pool stays just below the head, where a node is most likely to have
the block in a cache.

## Projection controls

`block-projection-controls.yaml` mixes two hashes-only fixtures at equal weight,
at 1,000 rps, 128 VUs, 60 s, with seed 7:
- `rpc-calls/block-projection-warm.json`: 200 blocks, 25,488,000-25,488,199.
  After the first pass, every read hits a warm cache.
- `rpc-calls/block-projection-rotating.json`: 20,000 blocks,
  25,450,000-25,469,999. Almost every read is a miss.

The two together give the cost of a warmed hit and of a miss on a different
block. They do not measure many requests that miss on the same block at the same
time, and they do not measure a cold OS page cache.

## Reading the results

- `fullTx: true` responses are several hundred KB, so the full pool can be bound
  by serialization and the network more than by the node.
- Some clients add fields to block responses. Compare the response size before
  you compare timings across clients.
