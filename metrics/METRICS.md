# Metrics reference

Every series the runner publishes to Prometheus, what it means, and how the
names map to the `k6_*` series it used to emit.

The runner publishes only when `--prometheus` is given. Without it a run still
produces its JSON and CSV exports and the per-request sample file; only the
time series are skipped.

```bash
runner benchmark --config bench.yaml --clients clients.yaml \
  --prometheus http://localhost:9090
```

## Three things that surprise people

**Trend values are cumulative from the start of the run, not per push.** At any
moment `bench_http_req_duration_p99` is the p99 of every request since the run
began, so a latency panel reads "p99 so far" and converges rather than
oscillating. This is what k6 did, and every dashboard panel is written against
it: they aggregate the raw gauge instead of taking a rate. If you want the p99
of a window, read the per-request sample file instead (below).

**Durations are in seconds; the CSV exports are in milliseconds.** Seconds is
the Prometheus base unit and what k6 wrote. `0.0429` is a 42.9 ms call.

**Counters are cumulative in the ordinary Prometheus sense**, so `irate()` and
`increase()` behave normally on the `_total` families.

**A run's `warmup` period is absent from all of it.** Warmup requests are
issued but excluded from every series here, so a latency panel shows steady
state rather than the average of steady state and start-up.

## Labels

| Label | On | Meaning |
|---|---|---|
| `testid` | everything | `test_name` from the benchmark config. The dashboard's run selector. |
| `scenario` | everything | Client name from the clients registry. One scenario per client. |
| `client_type` | everything | The client's `type` (`nethermind`, `geth`, …), when set. |
| `req_name` | per-request families | The call's `name` in the config, or the method when unnamed. |
| `rpc_method` | per-request families | The JSON-RPC method. This is what the per-method breakdown keys on. |
| `status` | distributions and `bench_http_reqs_total` | HTTP status, `0` when no response arrived. |
| `outcome` | distributions and `bench_http_reqs_total` | The response class — see below. |
| `rpc_code` | `bench_rpc_errors_total` | The `error.code` the node returned, e.g. `-32000`. |

### `outcome`

A JSON-RPC error arrives as HTTP 200, so status alone cannot say whether a call
succeeded. Every response resolves to exactly one class:

| Value | Meaning | Counts as an error |
|---|---|---|
| `ok` | HTTP 200, a JSON-RPC `result` with data | no |
| `rpc_null` | HTTP 200, `result` is `null`, `[]`, `{}` or `0x` | no |
| `rpc_error` | HTTP 200 carrying a JSON-RPC `error` object | yes |
| `http_error` | non-200 | yes |
| `truncated` | HTTP 200 whose body did not arrive whole | yes |
| `timeout` | the request deadline expired | yes |
| `transport` | any other transport failure | yes |

`rpc_null` is deliberately not an error: the call succeeded and returned
nothing. It is counted separately because "fast because it returned nothing" is
a real archive-node failure mode, and folding it into `ok` hides it.

Because `outcome` sits on the distributions too, the latency of the calls that
*worked* is a query rather than an unanswerable question:

```promql
bench_http_req_duration_p99{outcome="ok"}
```

That matters when comparing clients: one that fails fast on a third of its calls
reports a flattering p99 when the failures are mixed in.

## Series

### Request latency

Seven families, each with the full stat ladder
`_min _max _avg _med _p75 _p90 _p95 _p99 _p999`.

| Family | Meaning |
|---|---|
| `bench_http_req_duration` | `sending + waiting + receiving`. The number to quote as request latency; it excludes connection setup. |
| `bench_http_req_waiting` | Time to first byte. Usually the bulk of `duration`, and the part the node controls. |
| `bench_http_req_sending` | Writing the request. |
| `bench_http_req_receiving` | Reading the response body. Grows with payload size — pair it with `bench_resp_bytes`. |
| `bench_http_req_blocked` | Waiting for a free connection, including dial and TLS. Non-zero at steady state means the pool is too small. |
| `bench_http_req_connecting` | TCP connect, zero on a reused connection. |
| `bench_http_req_tls_handshaking` | TLS handshake, zero on plain HTTP and on reuse. |

Labels: `testid, scenario, client_type, req_name, rpc_method, status, outcome`.

### Request counts and failure rates

| Series | Type | Meaning |
|---|---|---|
| `bench_http_reqs_total` | counter | Requests, split by `status` and `outcome`. |
| `bench_http_req_failed_rate` | gauge | HTTP-level failure ratio for a method, `0`–`1`. Keeps k6's meaning: a JSON-RPC error is *not* counted here. |
| `bench_rpc_error_rate` | gauge | Share of a method's calls that returned a JSON-RPC error, `0`–`1`. |
| `bench_rpc_errors_total` | counter | JSON-RPC errors by `rpc_code`. |
| `bench_resp_bytes_{avg,max,p95}` | gauge | Response size. The missing axis when comparing `eth_getLogs` across clients: a client returning less data is not a faster client. |

