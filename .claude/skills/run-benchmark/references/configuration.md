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
duration: "1m"                   # required unless `stages` is used, Go duration syntax
warmup: "10s"                    # optional; load applied before the measured window,
                                 #   excluded from every reported statistic
stages:                          # optional; ramps the rate instead of holding it.
  - duration: "30s"              #   Sets the run's length, so `duration` must be omitted.
    target: 200                  #   Each stage ramps linearly from the previous rate
  - duration: "1m"               #   (`rps` for the first) to its target.
    target: 200
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
    thresholds:                  # optional pass/fail conditions, e.g. ["p(99)<500"] in ms
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
| `--prometheus` | unset (Prometheus disabled) | Prometheus base URL; the remote-write path is appended. Omitting it skips only the time series. An unreachable endpoint is warned about and the run still completes. See `metrics/METRICS.md` for the series. |
| `--on-saturation` | `queue` | What to do when the in-flight limit is reached at a request's scheduled time: `queue` (send late, record the delay), `drop` (discard and count), `abort` (fail the run). |
| `--fail-on-threshold` | off | Exit non-zero when a configured `thresholds:` entry is breached. |
| `--no-samples` | off | Skip the per-request sample file. |
| `--prometheus-rw-path` | `/api/v1/write` | remote-write path appended to `--prometheus` |
| `--prometheus-rw-user` / `--prometheus-rw-pass` | — | remote-write basic auth |
| `--html-report` | off | also generate `report.html` (JSON/CSV always produced) |
| `--historic` + `--storage-config` | off | persist run to PostgreSQL historic storage |

## Output layout of one run

```
<output-dir>/
  manifest.json        # how the run was produced; check before comparing runs
  samples.jsonl.gz     # one record per request (unless --no-samples)
  requests.csv         # generated RPC requests
  report.html          # only with --html-report
  exports/
    results.json           # full structured result, incl. manifest and client comparison
    method_metrics.csv     # per-method latency/error stats  <- main analysis input
                           #   Every distribution statistic is computed from the samples,
                           #   including Variance, IQR, MAD and Std Dev.
                           #   Outcome columns: Null Results, RPC Errors, HTTP Errors.
    client_comparison.csv  # per-client summary              <- main analysis input
                           #   Load delivery first: Scheduled, Sent, Late, Dropped,
                           #   Delivered (%), Target/Achieved RPS. Read these before
                           #   any latency column.
    time_series.csv
    system_metrics.csv
```

## Pre-generating the request set

`benchmark` samples requests from `calls` by weighted randomness at startup, so every invocation gets a different request set. To make runs comparable (multi-target, repeat runs), generate once and share:

```bash
./benchmark --output <dir> generate-requests --config <bench yaml> --out <dir>/requests.csv
```

Prints the CSV path (columns: id, name, method, payload). Reference it from every run's benchmark config via `calls_file`. A run with `calls_file` set uses that file verbatim and skips sampling; keep the `calls` section anyway for threshold metadata.

Which column matters: **`method` (column 3) drives the per-method breakdown.** Every request is tagged with it as `rpc_method`, and that is what `method_metrics.csv` rows are keyed on for a `calls_file` run — so the `Method` column holds real RPC methods, and `name` (column 2) is free to be any label. The file is parsed at startup, so a missing path or a malformed row fails immediately rather than minutes into the run.

## Related subcommands

- `./benchmark generate-requests --config <bench yaml> [--out <path>]` — see above.
- `./benchmark compare --config config/compare/<x>.yaml --clients <registry> --client-refs a,b` — one-shot response *correctness* comparison across clients (no load). Outputs `comparison-results.json` + `comparison-report.html`.
- `./benchmark historic --config <bench yaml> --storage-config <storage yaml>` — trend report from PostgreSQL history.
