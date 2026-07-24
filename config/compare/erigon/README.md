# Erigon rpc-tests → json-bench compare

Ported from [`erigontech/rpc-tests`](https://github.com/erigontech/rpc-tests):
the requests from its `integration/<network>` golden-file suite, converted into
json-bench `compare` configs. Because json-bench compare is **differential**
(it diffs each client's response against the others, not against a golden file),
only the requests are ported — the Erigon-captured expected responses are
dropped. What this adds over hand-written configs is coverage (100+ methods)
and **block-awareness**.

## Why block-awareness

A test's requirements depend on the block it targets. A full/head node (e.g. a
snap-synced snapshot at a recent block) keeps all block/tx/receipt/log history
but prunes historical **state**; an archive node keeps everything. The importer
buckets every request by what the node must hold, so you run the right subset:

| bucket | what it needs |
|---|---|
| `stateless` | nothing — chain-level calls (`eth_chainId`, `eth_blockNumber`, …) |
| `head` | any synced node — block tag is `latest`/`pending`/`safe`/`finalized` |
| `historical-immutable` | a **full** node — numeric/hash block, reads block/tx/receipt/log/header/raw |
| `historical-state` | an **archive** node — numeric/hash block, reads state (balance/code/call/trace/…) |
| `divergent` | informational — namespaces/methods that don't compare cleanly across clients (`erigon_`/`ots_`/`parity_`/`engine_`/`admin_`/`txpool_`/`trace_`, node-local, gas-price oracle) |

`erigon-<network>-rules.yaml` damps benign cross-client differences (ignore
`totalDifficulty`, compare JSON-RPC error codes only). Pair it with every run.

## Running

Against full/head nodes (e.g. the same-block backups), the safe subset:

```bash
for b in stateless head historical-immutable; do
  go run ./runner compare \
    --config config/compare/erigon/erigon-mainnet-$b.yaml \
    --clients config/clients/clients.yaml --client-refs nethermind,geth,reth \
    --rules config/compare/erigon/erigon-mainnet-rules.yaml \
    --output out/erigon-$b
done
```

Add `erigon-mainnet-historical-state.yaml` only when pointing at archive nodes.
`divergent` is for inspection, not pass/fail.

## Regenerating / updating

```bash
scripts/import_erigon_tests.sh mainnet     # clones rpc-tests at the pinned ref, rewrites the configs
```

Requires `python3` + `pyyaml`. Bump `RPC_TESTS_REF` in `scripts/import_erigon_tests.sh`
to pull a newer corpus. The generated files here are checked in so they're usable
without regenerating.
