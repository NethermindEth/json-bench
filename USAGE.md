# Usage guide

A short path from a checkout to a benchmark you can read. [README.md](README.md)
covers every subcommand and flag; [metrics/METRICS.md](metrics/METRICS.md)
covers the Prometheus series.

## Prerequisites

- Go 1.25 or later
- An Ethereum JSON-RPC endpoint to test
- Docker and Docker Compose, only for the optional Prometheus, Grafana and
  PostgreSQL stack

The load generator is built into the runner. There is nothing else to install.

## 1. Build

```bash
go build -o runner ./runner
```

## 2. Describe the endpoints

A clients registry maps names to URLs. Names may not contain dashes.

```yaml
# clients.yaml
clients:
  - name: nethermind
    type: nethermind
    url: http://127.0.0.1:8545
```

`headers`, `timeout` and an `auth` block (`basic`, `bearer` or `api_key`) are
honoured per client if you need them.

## 3. Describe the workload

```yaml
# bench.yaml
test_name: "read-heavy"
description: "eth_call and eth_getBlockByNumber against one node"
clients: ["nethermind"]
duration: "2m"
rps: 100
vus: 20
seed: 42
calls:
  - name: "eth_call"
    method: "eth_call"
    params:
      - to: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"
        data: "0x70a08231"
      - "latest"
    weight: 70
    thresholds: ["p(99)<500"]
  - name: "eth_getBlockByNumber"
    method: "eth_getBlockByNumber"
    params: ["latest", true]
    weight: 30
```

Things that bite if missed:

- **Set `vus` explicitly.** It is the ceiling on in-flight requests. Size it for
  the slowest method in the mix: a handful of slots saturated by multi-second
  `eth_getLogs` calls cannot offer a high rate, and the run will report the
  shortfall rather than quietly delivering less.
- **Only `weight` drives frequency.** Unknown keys are rejected, so a typo or a
  legacy `frequency:` fails at load time instead of silently giving a call zero
  traffic.
- **Set `seed` when results will be compared.** It fixes the request sequence,
  so two runs of the same config issue byte-identical requests.

Ready-made profiles live in `config/benchmark/`; reuse one before writing a new
file.

To keep start-up out of the numbers, add `warmup: "30s"` — those requests are
issued and then excluded from every reported statistic. To move the rate instead
of holding it, replace `duration` with `stages`:

```yaml
rps: 10            # the rate the first stage ramps from
stages:
  - duration: "1m"
    target: 500    # ramp 10 -> 500
  - duration: "3m"
    target: 500    # hold there
```

## 4. Run

```bash
./runner --output outputs/first-run benchmark \
  --config bench.yaml --clients clients.yaml --html-report
```

Comparing several clients is one run with several entries in the `clients` list:
they share one request sequence and one arrival schedule, so the comparison is
apples to apples. Note that a single run measures from one vantage point — to
remove the network from the measurement, run on each node against its own local
RPC and aggregate afterwards.

## 5. Read the result

Start here, in this order:

1. **The summary lines.** One per client, naming every outcome class and the
   error rate, plus whether the requested load was actually offered.
2. **`outputs/first-run/exports/client_comparison.csv`.** Load delivery first,
   then the outcome breakdown, then the latency percentiles. If `Delivered (%)`
   is below 100 the latency columns describe only the requests that were sent.
3. **`outputs/first-run/exports/method_metrics.csv`.** Per call: the full
   distribution, and which methods produced the errors.
4. **`outputs/first-run/manifest.json`.** How the run was produced. Check this
   matches before comparing against another run.

Two things worth knowing before drawing a conclusion:

- A JSON-RPC error is an HTTP 200. The error rate counts them; `rpc_null` — a
  call that succeeded and returned nothing — is counted separately, because
  "fast because it returned nothing" is a real failure mode.
- With several clients, `results.json` carries a rank-sum test per method with
  the median shift. At these sample sizes almost any difference is
  statistically significant, so read the shift and treat the p-value only as a
  check that the shift is real.

## Finding the capacity rather than measuring one rate

```bash
./runner --output outputs/capacity find-max-rps \
  --config bench.yaml --clients clients.yaml \
  --slo-p99 250 --slo-error-rate 0.5 --probe-duration 60s
```

