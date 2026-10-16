# generate-archive

Writes the mainnet archive corpus under `rpc-calls/archive/`: historical
`eth_getProof`, state reads, `eth_call` and block tracing. It needs no node. The
heights are fixed or seeded, so the same flags always give the same files.

This rebuilds the request sets of earlier archive measurements from their
recorded method. Those runs used their own scripts, which were not kept. The
families, heights, seeds and contracts follow that method; the exact requests
are new.

## Quick start

```bash
go run ./rpc-calls/scripts/generate-archive

go run ./rpc-calls/scripts/verify-calls \
  --rpc http://127.0.0.1:8545 --input rpc-calls/archive
```

Run `verify-calls` against the archive you are about to benchmark before you
time anything. It fails on RPC errors and null results, so a node that does not
hold the history is caught before the run, not after it.

## Families

| File | Requests | Heights |
|---|---|---|
| `proofs-spread.jsonl` | `eth_getProof` for WETH, USDC, USDT | x × 1,000,000 + 333,333 for x = 1..25 |
| `proofs-random.jsonl` | `eth_getProof` for WETH, USDC, USDT, DAI, LINK | 60, seed 13, 4,500,000..`--max-block` |
| `state-reads.jsonl` | `eth_getBalance` of the holder, `eth_getStorageAt` of its balance entry in the five tokens | 60, seed 19, 4,500,000..`--max-block` |
| `calls.jsonl` | `eth_call` of `balanceOf(holder)` and `totalSupply()` on WETH, USDC, USDT, from the zero address | 120, seed 21, 6,500,000..`--max-block` |
| `trace-blocks.jsonl` | `debug_traceBlockByNumber` (`callTracer`) and `trace_block` | 25, evenly over `--trace-from`..`--max-block` |

How the storage keys are built:
- Each proof asks for two keys: slot 0, and the holder's balance entry.
- The balance entry is `keccak256(pad32(holder) ++ pad32(slot))`. The balance
  mapping slot is 3 for WETH, 9 for USDC, 2 for USDT, 2 for DAI and 1 for LINK.
- Each slot was checked on mainnet: `eth_getStorageAt` of the derived key equals
  `balanceOf(holder)`.
- Keys are written in full 32-byte form, because some clients reject short hex
  such as `0x0`.

Some spread heights are below a token's deployment block. There the proof is for
an account that does not exist yet. An archive must answer it with an absence
proof.

## Flags

| flag | default | meaning |
| --- | --- | --- |
| `--output-dir` | `rpc-calls/archive` | directory the corpus is written to |
| `--max-block` | `25900000` | highest block a seeded or traced height may take |
| `--proof-seed` | `13` | seed of the random proof heights |
| `--state-seed` | `19` | seed of the state-read heights |
| `--call-seed` | `21` | seed of the `eth_call` heights |
| `--trace-from` | `15123456` | lowest traced block |
| `--trace-blocks` | `25` | traced blocks |
