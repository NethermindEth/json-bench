# Head control workloads

Two small workloads. Each isolates one request path on a mainnet snapshot whose
head is 25,490,000. Disable synchronization, so that `latest` stays fixed.

## Raw transaction lookup

`raw-transaction-snapshot.yaml` offers `debug_getRawTransaction` at 1,000 rps
(40 VUs, 60 s) over `rpc-calls/debug_getRawTransaction-snapshot.json`.
- The fixture holds the 286 transactions of block 25,489,872, which is 128 blocks
  below the head.
- The node needs its transaction lookup index for that block.
- After warm-up, the block is resident in the cache. The workload measures the
  lookup and the read of the stored transaction, not cold-block capacity.

## Empty block overrides

`empty-block-override-head.yaml` offers a WETH `balanceOf` read at `latest` at
1,000 rps (40 VUs, 60 s), through `eth_call` and `eth_estimateGas`.
- Half of the requests pass an empty block override.
- The other half pass no overrides and act as controls.

An empty override changes nothing, so both halves must return the same result. A
latency difference between them is the cost of handling the override itself. It
is not the cost of real state or block override traffic.