Read `limited_by` in `outputs/capacity/max-rps.json` before the number. If it
says `generator`, the search measured the load generator rather than the node —
raise `vus` and run it again. If it says `search_ceiling`, the node never
breached the SLO and the answer is a floor.

## Reproducible runs

Pre-generate the request sequence and point every run at the same file:

```bash
./runner --output outputs/shared generate-requests \
  --config bench.yaml --clients clients.yaml --out outputs/shared/requests.csv
```

Then set `calls_file: outputs/shared/requests.csv` in the config. This is
required whenever results will be compared across targets, hosts or repeat
runs. The breakdown keys on the CSV's `name` column with the RPC method
alongside, so give `name` the label you want to compare on — that is what lets
one method driven through many parameter shapes be read per shape.

## Sweeping a setting

To learn what a setting does, run the same benchmark at several values of it
rather than editing the config between runs:

```bash
./runner --output outputs/batching sweep \
  --config bench.yaml --clients clients.yaml \
  --vary batch_size=1,5,10,20 --repeat 3
```

One request sequence is pinned across every point, so the sweep compares
settings rather than workloads. A point the generator could not deliver is
reported as **inconclusive** instead of as a result. With `--repeat`, each point
also reports its **P99 spread** between passes — treat a difference between
settings as real only when it exceeds that.

## Re-reading a finished run

Every run retains its observations, so it can be taken apart again without
re-running it:

```bash
./runner report outputs/first-run --phases
./runner report --compare outputs/before outputs/after
```

`--phases` splits each call into `sending`, `waiting` and `receiving`. That split
answers a question no summary can: a difference that sits entirely in `waiting`
is the node thinking, while one in `sending` or `receiving` is the generator or
the link. `--ok-only` drops failures so a latency figure is not a blend of real
work and fast errors.

`--compare` refuses two runs that counted errors differently, and warns about
settings — seed, rate, batch size, saturation policy, transport — that change
what a difference means.

## Other transports

The URL scheme in the clients file picks the transport:

```yaml
clients:
  - name: "node_ws"
    url: "ws://127.0.0.1:8546"
  - name: "node_ipc"
    url: "ipc:///var/lib/nethermind/nethermind.ipc"
```

WebSocket and IPC multiplex over one connection and match answers by JSON-RPC
id. On those, `waiting` holds the whole round trip, `receiving` is zero, and
`status` is always 200 because there is no HTTP status — the outcome comes from
the JSON-RPC body. Do not compare latency across transports.

## Optional: Prometheus and Grafana

```bash
docker compose up -d prometheus grafana

./runner --output outputs/first-run benchmark \
  --config bench.yaml --clients clients.yaml \
  --prometheus http://localhost:9090
```

Grafana is at <http://localhost:3000> (admin/admin) with the **JSON-RPC
Benchmark** dashboard provisioned. Remote write supports basic auth
(`--prometheus-rw-user`/`-pass`), a bearer token, and extra headers for
multi-tenant backends (`--prometheus-rw-header X-Scope-OrgID=team`).

Omitting `--prometheus` skips only the time series. Do not point it at an unused
port to disable it.

## Optional: historic tracking

```bash
docker compose up -d postgres

./runner --output outputs/first-run benchmark \
  --config bench.yaml --clients clients.yaml \
  --historic --storage-config config/storage/storage-example.yaml
```

## Comparing responses rather than speed

`benchmark` measures latency. To check that two endpoints *agree*, use
`compare`, which diffs their responses call by call:

```bash
./runner --output outputs/compare compare \
  --clients clients.yaml --client-refs nethermind,geth \
  --config config/compare/example.yaml
```

## Troubleshooting

| Symptom | Cause |
|---|---|
| `field <name> not found` at startup | An unknown key in the YAML. Unknown keys are rejected rather than ignored. |
| `vus must be greater than 0` | `vus` is required. |
| A large `Dropped` count or a non-zero `Max Dispatch Delay` | The generator could not offer the requested rate. Raise `vus`, lower `rps`, or use `--on-saturation=abort` to fail instead. |
| Every request is `rpc_error` | The node rejected the calls. Check the JSON-RPC error codes in `method_metrics.csv` and that the required modules are enabled. |
| Every request is `rpc_null` | The calls succeeded but returned nothing — often the wrong chain, or blocks outside the node's retained history. |
| `connection refused` | Endpoint unreachable from where the runner is running. |
