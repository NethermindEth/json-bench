# Ethereum JSON-RPC Benchmarking Suite

A comprehensive benchmarking framework for Ethereum JSON-RPC clients, designed to evaluate and compare the performance and behavior of different client implementations like Geth and Nethermind.

## Overview

This project runs predefined RPC tests derived from the official Ethereum Execution APIs spec, generates standardized performance metrics, checks for response consistency, and provides both historic tracking and real-time analysis through a modern web dashboard.

## Features

- **Performance Benchmarking**: Benchmark and compare Ethereum clients under realistic load with a built-in JSON-RPC load engine
- **Historic Tracking**: Store and analyze performance trends over time with PostgreSQL + Grafana integration
- **Real-time Dashboard**: Modern React UI for viewing results, trends, and comparisons
- **Response Validation**: Validate RPC response compatibility with [ethereum/execution-apis](https://github.com/ethereum/execution-apis)
- **Schema Validation**: Check responses against official Ethereum JSON-RPC specifications
- **Baseline Management**: Set performance baselines and detect regressions automatically
- **Multiple Output Formats**: Generate HTML reports, CSV exports, and JSON data
- **WebSocket Updates**: Real-time updates for live monitoring
- **Grafana Integration**: Pre-built dashboards for time-series analysis and alerting

## Project Structure

```dir-tree
json-bench/
│
├── config/                       # YAML test configurations
│   ├── clients/                  # RPC client registries
│   │   └── clients.yaml
│   ├── benchmark/                # Benchmark workload definitions
│   │   ├── mixed.yaml
│   │   └── read-heavy.yaml
│   ├── compare/                  # `runner compare` request lists
│   │   ├── defaults.yaml
│   │   └── example.yaml
│   ├── compare-openrpc/          # OpenRPC-driven comparison inputs
│   │   └── param_variations.yaml
│   └── storage/                  # Historic storage configuration
│       ├── storage-example.yaml
│       └── storage-docker.yaml
│
├── runner/                   # Go benchmark runner with historic tracking
│   ├── main.go              # Thin entry point - delegates to cmd.Execute()
│   ├── cmd/                 # Cobra subcommands (benchmark, generate-requests,
│   │                        #  api, historic, compare, compare-openrpc) plus
│   │                        #  the stubnode and promsink test fixtures
│   ├── api/                 # HTTP API server and WebSocket support
│   ├── storage/             # PostgreSQL integration
│   ├── analysis/            # Trend analysis and regression detection
│   ├── engine/              # JSON-RPC load engine and Prometheus remote write
│   └── generator/           # HTML reports
│
├── dashboard/               # React dashboard for historic analysis
│   ├── src/
│   │   ├── pages/          # Dashboard pages (trends, comparisons, baselines)
│   │   ├── components/     # Reusable UI components
│   │   └── api/            # API client for backend integration
│   └── dist/               # Built dashboard files
│
├── metrics/                 # Postgres + Prometheus + Grafana configuration
│   ├── grafana-provisioning/
│   └── dashboards/         # Pre-built Grafana dashboards
│
└── rpc-calls/               # Request corpora and the generators that build them
```

## Getting Started

### Prerequisites

- **Docker and Docker Compose** (for client nodes and infrastructure)
- **Go 1.25+** (for the benchmark runner)
- **Node.js 18+** (for the React dashboard)
- **PostgreSQL** (for historic tracking - included in Docker Compose)

### Quick Start

1. **Clone the repository:**

    ```bash
    git clone <repository-url>
    cd json-bench
    ```

2. **Start the infrastructure (PostgreSQL, Prometheus and Grafana):**

    ```bash
    # Start Grafana, PostgreSQL and Prometheus
    docker compose up -d grafana postgres prometheus

    # You can also start the runner API and the dashboard
    docker compose up -d runner dashboard

    # Or just start everything
    docker compose up -d
    ```

3. **Set up your client nodes:**

    ```bash
    # Configure your own endpoints in the config files
    nano config/clients/clients.yaml
    ```

4. **Run a benchmark:**

    ```bash
    # Basic benchmark (no historic tracking)
    go run ./runner benchmark --config ./config/benchmark/mixed.yaml --clients ./config/clients/clients.yaml
    # With historic tracking (requires PostgreSQL)
    go run ./runner benchmark --config ./config/benchmark/mixed.yaml --clients ./config/clients/clients.yaml --historic --storage-config ./config/storage/storage-example.yaml

    # View results
    open outputs/report.html
    ```

    **NOTE:** `storage-example.yaml` works out of the box with the docker containers deployed in the compose file.

5. **Access the services:**

   - **PostgreSQL**: localhost:5432 (postgres/postgres)
   - **Prometheus**: <http://localhost:9090>
   - **Grafana**: <http://localhost:3000> (admin/admin)
   - **RunnerAPI**: <http://localhost:8082> (if started)
   - **Dashboard**: <http://localhost:8080> (if started)

## Usage

The `runner` binary exposes its functionality through subcommands. Running
`runner` with no subcommand prints usage and exits with status 2.

```text
runner benchmark        Run a load test against one or more JSON-RPC endpoints
runner find-max-rps     Search for the highest rate a target sustains within an SLO
runner compare          One-shot cross-client JSON-RPC response comparison
runner compare-openrpc  Cross-client comparison driven by an OpenRPC specification
runner api              Start the HTTP API server
runner historic         Generate a historic-analysis report from PostgreSQL
```

Global flags accepted by every subcommand:

- `--output` (default `outputs/`) - where artefacts are written.
- `--log-level` - `debug`, `info`, `warn`, or `error`. Falls back to the
  `LOG_LEVEL` environment variable, then `info`.

### Basic Benchmarking

```bash
# Run a mixed workload benchmark (no Prometheus; results land in the exports)
go run ./runner benchmark --config ./config/benchmark/mixed.yaml --clients ./config/clients/clients.yaml

# Run a read-heavy benchmark
go run ./runner benchmark --config ./config/benchmark/read-heavy.yaml --clients ./config/clients/clients.yaml

# Opt into Prometheus remote-write by passing an endpoint
go run ./runner --output ./custom-results benchmark \
  --config ./config/benchmark/mixed.yaml \
  --clients ./config/clients/clients.yaml \
  --prometheus http://prometheus:9090

# Override the remote-write path (defaults to /api/v1/write)
go run ./runner benchmark \
  --config ./config/benchmark/mixed.yaml \
  --clients ./config/clients/clients.yaml \
  --prometheus http://prometheus:9090 \
  --prometheus-rw-path /custom/remote-write/path

# Generate an HTML report alongside the JSON and CSV exports (off by default)
go run ./runner benchmark \
  --config ./config/benchmark/mixed.yaml \
  --clients ./config/clients/clients.yaml \
  --html-report
```

`--prometheus` is optional and disabled by default. Omitting it skips the time
series only: the exports, the sample file and the reports are produced either
way. Passing an endpoint publishes `bench_*` series over remote write while the
run is in progress — see [metrics/METRICS.md](metrics/METRICS.md) for every
series and its meaning. An unreachable endpoint is reported before the run and
warned about on every push, and the run itself still completes; do not point
`--prometheus` at an unused port to disable it, just omit the flag.

A run writes, under `--output`:

| File | Contents |
|---|---|
| `manifest.json` | How the run was produced: engine and version, error-rate semantics, seed, saturation policy, load shape, transport settings, and each client's URL. Read this before comparing two runs. |
| `samples.jsonl.gz` | One gzipped JSON record per request: timings, phase breakdown, outcome, JSON-RPC code, byte counts. Disable with `--no-samples`. |
| `exports/results.json` | The whole result, including the manifest and the pairwise client comparison. |
| `exports/client_comparison.csv` | Per client: load delivery, outcome breakdown, latency percentiles. |
| `exports/method_metrics.csv` | Per method: full distribution statistics and outcome counts. |
| `report.html` | Opt-in via `--html-report`. |

`compare` and `compare-openrpc` always produce their HTML report.

#### Load shape and honesty about it

The engine schedules arrivals on a fixed interval and dispatches them through a
bounded pool sized by `vus`. When the pool is full at a request's scheduled
moment, `--on-saturation` decides what happens:

| Value | Behaviour |
|---|---|
| `queue` (default) | Send as soon as a slot frees, and record how late it went out. The dispatch delay is the coordinated-omission error: while it is above zero the latencies describe a slower offered rate than the one configured. |
| `drop` | Discard the request and count it. |
| `abort` | Fail the run, so a CI job cannot publish numbers from a generator that could not offer the load. |

Either way the exports and the report carry `scheduled`, `sent`, `late`,
`dropped` and the achieved rate, because a run that offered a fraction of its
requested load must not read like one that met it.

#### Warmup and ramps

The first seconds of a run measure cold caches, an empty connection pool and a
runtime that has not compiled anything yet. `warmup` applies load for a period
before the measured window opens and excludes those requests from every reported
statistic:

```yaml
duration: "5m"
warmup: "30s"
rps: 200
vus: 40
```

The warmup requests are still written to the sample file, marked `warmup`, so
nothing is thrown away — only the report's percentiles, error rate, throughput
and delivery accounting describe the measured window alone.

To move the rate rather than hold it, use `stages`. Each stage ramps linearly
from wherever the previous one left off to its `target`, with `rps` as the
starting rate; a stage whose target equals the previous one holds. Stages set
the run's length, so `duration` is omitted:

```yaml
rps: 10
vus: 200
stages:
  - duration: "1m"
    target: 500      # ramp 10 -> 500
  - duration: "3m"
    target: 500      # hold
  - duration: "30s"
    target: 0        # ramp down
```

This follows k6's `ramping-arrival-rate` shape. Warmup is the addition: k6 has
no way to discard a period from its statistics, and for a benchmark that is the
difference between reporting steady state and reporting the average of steady
state and start-up.

#### Batching

`batch_size` groups consecutive requests into JSON-RPC arrays, one HTTP round
trip each — which is how a client library with batching enabled actually talks
to a node, and what exercises Nethermind's `JsonRpc.MaxBatchSize`:

```yaml
rps: 500
batch_size: 10      # 500 requests a second, carried by 50 round trips
vus: 20
```

`rps` stays a rate of *requests*, so batches go out at `rps/batch_size` and two
runs at different batch sizes offer the node the same work. `--batch-size`
overrides the config, which is the quick way to sweep it.

What the numbers mean changes:

- **Each request's latency is its batch's latency**, because every caller in a
  batch waited the whole round trip for its answer. That is the latency they
  saw, but it means comparing methods against each other *within* a batched run
  is not meaningful — they share a duration.
- **`bench_iteration_duration_*` is the per-batch latency**, recorded once per
  round trip, while `bench_http_req_duration_*` records it once per request.
  `bench_iterations_total` counts round trips and `bench_http_reqs_total` counts
  calls.
- **A batch can fail in part.** Each member is matched to its own response by
  id and classified on its own, so a batch of ten with three reverts reports
  three `rpc_error`s. A node refusing the batch outright — which is what
  exceeding a batch limit looks like — answers with one error object instead of
  an array, and that failure is attributed to every call it carried.

#### Errors

A JSON-RPC error arrives as HTTP 200, so every response is classified into one
of `ok`, `rpc_null`, `rpc_error`, `http_error`, `truncated`, `timeout` or
`transport`. The reported error rate counts JSON-RPC errors, and `rpc_null` — a
call that succeeded and returned nothing — is counted separately rather than
folded into `ok`. `--fail-on-threshold` turns a breached `thresholds:` entry
into a non-zero exit.

### Finding a target's capacity

`benchmark` measures one rate. `find-max-rps` searches for the highest rate one
endpoint sustains while holding a service level:

```bash
go run ./runner --output outputs/capacity find-max-rps \
  --config ./config/benchmark/mixed.yaml \
  --clients ./config/clients/clients.yaml \
  --slo-p99 250 --slo-error-rate 0.5 --probe-duration 60s
```

It doubles the rate until the SLO breaks, then bisects. The config's `rps` and
`duration` are replaced per probe; everything else — the call mix, the seed,
`vus` — is used as written.

The result lands in `outputs/capacity/max-rps.json` with every probe, the
answer, and **what bounded it**:

| `limited_by` | Meaning |
|---|---|
| `slo` | The endpoint breached the SLO. `max_rps` is its capacity, bracketed below `lowest_failing_rps`. |
| `search_ceiling` | `--max-rps` was reached without a breach, so `max_rps` is a floor rather than a limit. |
| `generator` | **Inconclusive.** The generator could not offer the next rate, so the result is a property of the load generator, not the endpoint. Raise `vus` and search again. |

That last row is the reason this is trustworthy. At a rate the generator cannot
offer, latency looks terrible for reasons that have nothing to do with the node,
and a search reading only latency would report the generator's ceiling as the
node's capacity. Each probe checks delivery first and refuses to draw a
conclusion it cannot support. `--fail-on-generator-limit` turns that into a
non-zero exit for CI.

### Historic Tracking & Analysis

Enable historic tracking to store results in PostgreSQL and analyze trends over time:

```bash
# Persist this benchmark run to PostgreSQL
go run ./runner benchmark \
  --config ./config/benchmark/mixed.yaml \
  --clients ./config/clients/clients.yaml \
  --historic \
  --storage-config ./config/storage/storage-example.yaml

# Generate a historic-analysis report (no new benchmark)
go run ./runner historic \
  --config ./config/benchmark/mixed.yaml \
  --storage-config ./config/storage/storage-example.yaml
```

### API Server for Real-time Access

Start the HTTP API server for real-time data access and WebSocket updates:

```bash
# Start the API server with historic storage
go run ./runner api \
  --storage-config ./config/storage/storage-example.yaml \
  --api-addr :8081

# API will be available at http://localhost:8081
```

### One-shot Cross-Client Comparison

`runner compare` runs a one-shot JSON-RPC response comparison across a
set of clients defined in `clients.yaml`. It reads the request list from
a YAML config and is filesystem-only: results are not written to the
historic-runs database.

The compare config is a curated list of `{method, params}` calls grouped
by method. Each entry may carry an optional `id` (must be
`[a-zA-Z0-9_-]+` and unique within its method); when omitted, entries
are numbered `variant1`, `variant2`, ...

```yaml
# config/compare/example.yaml
name: "Ethereum API comparison"
description: "Cross-client sanity checks"
calls:
  eth_blockNumber:
    - params: []
  eth_getBalance:
    - id: "vitalik-latest"
      params: ["0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "latest"]
    - id: "vitalik-at-block-4096"
      params: ["0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "0x1000"]
  eth_call:
    - id: "weth-balanceOf"
      params:
        - to: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"
          data: "0x70a08231000000000000000000000000000000000000000000000000000000000000000a"
        - "latest"
```

```bash
go run ./runner compare \
  --config ./config/compare/example.yaml \
  --clients ./config/clients/clients.yaml \
  --client-refs geth,nethermind \
  --output ./comparison-results
```

These artefacts are always produced:

- `<output>/comparison-results.json` — a self-describing document:
  `{schema_version, name, description, generated_at, client_refs, summary,
  results}`. (Before `schema_version: 2` this file was a bare array of
  results, which is now the `results` field.)
- `<output>/comparison-report.html`
- `<output>/comparison-provenance.json` — the effective config (client refs,
  active rules, block override, output/retry settings, sample counts) so a
  report is self-describing and reproducible.

A `config/compare/defaults.yaml` ships with the repo and reproduces the
old baseline of eight common methods (eth_blockNumber, eth_getBalance,
eth_call, eth_getBlockByNumber, eth_getTransactionCount, eth_chainId,
eth_gasPrice, net_version) with the parameters the CLI previously used
implicitly.

#### Declaring expected differences (`comparison:` block)

Cross-client (and cross-version) runs surface many *expected* differences —
a serialization change like `totalDifficulty`, sub-percent `eth_estimateGas`
drift, or a namespace that is disabled on one node. Declare these in an
optional `comparison:` block so they stop flooding the report:

```yaml
comparison:
  # Rewrite latest/pending tags to a static block and append a block arg to
  # calls that omit one, so archive nodes at different heads are comparable.
  block_override: "0x1406f40"
  rules:
    # Treat two hex quantities as equal within abs units OR rel fraction.
    # (eth_estimateGas already gets a built-in 10% tolerance by default.)
    - method: eth_estimateGas
      path: result
      kind: numeric_tolerance
      abs: 32
      rel: 0.10
    # Drop a path entirely; `[*]` matches any array index.
    - path: result.totalDifficulty
      kind: ignore
    - method: eth_getBlockByNumber
      path: result.transactions[*].v
      kind: ignore
    # Compare only the error code, not the (benign) message wording.
    - method: eth_call
      kind: error_code_only    # also: error_presence_only
```

Rule kinds: `ignore`, `numeric_tolerance` (`abs`/`rel`), `error_code_only`,
`error_presence_only`. A rule with no `method` applies to all methods; no
`path` applies to the whole response value. Everything in the block is
optional and defaults reproduce the prior behavior.

The same block can live in a **standalone file** passed with `--rules`, which
merges over the config's own rules and works in corpus mode too:

```bash
go run ./runner compare \
  --from-jsonl ./rpc-calls --sample 400 \
  --rules ./config/compare/archive-rules.yaml \
  --block-override 0x1406f40 \
  --clients ./config/clients/clients.yaml --client-refs old,new \
  --diff-only --fail-on-diff --concurrency 4
```

#### Building a config from a corpus (`--from-jsonl`)

Instead of `--config`, point `--from-jsonl <dir>` at a corpus directory. It
recurses and reads both line-delimited `*.jsonl` files and `*.json` files
holding a JSON array of `{method, params}` objects. `--sample N` keeps at
most N calls per method (deterministic; control the seed with
`--sample-seed`). The directory may be anywhere on disk — an absolute path is
fine. Head-dependent and unstorable methods (`eth_getProof`, `eth_gasPrice`,
`eth_syncing`, `eth_blockNumber`, `eth_maxPriorityFeePerGas`, the `debug_`
namespace) are excluded; `eth_feeHistory` is kept only when a block override
pins its `newestBlock`.

A corpus tree usually holds files that are not corpora — generator inputs,
benchmark scenarios. Those are skipped with a warning naming each file and its
reason, and the run continues; the load ends with a line stating how many calls
came from how many files, how many files held only excluded methods, and how
many were skipped. The load fails only when nothing usable was found at all, so
check the warnings when a corpus you expect to work comes up short.

#### Smaller reports and CI gating

- `--diff-only` excludes identical calls. To keep the report small it also
  caps response bodies (`4096` bytes) unless you pass `--keep-response-bodies`
  to retain them or `--omit-matching-responses` to drop them entirely.
  `--max-response-bytes N` sets an explicit cap.
- The run prints a category summary: identical / differ (real) / differ
  (env/expected) / transport-error (of which rate-limited) / schema-error /
  skipped, plus per-class environment/capability error counts. A call that lost
  any client to a transport error is counted only as transport-error — it was
  never compared, so it is neither identical nor differing.
- `--fail-on-diff` exits non-zero on **real** differences only (environment/
  capability differences are excluded by default); add `--fail-on-env-diff`
  for strict mode that also fails on those, and
  `--fail-on-transport-error` so a run decimated by throttling cannot pass
  silently.
- `--skip-above-head` queries each client's head and skips calls pinned to a
  higher block, so a less-synced node does not produce false differences.

#### Robustness against throttling nodes

Transport errors, 5xx and `429 Too Many Requests` are retried with jittered
exponential backoff, honouring a `Retry-After` header when the server sends one
(capped at 30s). `--max-retries` sets the attempt budget (falls back to a
client's `max_retries` in `clients.yaml`, or 5 if unset) and
`--retry-base-delay` the base backoff. A call that still fails is recorded
per-call, classified (`rate_limited`, `timeout`, `other`), and the run
continues, so a single dead endpoint never discards an otherwise good run.

Retries alone do not absorb a *sustained* rate limit. `--rate-limit <rps>` caps
requests per second **per client**, so a throttled reference endpoint does not
hold back a fast local node; fractional rates are allowed
(`--rate-limit 2.4`). Without the flag, a client's own `rate_limit`
(`requests_per_second`, `burst`) from `clients.yaml` is enforced. Prefer this
over lowering `--concurrency`: concurrency bounds calls in flight, not the rate,
and it is shared across clients.

### OpenRPC-Driven Comparison

`runner compare-openrpc` loads the method set from an OpenRPC
specification and runs the same cross-client comparison as `compare`,
optionally expanding individual methods into per-parameter variations
defined in a YAML file. Like `compare`, it is filesystem-only and does
not write to the historic-runs database.

```bash
go run ./runner compare-openrpc \
  --spec ./openrpc.json \
  --variations ./config/compare-openrpc/param_variations.yaml \
  --clients ./config/clients/clients.yaml \
  --client-refs geth,nethermind \
  --filter eth_blockNumber,eth_getBlockByNumber \
  --output ./openrpc-results
```

Useful debug aids:

- `--filter <m1,m2,...>` - restrict the comparison to a whitelist of
  method names. Matches the base method name, so `--filter
  eth_getBlockByNumber` keeps every `eth_getBlockByNumber_variantN`
  generated from the variations file.
- `--curl` - log a curl-equivalent command for every JSON-RPC request
  the comparator makes, useful for reproducing a single call against a
  client by hand.

Both `comparison-results.json` and `comparison-report.html` are written
to `--output`.

### Migrating from the pre-refactor CLI

The flag-modal entry point (`runner -api`, `runner -historic-mode`,
single-dash long flags) has been replaced with explicit Cobra
subcommands. The old invocations will fail at flag-parse time with a
clear error. The mapping is:

| Old | New |
|---|---|
| `runner -config ... -prometheus-rw ...` | `runner benchmark --config ... --prometheus ...` |
| `runner -api -storage-config ... -api-addr ...` | `runner api --storage-config ... --api-addr ...` |
| `runner -historic-mode -storage-config ... -config ...` | `runner historic --storage-config ... --config ...` |
| `runner -historic -storage-config ...` (with `-config ...`) | `runner benchmark --historic --storage-config ...` |
| `-config`, `-clients`, `-output`, `-prometheus-rw`, ... (single dash) | `--config`, `--clients`, `--output`, `--prometheus`, ... (double dash) |
| `--prometheus-rw http://host:9090/api/v1/write` (full URL) | `--prometheus http://host:9090` (base only). Override the appended path with `--prometheus-rw-path /api/v1/write` if your deployment is non-standard. |

Additional behaviour changes worth noting:

- `--prometheus` is now optional and disabled by default. Omit it to run
  without publishing time series; pass an endpoint to opt into Prometheus.
- The benchmark `report.html` is opt-in via `--html-report`. JSON and CSV
  exports remain on by default.
- The legacy `endpoints + frequency` YAML schema is no longer accepted.
  Configs using it must be migrated by hand to the `calls:` schema (see
  `config/benchmark/mixed.yaml` for the canonical shape). No migrator is
  provided.
- The Prometheus series are now named `bench_*` rather than `k6_*`, and the
  dashboard that reads them is `benchmark-dashboard.json`. The previous one is
  kept at `metrics/dashboards/archive/k6-dashboard.json`.
  [metrics/METRICS.md](metrics/METRICS.md) maps every old name to its
  replacement.

**Available API endpoints:**

- `GET /api/runs` - List historic benchmark runs
- `GET /api/runs/:id` - Get specific run details
- `GET /api/trends` - Get performance trend data
- `GET /api/baselines` - List performance baselines
- `GET /api/compare?run1=:id1&run2=:id2` - Compare two runs
- `POST /api/runs/:id/baseline` - Set run as baseline
- `WS /api/ws` - WebSocket for real-time updates

### React Dashboard

The modern React dashboard provides an intuitive interface for analyzing benchmark results and trends:

```bash
# Install dashboard dependencies
cd dashboard
npm install

# Start development server
npm run dev
# Dashboard available at http://localhost:3000

# Build for production
npm run build
npm run preview
```

**Dashboard Features:**

- **Dashboard Page**: Overview of recent runs and performance trends
- **Run Details**: Detailed analysis of individual benchmark runs
- **Comparison View**: Side-by-side comparison of multiple runs
- **Baseline Management**: Set and manage performance baselines
- **Trend Analysis**: Interactive charts showing performance over time
- **Regression Alerts**: Automatic detection of performance regressions

### Grafana Integration

For advanced time-series analysis and alerting, you can use Grafana:

1. **Setup Grafana locally or use Docker:**

   ```bash
   docker compose up -d grafana
   ```

2. **Open Grafana**: <http://localhost:3000> (admin/admin)

3. **Provisioned data sources**:
   - **Prometheus**:
     - URL: `http://localhost:9090` (or `http://prometheus:9090` if using Docker network)
     - Access: Server (default)
   - **PostgreSQL** (for historic data):
     - Host: `localhost:5432` (or `postgres:5432` if using Docker network)
     - Database: `jsonrpc_bench`
     - User: `postgres`
     - Password: `postgres`
     - SSL Mode: `disable`

4. **Provisioned dashboards** from `metrics/dashboards/`

   - Client performance comparison
   - Method-specific latency trends
   - Error rate monitoring
   - System resource usage
   - Historic trend analysis

5. **Set up alerting** for performance regressions and system issues

#### Reading the node's own metrics

Give a client a `metrics_url` and the runner reads that endpoint during the run,
reporting what the node said about itself beside the latency it produced:

```yaml
clients:
  - name: nethermind
    url: http://127.0.0.1:8545
    metrics_url: http://127.0.0.1:9091/metrics
```

Counters are reported as their change over the run, gauges as their range, and
the selected families are republished to Prometheus as `bench_target_*` so both
sides sit on one timeline. Choose the families with `--target-metric`
(repeatable, trailing `*` matches by prefix); the default set is generic process
and runtime families, so client-specific ones need naming:
`--target-metric "nethermind_*"`.

This is a point-in-time read taken by the benchmark, not a substitute for
scraping your nodes continuously. For that, bring
your own Prometheus and point it at the node's metrics endpoint (each EL client
publishes one):

```yaml
# prometheus.yml — example for a local Geth instance
scrape_configs:
  - job_name: 'geth'
    metrics_path: /debug/metrics/prometheus
    static_configs:
      - targets: ['localhost:6060']
```

If you compose your own Prometheus alongside this stack, add the scrape
job to that config; the bundled `metrics/prometheus.yml` only handles the
runner's remote-write target.

### Storage Configuration

Configure PostgreSQL storage for historic tracking. Choose the appropriate configuration file based on your setup:

**Local Development (outside Docker):**

```bash
# Use storage-example.yaml for local PostgreSQL connection
go run ./runner benchmark --config ./config/benchmark/mixed.yaml --historic --storage-config ./config/storage/storage-example.yaml
```

**Docker Environment:**

```bash
# Use storage-docker.yaml when running inside Docker containers
# (This config uses 'postgres' hostname which only exists in Docker network)
docker run ... api --storage-config ./config/storage/storage-docker.yaml
```

**Configuration Examples:**

```yaml
# config/storage/storage-example.yaml (for local development)
historic_path: "./historic"
enable_historic: true

postgresql:
  host: "localhost"          # Use localhost when running outside Docker
  port: 5432
  database: "jsonrpc_bench"
  username: "postgres"
  password: "postgres"
  ssl_mode: "disable"
  
  grafana:
    metrics_table: "benchmark_metrics"
    runs_table: "benchmark_runs"
    retention_policy:
      metrics_retention: "30d"
      aggregated_retention: "90d"
```

```yaml
# config/storage/storage-docker.yaml (for Docker environment)
historic_path: "/app/historic"
enable_historic: true

postgresql:
  host: "postgres"           # Use service name when running in Docker
  port: 5432
  database: "jsonrpc_bench"
  username: "postgres"
  password: "postgres"
  ssl_mode: "disable"
```

## Advanced Features

### Baseline Management

Set performance baselines to detect regressions:

```bash
# Set a run as baseline via API
curl -X POST http://localhost:8080/api/runs/20250103-120000-abc123/baseline \
  -H "Content-Type: application/json" \
  -d '{"name": "Production Baseline", "description": "Post-optimization baseline"}'

# Compare current run against baseline
curl "http://localhost:8080/api/compare?run1=baseline&run2=20250103-130000-def456"
```

### Custom Test Configurations

Create custom benchmark configurations:

```yaml
# Custom benchmark configuration
test_name: "Custom Load Test"
description: "High-load test for production validation"

clients:
  - name: "geth"
    url: "http://localhost:8545"
  - name: "nethermind"
    url: "http://localhost:8546"

rps: 500
duration: "5m"

calls:
  - name: "my_method_1"
    method: "eth_blockNumber"
    weight: 20
  - name: "my_method_2"
    method: "eth_getBalance"
    params: ["0x742d35Cc641C0532a7D4567bb19f68cE3FdD72cD", "latest"]
    weight: 40
```

### Parameter Variations

Test methods with different parameter sets:

```yaml
# config/compare-openrpc/param_variations.yaml
eth_call:
  - [{"to": "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"}, "latest"]
  - [{"to": "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"}, "pending"]

eth_getBalance:
  - ["0x742d35Cc641C0532a7D4567bb19f68cE3FdD72cD", "latest"]
  - ["0x742d35Cc641C0532a7D4567bb19f68cE3FdD72cD", "pending"]
```

### Docker Deployment

Deploy the entire stack using Docker:

```bash
# Full stack deployment
docker-compose up -d
```

## Environment Variables

Configure the application using environment variables:

```bash
# Create a copy of the .env.example file
cp .env.example .env

# Edit environment configuration
nano .env
```

## Monitoring and Alerting

### Grafana Alerts

Set up alerts for performance regressions:

1. **Configure notification channels** (Slack, Discord, email)
2. **Set alert rules** for:
   - Latency increases > 20%
   - Error rate > 1%
   - Throughput decreases > 15%
3. **Enable alert evaluation** in Grafana settings

### Webhook Integration

Configure webhooks for CI/CD integration:

```bash
# Environment variables for webhook notifications
export DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/..."
export SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..."
export GITHUB_WEBHOOK_URL="https://api.github.com/repos/owner/repo/dispatches"
```

## Output Formats

The benchmark runner generates multiple output formats:

- **HTML Reports**: Interactive reports with charts and metrics
- **JSON Data**: Machine-readable benchmark results
- **CSV Exports**: For spreadsheet analysis
- **Grafana Dashboards**: Time-series visualizations
- **Historic Analysis**: Trend reports and regression detection

## Contributing

1. **Follow Go conventions** for backend code
2. **Use TypeScript** for frontend development
3. **Add tests** for new features
4. **Update documentation** for API changes
5. **Ensure backward compatibility** for configuration files

## License

This project is licensed under the MIT License - see the LICENSE file for details.
