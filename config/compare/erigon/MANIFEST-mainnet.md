# Erigon rpc-tests -> json-bench compare (mainnet)

Source: erigontech/rpc-tests (source rpc-tests 214c13799371e832a90d92781f83b0fe2d143d68)

| bucket | calls | runs on |
|---|---:|---|
| `erigon-mainnet-stateless.yaml` | 8 | any node |
| `erigon-mainnet-head.yaml` | 181 | any synced node (full or archive) |
| `erigon-mainnet-historical-immutable.yaml` | 195 | full node (has block/tx/receipt/log history) or archive |
| `erigon-mainnet-historical-state.yaml` | 510 | ARCHIVE node only (full/head nodes prune historical state) |
| `erigon-mainnet-divergent.yaml` | 505 | informational — will not compare cleanly across clients; curate before use |

Total requests: 1399

Run against the 25490000 backups (full/head) with the safe buckets:

```bash
runner compare --config config/compare/erigon/erigon-mainnet-<bucket>.yaml \
  --clients config/clients/clients.yaml --client-refs nethermind,geth,reth \
  --rules config/compare/erigon/erigon-mainnet-rules.yaml
```

`stateless` + `head` + `historical-immutable` are safe on full/head nodes; `historical-state` needs an archive node; `divergent` is informational.
