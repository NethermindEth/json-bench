# Erigon rpc-tests → json-bench compare

Ported from [`erigontech/rpc-tests`](https://github.com/erigontech/rpc-tests):
the requests from its `integration/<network>` golden-file suite, converted into
json-bench `compare` configs. json-bench compare is **differential** (each
client's response is diffed against the others, not against a golden file), so
only the requests are ported — the Erigon-captured expected responses are
dropped. What this adds over hand-written configs is coverage (100+ methods)
and that **every ported test is runnable on any synced node**.

## Any-block, no archive dependency

The Erigon tests pin specific historical blocks (e.g. `eth_getBalance` at block
17,000,000), which only an archive node can answer. The importer rewrites every
test's block reference to a runnable target (`latest` by default), so the whole
suite runs against a normal synced node:

- block-number / block-tag arguments → the target (`latest`);
- `eth_getLogs` `fromBlock`/`toBlock` → the target;
- requests that only *fetch* immutable data by hash (`eth_getBlockByHash`,
  `eth_getTransactionByHash`, receipts, …) are kept as-is — any full node
  retains that data;
- requests that *replay* a fixed point by hash (`trace_transaction`,
  `debug_traceTransaction`, `debug_traceBlockByHash`, …) have no block argument
  to retarget and need archive state, so they are **dropped** (listed in
  `MANIFEST-<network>.md`).

Because compare sends the same request to every client at once, `latest` is
deterministic when the nodes are parked at the same head. For a moving-head or
archive node, regenerate with `--target-block <fixed number>`.

## Files

| config | contents |
|---|---|
| `erigon-<network>.yaml` | core standard `eth_`/`debug_` methods — compares cleanly across clients |
| `erigon-<network>-divergent.yaml` | non-standard namespaces / node-local / gas-price-oracle methods (`erigon_`/`ots_`/`parity_`/`engine_`/`admin_`/`txpool_`/`trace_`) — informational, noisy cross-client |
| `erigon-<network>-rules.yaml` | damps benign cross-client differences (ignore `totalDifficulty`, compare error codes only) — pair with every run |

## Running

```bash
go run ./runner compare \
  --config config/compare/erigon/erigon-mainnet.yaml \
  --clients config/clients/clients.yaml --client-refs nethermind,geth,reth \
  --rules config/compare/erigon/erigon-mainnet-rules.yaml \
  --output out/erigon
```

`-divergent.yaml` is for inspection, not pass/fail.

## Regenerating / updating

```bash
scripts/import_erigon_tests.sh mainnet        # clones rpc-tests at the pinned ref, rewrites the configs
RPC_TESTS_REF=<sha> scripts/import_erigon_tests.sh mainnet   # pull a newer corpus
```

Requires `python3` + `pyyaml` (dev-time only). The generated files here are
checked in so they're usable without regenerating.
