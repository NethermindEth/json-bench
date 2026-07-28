---
name: run-benchmark
description: Run an Ethereum JSON-RPC benchmark with this repository's Go runner (k6-based load testing against RPC endpoints, with reports and cross-client analysis). Use this skill whenever the user asks to run a benchmark, load-test or stress-test an RPC endpoint, measure JSON-RPC latency or throughput, or compare the performance of Ethereum clients (Nethermind, Geth, Reth, Erigon, etc.) — even if they don't use the word "benchmark".
---

# Run a JSON-RPC Benchmark

This repository benchmarks Ethereum JSON-RPC endpoints: a Go runner (`./runner`) generates a k6 load test from a YAML config, executes it against one or more RPC endpoints, and writes reports (JSON/CSV always, HTML on request) plus per-method and per-client metric exports.

The process below is the general shape of a run. The user's specific instructions always take precedence — if they name a config, a load profile, a host, or a workflow detail, follow it instead of the defaults here.

## Step 1: Establish the run parameters

From the user's request, pin down:

- **Targets** — which RPC endpoint(s)/client(s) to benchmark.
- **Workload** — which RPC methods and mix. Check `config/benchmark/` first: it has ready-made profiles (`mixed.yaml`, `read-heavy.yaml`, `ethcall-contracts.yaml`, `realistic-mix.yaml`, ...). Reuse one when it matches; generate a new config only when the user's demands don't fit an existing one.
- **Load shape** — `duration` plus either `rps` (constant arrival rate) or `iterations` (shared iterations), and optionally `vus`.
- **Prometheus export** — the runner can remote-write k6 metrics to a Prometheus instance for later visualization in Grafana. If the user hasn't said either way, ask: *"Should this run export metrics to Prometheus for later visualization, or is the generated file output enough?"* Don't silently assume either.

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
- Always set `vus` explicitly. The loader requires `vus > 0`, and the automatic inference from `rps` rounds to zero at low rates. Size it for the slowest method in the mix — a handful of VUs saturated by multi-second calls (e.g. `eth_getLogs`) will silently deliver a fraction of the requested rate.
- Only `weight` drives call frequency; some older committed profiles use `frequency: 10%`, which the loader ignores (that call would get zero traffic). Convert to `weight` when deriving from them.
- Generated configs for a specific run belong next to the run's outputs (or a scratch path), not committed into `config/` — that directory is for reusable profiles.

**Same requests everywhere.** The runner builds the request set by weighted random sampling, so two runs of the same config send *different* requests — which makes their results incomparable. Before running any benchmark command, build the runner once, then pre-generate the request set and reuse it in every run:

```bash
go build -o benchmark ./runner

./benchmark --output outputs/<benchmark-name>/ generate-requests \
  --config <benchmark-config>.yaml --out outputs/<benchmark-name>/requests.csv
```

then point every run's benchmark config at it with `calls_file: <path>/requests.csv`. This is mandatory whenever results will be compared — across targets, across hosts, or across repeat runs — and a good default even for a single target (it makes the run reproducible).

**Multiple targets.** Follow the user's instructions for how to handle them. The runner natively benchmarks several registry clients in one run (parallel k6 scenarios, which already share one request set), which works when benchmarking from a single vantage point. But for on-host SSH runs each target needs its own individual run on its own host — same workload config, same pre-generated `calls_file`, only the clients registry differs. Manage them one at a time and aggregate afterwards.

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
- **Prometheus off**: beware the default — the runner always attempts remote-write to `--prometheus` (default `http://localhost:9090`), so if a local Prometheus happens to be running, metrics get exported silently even though nobody asked. When the user wants no export, point `--prometheus` at an unused port (e.g. `http://localhost:19999`) and note that the resulting remote-write error logs are expected noise, not benchmark failure.
- Sanity-check the endpoint before a long run (e.g. an `eth_blockNumber` curl) so a dead target fails in seconds, not after the full duration.

Verify each run produced its artifacts (`summary.json`, `exports/results.json`, `exports/client_comparison.csv`, `exports/method_metrics.csv`, `report.html` if requested) before moving on.

## Step 5: Collect and analyze

The deliverable is the benchmark's output directory containing every run's reports **plus a final analysis document** you write:

- Create `outputs/<benchmark-name>/ANALYSIS.md`.
- Compare targets on latency percentiles per method (avg/p90/p95/p99 from `method_metrics.csv`), error rates, and achieved throughput vs requested.
- State the run context: where each run executed (on-host vs local, and the network caveat if local), config used, duration/load shape, and timestamps.
- Call out anomalies — error spikes, methods that dominate latency, targets that couldn't sustain the requested rate.
- If metrics went to Prometheus, mention that time-series are available in Grafana (`http://localhost:3000`).

Finish by telling the user where the output directory is and summarizing the headline comparison in a few sentences.