The two rate families are reported **per method**, merged across status and
outcome, so they carry no `status` or `outcome` label. A failure rate inside a
single status bucket is only ever 0 or 1; k6 emitted exactly that, which is why
its dashboard's failure percentage — an average over those buckets — never
described anything.

For an exact failure percentage across a client, count requests:

```promql
sum by (scenario) (bench_http_reqs_total{outcome!~"ok|rpc_null"})
  / sum by (scenario) (bench_http_reqs_total) * 100
```

### Load delivery

The load actually offered, as distinct from the load requested. Nothing here has
a k6 counterpart beyond `dropped_iterations`, and without it a run that offered
a fraction of its target rate reads exactly like one that met it.

| Series | Type | Meaning |
|---|---|---|
| `bench_requests_scheduled_total` | counter | Requests the arrival schedule called for. |
| `bench_requests_late_total` | counter | Requests dispatched more than one arrival interval behind schedule. |
| `bench_requests_dropped_total` | counter | Requests never sent because the in-flight limit was reached. Only under `--on-saturation=drop`. |
| `bench_achieved_rate` | gauge | Requests per second actually offered. Compare against the configured `rps`. |
| `bench_queue_delay_*` | gauge | Distribution of dispatch delay, full stat ladder. |
| `bench_inflight` | gauge | Concurrent requests right now. |
| `bench_inflight_peak` | gauge | High-water mark. At the configured `vus` the generator is the limiter, not the node. |

`bench_queue_delay` is the coordinated-omission signal. While it is above zero
the latencies above describe a slower offered rate than the one configured, and
the run understates what the node would do at the requested load.

### What the node said about itself

When a client declares a `metrics_url`, the runner reads that endpoint during
the run and republishes the selected families as `bench_target_<name>`, keeping
the node's own labels and adding `testid`, `scenario` and `client_type`. Node
CPU therefore sits on the same timeline as the latency it produced — the
correlation this exists for.

```yaml
clients:
  - name: nethermind
    url: http://127.0.0.1:8545
    metrics_url: http://127.0.0.1:9091/metrics
```

Two things to know:

- **These counters count from when the node started**, not from the start of the
  run, unlike every other `bench_*` counter. `rate()` and `increase()` behave
  normally; the absolute value does not describe the benchmark. The CSV and
  `results.json` report a counter as its delta over the run for that reason.
- **The name is prefixed rather than passed through**, so a series that reached
  Prometheus by way of a benchmark is distinguishable from one scraped directly
  and cannot collide with it. The run's own labels are applied last, so a node
  publishing a label called `scenario` cannot overwrite which client a sample
  came from.

Which families are read is chosen by `--target-metric`, repeatable, with a
trailing `*` matching by prefix. The default set covers generic process and
runtime families (`process_cpu_seconds_total`,
`process_resident_memory_bytes`, `go_*`, `dotnet_*`); client-specific ones have
to be named, because a built-in list of them would rot as each client renames
its own:

```bash
--target-metric "process_*" --target-metric "nethermind_*"
```

### Run totals

| Series | Type | Meaning |
|---|---|---|
| `bench_iterations_total` | counter | Completed HTTP round trips: batches when batching, requests otherwise. `bench_http_reqs_total` counts the calls inside them. |
| `bench_iteration_duration_*` | gauge | Duration of one HTTP round trip, full stat ladder. With `batch_size` that is the batch's latency, recorded once per round trip rather than once per call. |
| `bench_data_sent_total` | counter | Request bytes, headers included. |
| `bench_data_received_total` | counter | Response bytes. |

Labels on this section and on load delivery: `testid, scenario, client_type`.

## Migrating a `k6_*` query

Structure is unchanged — stat suffixes, `_total` counters, `_rate` gauges,
seconds, cumulative trends — so most queries need only the prefix swapped.

