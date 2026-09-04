---
name: run-benchmark
description: Run an Ethereum JSON-RPC benchmark with this repository's Go runner (built-in JSON-RPC load engine, with reports and cross-client analysis). Use this skill whenever the user asks to run a benchmark, load-test or stress-test an RPC endpoint, measure JSON-RPC latency or throughput, or compare the performance of Ethereum clients (Nethermind, Geth, Reth, Erigon, etc.) — even if they don't use the word "benchmark".
---

# Run a JSON-RPC Benchmark

This repository benchmarks Ethereum JSON-RPC endpoints: a Go runner (`./runner`) drives a load test from a YAML config against one or more RPC endpoints and writes reports (JSON/CSV always, HTML on request) plus per-method and per-client metric exports. The load generator is built in — there is nothing to install alongside it.

The process below is the general shape of a run. The user's specific instructions always take precedence — if they name a config, a load profile, a host, or a workflow detail, follow it instead of the defaults here.

## Step 1: Establish the run parameters

From the user's request, pin down:

- **Targets** — which RPC endpoint(s)/client(s) to benchmark.
- **Workload** — which RPC methods and mix. Check `config/benchmark/` first: it has ready-made profiles (`mixed.yaml`, `read-heavy.yaml`, `ethcall-contracts.yaml`, `realistic-mix.yaml`, ...). Reuse one when it matches; generate a new config only when the user's demands don't fit an existing one.
- **Load shape** — `duration` plus either `rps` (constant arrival rate) or `iterations` (shared iterations), and optionally `vus`.
- **Prometheus export** — the runner can remote-write its `bench_*` metrics to a Prometheus instance for later visualization in Grafana. If the user hasn't said either way, ask: *"Should this run export metrics to Prometheus for later visualization, or is the generated file output enough?"* Don't silently assume either.

Only ask about what's genuinely unspecified and consequential; fill the rest with sensible defaults and state them.

## Step 2: Decide where to run

Network distance distorts latency measurements, so run the benchmark as close to the target RPC as possible:

1. **On the target host via SSH** (preferred). If the target is a remote node, check whether SSH access exists (ask the user if unclear). If it does, run the benchmark *on the node* against its local RPC (`http://127.0.0.1:8545` or equivalent). Read `references/remote-runs.md` for the full on-host procedure. Check `scripts/` for existing remote-orchestration helpers before writing your own.
2. **Locally** (fallback). If remote access isn't possible, run from this machine against the remote URL. Record in the final analysis that results include network latency between this machine and the target.

## Step 3: Prepare the configuration

Two YAML files drive a run — a **clients registry** (`config/clients/*.yaml`) mapping client names to RPC URLs, and a **benchmark config** (`config/benchmark/*.yaml`) defining the workload. Schemas and examples are in `references/configuration.md`; read it before generating configs.

Rules that bite if missed:

- Client `name` values must not contain dashes (registry validation rejects them); use underscores.
- The `clients` list in the benchmark config references registry names, not URLs.
- Always set `vus` explicitly; the loader requires `vus > 0`. Size it for the slowest method in the mix — a handful of slots saturated by multi-second calls (e.g. `eth_getLogs`) cannot offer a high rate. The runner now reports the shortfall rather than delivering less in silence: check `Dropped`, `Delivered (%)` and `Achieved RPS` in `client_comparison.csv`, or pass `--on-saturation=abort` to fail the run instead.
- Only `weight` drives call frequency. Unknown YAML keys are now rejected, so a legacy `frequency:` or a typo fails at load time instead of silently giving that call zero traffic.
- Generated configs for a specific run belong next to the run's outputs (or a scratch path), not committed into `config/` — that directory is for reusable profiles.

**Same requests everywhere.** The runner builds the request set by weighted random sampling, so two runs of the same config send *different* requests — which makes their results incomparable. Before running any benchmark command, build the runner once, then pre-generate the request set and reuse it in every run:

```bash
go build -o benchmark ./runner

./benchmark --output outputs/<benchmark-name>/ generate-requests \
  --config <benchmark-config>.yaml --out outputs/<benchmark-name>/requests.csv
```

then point every run's benchmark config at it with `calls_file: <path>/requests.csv`. This is mandatory whenever results will be compared — across targets, across hosts, or across repeat runs — and a good default even for a single target (it makes the run reproducible). The per-method breakdown keys on the CSV's `method` column (column 3), so its `name` column can be any label; see `references/configuration.md`.

**Multiple targets.** Follow the user's instructions for how to handle them. The runner natively benchmarks several registry clients in one run, sharing one request sequence and one arrival schedule, which works when benchmarking from a single vantage point. But for on-host SSH runs each target needs its own individual run on its own host — same workload config, same pre-generated `calls_file`, only the clients registry differs. Manage them one at a time and aggregate afterwards.

## Step 4: Execute

The runner is already built (step 3). Run per the plan:

```bash
./benchmark --output outputs/<benchmark-name>/ benchmark \
  --config <benchmark-config>.yaml \
  --clients <clients-registry>.yaml \
  --html-report
```

- **Output location**: everything goes under the git-ignored `outputs/` directory, in a subdirectory dedicated to this benchmark (e.g. `outputs/<date>-<short-name>/`). With multiple individual runs, give each target its own subdirectory inside it (`outputs/<benchmark-name>/<client>/`).
- **Prometheus on**: start the local stack if needed (`docker compose up -d prometheus grafana`) and pass `--prometheus http://localhost:9090`. For on-host runs, metrics reach the local Prometheus through an SSH reverse tunnel (see `references/remote-runs.md`).
- **Prometheus off**: just omit `--prometheus`. Omitting it skips only the time series; the exports, sample file and reports are produced either way. Do **not** point it at an unused port to "turn it off" — that publishes to a dead address and only produces warnings.
- Sanity-check the endpoint before a long run (e.g. an `eth_blockNumber` curl) so a dead target fails in seconds, not after the full duration.

Verify each run produced its artifacts (`manifest.json`, `samples.jsonl.gz`, `exports/results.json`, `exports/client_comparison.csv`, `exports/method_metrics.csv`, `report.html` if requested) before moving on.

## Step 5: Collect and analyze

The deliverable is the benchmark's output directory containing every run's reports **plus a final analysis document** you write:

- Create `outputs/<benchmark-name>/ANALYSIS.md`.
- **First state whether each run actually offered its requested load** — `Scheduled`, `Sent`, `Dropped`, `Delivered (%)` and `Achieved RPS` in `client_comparison.csv`. A run below 100% delivered has latency figures that describe only the requests that were sent, and any comparison against it must say so.
- Compare targets on latency percentiles per method (avg/p90/p95/p99 from `method_metrics.csv`), error rates, and achieved throughput vs requested.
- Break errors down by outcome class rather than quoting one error rate: `RPC Errors` (an HTTP 200 carrying a JSON-RPC error), `HTTP Errors`, `Truncated`, `Timeouts`, `Transport Errors`, and `Null Results` — a call that succeeded and returned nothing, which is a real archive-node failure mode.
- With several clients in one run, `exports/results.json` carries a rank-sum test per method with the median shift. Quote the shift; the p-value only says the shift is real, and at these sample sizes nearly everything is significant.
- State the run context: where each run executed (on-host vs local, and the network caveat if local), config used, duration/load shape, and timestamps.
- Call out anomalies — error spikes, methods that dominate latency, targets that couldn't sustain the requested rate.
- If metrics went to Prometheus, mention that time-series are available in Grafana (`http://localhost:3000`).

Finish by telling the user where the output directory is and summarizing the headline comparison in a few sentences.
