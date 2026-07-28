# Configuration reference

## Clients registry (`config/clients/*.yaml`)

Maps client names to RPC endpoints. Benchmark configs reference these names.

```yaml
clients:
  - name: "nethermind_local"     # required, unique; NO DASHES (validation rejects them)
    type: "nethermind"           # optional label; tagged onto metrics as client_type
    url: "http://localhost:8545" # required RPC endpoint
    timeout: "30s"               # optional
    max_retries: 3               # optional
    headers:                     # optional custom HTTP headers
      X-Custom: "value"
    auth:                        # optional; type: bearer | api_key
      type: "bearer"
      token: "${SOME_TOKEN}"     # env expansion supported
    rate_limit:                  # optional client-side limit
      requests_per_second: 100
      burst: 10
```

Existing registries: `config/clients/clients.yaml`, `clients-production.yaml`, `test-clients.yaml`.

## Benchmark config (`config/benchmark/*.yaml`)

```yaml
test_name: "my-benchmark"        # required
description: "..."               # optional
clients:                         # required: names from the clients registry
  - nethermind_local
duration: "1m"                   # required k6 duration ("30s", "5m", ...)
rps: 300                         # constant-arrival-rate executor...
iterations: 1000                 # ...OR shared-iterations executor (pick one)
vus: 20                          # ALWAYS set explicitly: loader requires vus > 0 and
                                 # inference from rps rounds to 0 at low rates; size for
                                 # the slowest method or the achieved rate falls short
calls:                           # the workload mix
  - name: "eth_call_erc20"       # required; used in reports/metrics
    method: "eth_call"           # inline method + params...
    params:
      - to: "0x..."
        data: "0x..."
      - "latest"
    weight: 60                   # relative weight -> request frequency
                                 # (ONLY weight is parsed; a "frequency: N%" key in older
                                 # profiles is silently ignored -> zero traffic for that call)
    thresholds:                  # optional k6 thresholds
      - "p(95) < 500ms"
  - name: "recorded_getlogs"
    file: "./rpc-calls/..."      # ...or a file of recorded calls
    file_type: "jsonl"           # json | jsonl
    weight: 40
calls_file: "./path/requests.csv"  # pre-generated requests CSV (replaces sampling from calls);
                                   # REQUIRED for comparable runs — see generate-requests below
```

Existing profiles worth reusing: `mixed.yaml`, `read-heavy.yaml`, `realistic-mix.yaml`, `realistic-mix-no-logs.yaml`, `ethcall-contracts.yaml`, `new-state-methods-head.yaml`. Several draw calls from `rpc-calls/` corpora.

## Runner invocation

```bash
go build -o benchmark ./runner

./benchmark [global flags] benchmark [benchmark flags]
```

Global flags (before the subcommand):

| Flag | Default | Meaning |
|---|---|---|
| `--output` | `outputs/` | artifact directory |
| `--log-level` | `info` | debug/info/warn/error |

`benchmark` subcommand flags:

| Flag | Default | Meaning |
|---|---|---|
| `--config` | (required) | benchmark YAML |
| `--clients` | — | clients registry YAML |
| `--prometheus` | `http://localhost:9090` | Prometheus base URL (queries + remote-write root) |
| `--prometheus-rw-path` | `/api/v1/write` | remote-write path appended to `--prometheus` |
| `--prometheus-rw-user` / `--prometheus-rw-pass` | — | remote-write basic auth |
| `--html-report` | off | also generate `report.html` (JSON/CSV always produced) |
| `--historic` + `--storage-config` | off | persist run to PostgreSQL historic storage |

## Output layout of one run

```
<output-dir>/
  config.json          # generated k6 options
  k6-script.js         # generated k6 script
  requests.csv         # generated RPC requests
  summary.json         # k6 summary
  report.html          # only with --html-report
  exports/
    results.json           # full structured result
    method_metrics.csv     # per-method latency/error stats  <- main analysis input
    client_comparison.csv  # per-client summary              <- main analysis input
    time_series.csv
    system_metrics.csv
```

## Pre-generating the request set

`benchmark` samples requests from `calls` by weighted randomness at startup, so every invocation gets a different request set. To make runs comparable (multi-target, repeat runs), generate once and share:

```bash
./benchmark --output <dir> generate-requests --config <bench yaml> --out <dir>/requests.csv
```

Prints the CSV path (columns: id, name, method, payload). Reference it from every run's benchmark config via `calls_file`. A run with `calls_file` set uses that file verbatim and skips sampling; keep the `calls` section anyway for name/threshold metadata.

## Related subcommands

- `./benchmark generate-requests --config <bench yaml> [--out <path>]` — see above.
- `./benchmark compare --config config/compare/<x>.yaml --clients <registry> --client-refs a,b` — one-shot response *correctness* comparison across clients (no load). Outputs `comparison-results.json` + `comparison-report.html`.
- `./benchmark historic --config <bench yaml> --storage-config <storage yaml>` — trend report from PostgreSQL history.