| k6 | Replacement | Note |
|---|---|---|
| `k6_http_req_duration_*` | `bench_http_req_duration_*` | Prefix only. Gains `outcome`. |
| `k6_http_req_waiting_*` | `bench_http_req_waiting_*` | Prefix only. |
| `k6_http_req_sending_*` | `bench_http_req_sending_*` | Prefix only. |
| `k6_http_req_receiving_*` | `bench_http_req_receiving_*` | Prefix only. |
| `k6_http_req_blocked_*` | `bench_http_req_blocked_*` | Prefix only. |
| `k6_http_req_connecting_*` | `bench_http_req_connecting_*` | Prefix only. |
| `k6_http_req_tls_handshaking_*` | `bench_http_req_tls_handshaking_*` | Prefix only. |
| `k6_http_reqs_total` | `bench_http_reqs_total` | Gains `outcome`; loses `url`, `group`, `error`, `error_code`. |
| `k6_http_req_failed_rate` | `bench_http_req_failed_rate` | Now per method, so it loses `status`. Same HTTP-only meaning. |
| `k6_iteration_duration_*` | `bench_iteration_duration_*` | Prefix only. |
| `k6_iterations_total` | `bench_iterations_total` | Prefix only. |
| `k6_data_sent_total` | `bench_data_sent_total` | Prefix only. |
| `k6_data_received_total` | `bench_data_received_total` | Prefix only. |
| `k6_dropped_iterations_total` | `bench_requests_dropped_total` | Renamed, and gains `client_type` which k6 omitted. |
| `k6_vus` | `bench_inflight` | Renamed: requests in flight is the quantity that was meant. |
| `k6_vus_max` | `bench_inflight_peak` | Renamed. `_max` would read as a trend stat. |
| `k6_checks_rate` | — | Dropped. `outcome` carries strictly more, and unlike checks it also reaches the CSV exports. |
| `k6_group_duration_*` | — | Dropped. k6 groups have no counterpart, and no panel read them. |

Labels that no longer exist:

- `url` — one value per client, and no panel grouped by it.
- `group` — a k6 path artifact (`:::eth_call`).
- `error`, `error_code` — k6's own numeric taxonomy (`1000` generic network,
  `1500` HTTP 5xx). `outcome` and `rpc_code` say what actually happened.
- `expected_response` — k6 never emitted it, because it was absent from the
  configured `systemTags`, so every panel filtering on it drew nothing.
  `outcome` replaces it.

One consequence worth having: k6 attached `error` and `error_code` only to
failing series, so a flapping node minted new series mid-incident. Every
`bench_*` family keeps one label-key set for the life of a run.

## Cardinality

Per client and method: 7 latency families × 9 stats, plus `resp_bytes` × 3 and
the counters — around 70 series, multiplied by the distinct `(status, outcome)`
pairs a method actually produces, which is one for a healthy method and two or
three for a failing one. Per client: about 25 more.

For a 5-client, 45-method profile that is roughly 16,000 series at one push
every 5 s, comparable to what k6 produced. Tune with
`--prometheus-push-interval`.

## Run artifacts

A run writes these under `--output`, whether or not Prometheus is configured:

| File | Contents |
|---|---|
| `manifest.json` | How the run was produced: engine and version, the error-rate semantics, seed, saturation policy, load shape, the transport settings that change the numbers, and each client's name, type and URL. Read this before comparing two runs. |
| `samples.jsonl.gz` | One record per request. |
| `exports/results.json` | The whole result, including the manifest and the pairwise client comparison. |
| `exports/client_comparison.csv` | Per client: delivery accounting, the outcome breakdown, latency percentiles. |
| `exports/method_metrics.csv` | Per method: the full distribution statistics and outcome counts. |

`manifest.json` exists because the error rate counts JSON-RPC errors, which an
HTTP-only pipeline could not see. Two runs measured under different semantics
are not comparable, and a regression detector needs something to check that
against rather than silently reporting the change as a regression.

## Beyond Prometheus

Prometheus holds cumulative aggregates. For anything else — the p99 of a
window, a latency histogram, the distribution of one method's JSON-RPC error
codes — read the per-request sample file, `<output>/samples.jsonl.gz`. One
gzipped JSON object per request, carrying its timings, phase breakdown, outcome,
RPC code and byte counts. Percentiles recomputed from it agree exactly with the
reports, because both come from the same samples.

The same samples drive the pairwise client comparison in `results.json`: a
two-sided Mann-Whitney U test of each method's latency between each pair of
clients, with the median shift as the effect size. At benchmark sample sizes
almost any difference is significant — tens of thousands of requests make a
tenth of a millisecond "significant" — so the shift is the number to read and
the p-value only says whether the shift is real.

## Dashboards

| Dashboard | Purpose |
|---|---|
| `dashboards/benchmark-dashboard.json` | The live one. Reads `bench_*`. |
| `dashboards/archive/k6-dashboard.json` | The k6-era dashboard, kept for reference. Reads `k6_*`, which nothing writes any more, so its panels render empty. Keeps its original uid so existing links and forks resolve. |
| `dashboards/jsonrpc-benchmark-enhanced.json` | Historic runs from PostgreSQL through the runner's API, not Prometheus. |
| `dashboards/baseline-comparison.json` | Baseline comparison, SQL over PostgreSQL. |
