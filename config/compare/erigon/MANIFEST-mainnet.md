# Erigon rpc-tests -> json-bench compare (mainnet)

Source: erigontech/rpc-tests (source rpc-tests 214c13799371e832a90d92781f83b0fe2d143d68)

Every test is retargeted to block `latest`, so the whole suite runs on any synced node (no archive dependency).

| config | calls | notes |
|---|---:|---|
| `erigon-mainnet.yaml` | 577 | standard methods, compares cleanly across clients |
| `erigon-mainnet-divergent.yaml` | 337 | informational (noisy cross-client) |

Total runnable: 914

## Dropped (not adaptable to an arbitrary block)

Replay/state addressed only by a fixed hash — no block argument to retarget, and replaying the referenced point needs archive state:

- `debug_getModifiedAccountsByHash` (11)
- `debug_traceBlockByHash` (10)
- `debug_traceTransaction` (81)
- `ots_getInternalOperations` (15)
- `ots_getTransactionError` (15)
- `ots_traceTransaction` (22)
- `trace_replayTransaction` (30)
- `trace_transaction` (47)

## Run

```bash
go run ./runner compare --config config/compare/erigon/erigon-mainnet.yaml \
  --clients config/clients/clients.yaml --client-refs nethermind,geth,reth \
  --rules config/compare/erigon/erigon-mainnet-rules.yaml
```

For a moving-head node set `--target-block` to a fixed recent number when regenerating (default `latest` is deterministic across nodes parked at the same head).
