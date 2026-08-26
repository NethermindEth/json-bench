# `runner compare` — bugs and gaps to fix

Handoff document. Four defects found while running cross-node correctness
comparisons on 2026-08-24 (Nethermind flat-history archives vs Infura/Geth).
Each is independently fixable. Ordered by impact.

**Status: bugs 1–7 fixed** on branch `fix/compare-tool-bugs`. Each section keeps
the original report and ends with what was done. Two claims in the Bug 4
write-up turned out to be inaccurate; both are corrected in place.

---

## Bug 1 — corpus loader aborts the whole run on a non-corpus JSON file

**Severity: high** (blocks `--from-jsonl rpc-calls` entirely, the documented
default path in the compare-nodes skill).

`--from-jsonl <dir>` recurses the directory and parses every `*.json` and
`*.jsonl`. A single file that is not an array of corpus entries kills the run:

```
$ go run ./runner --output out compare --from-jsonl rpc-calls --sample 120 ...
level=error msg="compare failed" error="failed to build config from corpus:
  failed to parse rpc-calls/scripts/generate-from-filter/filter-queries-mainnet.json:
  json: cannot unmarshal array into Go value of type comparator.corpusEntry"
```

The offending file is checked in and is *input to a generator*, not a corpus —
its shape is `[[{"fromBlock":0,"toBlock":...,"address":[...],"topics":[...]}]]`
(an array of arrays of filter objects).

**Where:** the corpus loader in `runner/comparator/` (the code path that
produces `failed to parse %s`), reached from the `--from-jsonl` handling in the
compare command.

**Suggested fix** — one or more of:
1. Skip files that fail to unmarshal, and log a warning naming each skipped
   file and the count of entries loaded. Failing the whole run because one file
   in a large tree is the wrong shape is too brittle.
2. Do not recurse into `scripts/` (or any directory without corpus data).
3. Accept a `--exclude <glob>` flag.

Option 1 is the important one; a run that silently loads 51 of 52 files is far
more useful than one that loads none. Make sure the warning is loud enough that
a genuinely malformed corpus is still noticed.

**Regression test:** a corpus dir containing one valid `.jsonl` and one
array-of-arrays `.json`; assert the run succeeds, loads the valid entries, and
logs a skip warning.

**Fixed** (option 1). `LoadCorpusConfig` returns a `*CorpusReport` and skips a
file rather than failing (`runner/comparator/corpus.go`), warned per file by
`logCorpusReport` in `runner/cmd/compare.go`. A file that parses but names no
method is a skip; a file whose calls are all dropped by the method exclusions
(`debug_*`, `eth_getProof`, …) is counted separately as expected rather than
warned about — warning on those produced seven noisy lines for legitimate files.
The load still fails when nothing usable was found. `--from-jsonl rpc-calls` now
loads 4046 calls from 45 files, skipping 4.

---

## Bug 2 — `--from-jsonl` rejects absolute paths

**Severity: medium** (blocks the natural workaround for Bug 1).

Having hit Bug 1, the obvious workaround is to mirror the corpus to a temp
directory with the bad file pruned. That fails too:

```
$ go run ./runner ... compare --from-jsonl /tmp/.../scratchpad/corpus --sample 120 ...
level=error msg="compare failed" error="failed to build config from corpus:
  absolute file path is not allowed: /tmp/.../scratchpad/corpus/common-queries.json"
```

The corpus had to be staged *inside the repo tree* (`outputs/_corpus-filtered/`)
to run at all.

**Where:** the path-validation guard applied to each discovered corpus file.

**Suggested fix:** the guard appears intended to stop path traversal via
untrusted config values, but `--from-jsonl` is an operator-supplied CLI
argument, and the paths being rejected are ones the tool itself produced by
walking the directory the operator named. Either:
- exempt paths that are descendants of the user-supplied `--from-jsonl` root, or
- resolve the root to an absolute path up front and validate descendants against
  it rather than rejecting absoluteness outright.

**Regression test:** `--from-jsonl /abs/path/to/corpus` loads successfully.

**Fixed** (option 2). New `config.SafeReadPathUnder(root, p)` resolves both to
absolute paths and asserts containment. The guard also came off the other
CLI-entry loaders — `--config`, `--rules`, `--spec`, `--variations` — which had
the same trap; `config.SafeReadPath` stays for the one caller that reads a path
*out of* a YAML file (`runner/config/file_loader.go`).

---

## Bug 3 — `classifyError` misses common environment/capability errors

**Severity: medium** (produces false "real differences"; directly undermines
`--fail-on-diff` as a CI gate).

`classifyError` (`runner/comparator/rules.go:210`) buckets only:

| code | class |
|---|---|
| `-32601`, `-32600` | `namespace_disabled` |
| `-32002` | `no_state` |
| `-32602` + message contains `range`/`logs`/`limit` | `range_cap` |

Errors seen in practice that are **not** classified, and therefore surface as
correctness findings:

| observed error | source | should be |
|---|---|---|
| HTTP `429 Too Many Requests` (`-32005`) | rate-limited provider (Infura) | transport/rate-limit class — see Bug 4 |
| `-32000 missing trie node 0x…` | Nethermind, pruned state | `no_state` |
| `-32000 Historical state for block N is unavailable` | Nethermind, pruned/sliced state | `no_state` |
| `4444 Pruned history unavailable` | Nethermind, pruned log index | new `pruned_history` class |
| `-32005 query returned more than 10000 results` | Geth/Infura log cap | `range_cap` |
| `-32016 eth_getLogs request was canceled due to enabled timeout` | Nethermind internal RPC timeout | new `internal_timeout` class |

**Suggested fix:** extend `classifyError` to match on message substrings in
addition to code, since Nethermind reuses `-32000` for several distinct
conditions. Keep the matching case-insensitive and anchored on distinctive
phrases (`missing trie node`, `historical state for block`, `pruned history
unavailable`). Add the two new classes to the summary buckets.

**Caution:** do not over-classify. `-32000` also carries genuine execution
errors (`err: max fee per gas less than block base fee`, `insufficient funds
for transfer`), which must stay *real* differences. Match the specific phrases,
never the bare code.

**Regression test:** table-driven, extending the existing cases in
`runner/comparator/diff_test.go:218`.

**Fixed.** `classifyError` now matches distinctive lower-cased message phrases in
addition to codes (`runner/comparator/rules.go`), adding `pruned_history` and
`internal_timeout` as named constants. No summary plumbing was needed:
`Summary.EnvError` is keyed by the class string and every consumer is
value-agnostic. `-32005` is matched by phrase only, since it is also Infura's
"Too Many Requests" (a transport concern — see Bug 4).

---

## Bug 4 — no request-rate control, and 429s are indistinguishable from failures

**Severity: medium** (makes any rate-limited endpoint unusable as a reference).

`compare` exposes `--concurrency`, `--max-retries` and `--retry-base-delay`, but
no way to cap requests per second. Against the Infura free tier (hard 429 above
~2.5 req/s) even `--concurrency 3` lost more than half the run:

```
Summary: 1977 calls — 878 identical, 48 differ (real), 1051 transport-error
  transport_errors: "HTTP request failed with status 429:
    {"code":-32005,"message":"Too Many Requests"}"
```

Retry-with-backoff did not absorb it because the limit is sustained, not bursty.
The 48 reported differences were meaningless — a client whose response was lost
to a 429 still gets compared against one that answered.

Workaround used: a local rate-limiting reverse proxy that serialized upstream
calls at one per 0.42s and retried 429 internally. It absorbed 379 × 429 with
0 failures across ~5,100 calls, and the same comparison then returned
`1977 calls — 1875 identical, 102 differ, 0 transport-error`.

**Suggested fix:**
1. Add `--rate-limit <requests-per-second>` (global, or better per-client so a
   slow reference does not throttle a fast local node). A token bucket around
   the JSON-RPC send path in `runner/comparator/jsonrpc.go` is enough.
2. Treat HTTP 429 as retryable with `Retry-After`-aware backoff, distinct from
   a hard transport failure.
3. If a call ends up with a transport error on *any* client, exclude it from the
   identical/differ tallies and report it in its own bucket — comparing a
   successful response against a lost one is not meaningful.

Point 3 matters independently of rate limiting: it is what made the first run's
"48 differences" misleading.

**Regression test:** a stub server returning 429 for the first N requests, then
200; assert the run completes with 0 transport errors and correct tallies.

**Two corrections to the report above.** Point 3's premise does not hold for a
two-client run: `CompareResponses` already skipped the diff when fewer than two
clients answered, so those calls were bucketed as transport-error, not as
differences — the original run's "48 differences" were calls where both clients
happened to answer, out of a heavily decimated sample. The mis-bucketing was real
only at **three or more** clients, where `Summarize` tested `hasDifferences()`
before the transport arm. A related bug the report missed: the diff reference was
`answered[0]`, so a dead `Clients[0]` silently re-based the comparison on another
client.

**Fixed.**
1. `--rate-limit <rps>` caps requests per second per client (fractional
   allowed), overriding the per-client `rate_limit` block in `clients.yaml` that
   existed and was validated but never enforced. The send path moved to a
   per-client `rpcTransport` (`runner/comparator/transport.go`) owning that
   client's HTTP pool and `golang.org/x/time/rate` limiter; the `eth_chainId`
   and `eth_blockNumber` preflights go through it too, so a 429 there no longer
   kills the run.
2. 429 is retryable alongside 5xx, honouring `Retry-After` in both forms and
   capped at 30s; backoff is jittered and capped. A terminal failure is
   classified (`rate_limited` / `timeout` / `other`) and the summary reports how
   many transport errors were rate limits.
3. A call is compared only when *every* client answered, with the reference
   pinned to `Clients[0]`. `Summarize` tests the transport arm first.
   `--fail-on-transport-error` makes it a CI gate.

---

## Lower-priority observations (not filed as bugs)

- `--diff-only` without a body-trimming flag logs a warning and silently
  truncates bodies to 4096 bytes. This is reasonable, but the truncated entries
  (`{"_bytes":20544,"_truncated":true}`) then appear in
  `comparison-results.json` where an analysis script may mistake them for
  responses. Consider a distinct marker or keeping the method/error summary.
  **Done:** the marker is `{"_truncated":true,"_bytes":N,"_kind":"result"}` with
  every key underscore-prefixed, and an error keeps its code/message under
  `_error`.
- `comparison-results.json` is a bare top-level JSON array. A wrapper object
  with run metadata (counts, client refs, timestamp) would make downstream
  tooling simpler and self-describing; that data currently lives only in
  `comparison-provenance.json`.
  **Done, and this breaks anything reading the bare array:** the file is now
  `{schema_version: 2, name, description, generated_at, client_refs, summary,
  results}`. `summary` always describes the whole run, even when `--diff-only`
  trims `results`. The HTML report also gained the transport errors it used to
  drop, and its dead `CallErrors` counter is now populated.
- The `--skip-above-head` hash-truncation trap documented in
  `.claude/skills/compare-nodes/references/known-differences.md` is still
  present (`hexBlock` accepts any `0x` string ≤ 18 chars; a 32-byte hash is
  longer and correctly rejected, but `eth_getBlockReceipts` by hash was
  previously mis-parsed). Worth a test pinning the current behaviour.

---

## Reproduction environment

- Repo: `json-bench`, branch `main`, commit `bd5c5e9` era (working tree clean
  apart from the appended skill reference).
- Go build: `go build ./runner/...` succeeds.
- The corpus file that triggers Bug 1 is checked in at
  `rpc-calls/scripts/generate-from-filter/filter-queries-mainnet.json`.
- Bugs 1 and 2 need no network access. Bugs 3 and 4 need a rate-limited or
  pruned endpoint; both can be reproduced with a stub HTTP server.
- Bugs 5–7 were verified against a local stub RPC server plus k6 v2.2.0; no real
  node is needed.

## Left alone deliberately

- `dropped_iterations` is still unparsed. Under `constant-arrival-rate` — what
  every `config/benchmark/*.yaml` uses — achieved throughput is pinned to `rps`
  by construction, so the informative signal is the shortfall. A
  `Target RPS` / `Achieved RPS` / `Dropped` triple would say more than the
  throughput column can.
- A config whose `calls` all have `weight: 0` (or no weight) generates an **empty
  requests file** and the run completes with zero requests: k6 throws
  "No more requests found" per iteration and every metric reads 0. Same
  silent-empty-artifact family as Bug 5, cheap to guard, not filed.
- `dashboard/src/pages/Dashboard.tsx` still has the `1000 / value` throughput
  formula plus hardcoded request totals. The other two dashboard derivations are
  already correct.
- `runner/generator/html_report.go` builds a "Request Rate by Method" table from
  summary keys that are never emitted, so it always reads zero, and hardcodes
  per-method error-rate fudging for `nethermind`. Dead third-choice report path.
- Historic DB rows written before these fixes keep the old `1/latency` throughput
  values; they were not migrated.

---

# Addendum — `benchmark` subcommand issues (2026-08-24 perf runs)

Found while benchmarking two archive nodes on-host. Same repo, different
subcommand; both cost a re-run.

## Bug 5 — a non-empty but unreachable `--prometheus` silently produces empty exports

**Severity: high** (a run reports success and writes header-only CSVs).

The `run-benchmark` skill advises pointing `--prometheus` at an unused port when
you don't want metrics exported. Doing that produces:

```
$ ./benchmark --output pilot benchmark --config c.yaml --clients cl.yaml \
    --prometheus http://localhost:19999
... level=warning msg="Failed to collect benchmark clients metrics"
    error="failed to query prometheus: ... connect: connection refused"
... level=info msg="Benchmark completed"

$ cat pilot/exports/method_metrics.csv
Client,Method,Count,Success Rate (%),Min (ms),...        <- header only, no rows
$ cat pilot/exports/client_comparison.csv
Client,Total Requests,Total Errors,...                   <- header only, no rows
```

The k6 run itself succeeded (592 requests, 0 failures, `summary.json` fully
populated) — only the exports are empty. Contrast `--prometheus ""`, which logs
`Prometheus had no data ..., falling back to summary.json` and writes real rows.

**Suggested fix:** when a Prometheus query fails or returns no data, fall back
to `summary.json` exactly as the empty-URL path already does, instead of
emitting empty exports. At minimum, escalate the "Failed to collect benchmark
clients metrics" warning to an error, since the artifacts are unusable.

**Also worth doing:** the fallback populates only the metrics k6's summary
carries (count, success rate, min, p50, p90, p95, p99, max, avg). `P75`,
`P99.9`, `Std Dev`, `Variance`, `CV (%)`, `IQR`, `MAD` are written as `0.00`,
which reads as a measured zero rather than "not available". Emit empty cells or
`NA`.

**Fixed.** `CollectClientsMetrics` now catches a Prometheus failure and delegates
to the summary path instead of returning nil, so the exports are populated; the
warning states that metrics came from `summary.json` and that no time series will
be in Grafana (`runner/metrics/results_collection.go`). It never returns a nil
map. A new `metrics.CheckPrometheus` also probes the endpoint *before* k6 starts,
so an unreachable URL is reported in the first seconds rather than after the run
(`runner/cmd/benchmark.go`).

`P75` and `P99.9` are now real measurements: `p(75)` and `p(99.9)` were added to
k6's `SummaryTrendStats` and `K6_PROMETHEUS_RW_TREND_STATS`
(`runner/generator/k6_generator.go`) and are read on both collection paths. The
Prometheus indicator parse now splits once, since splitting on every `_` turned
`p99_9` into `p99` and overwrote the real p99. `Variance`, `IQR`, `MAD`,
`Timeout Rate` and `Connection Errors` — which nothing in the repo assigns, and
which need per-sample data neither source retains — render as `NA`
(`runner/exporter/data_exporter.go`). `Std Dev` / `CV` keep the `(max-min)/4`
heuristic but are now labelled as estimates. `Error Count` was also recomputing
itself from `SuccessRate`; it now uses the measured count.

**Root cause of the bad advice:** `--prometheus` already defaults to empty, so
the `run-benchmark` skill's "point it at an unused port" guidance was stale and
was what walked the operator into this bug. It and the flag table's
`http://localhost:9090` default are corrected.

## Bug 6 — `Throughput (req/s)` in `method_metrics.csv` disagrees with k6

**Severity: medium** (wrong number in the primary analysis artifact).

For a run where k6 reported `http_reqs count=3600 rate=19.99/s` with
`dropped_iterations` absent and `vus_max=16`, `method_metrics.csv` reported
`Throughput (req/s) = 17.29` for one client and `7.78` for the other — while
both clients completed all 3,600 requests in the same 180 s window.

The per-client k6 rates were 19.99/s and 19.98/s. The exported throughput column
is not those numbers and appears to divide by something other than the scenario
duration. Either fix the computation or drop the column; as written it invites
a "node B does half the throughput" conclusion that the underlying data
contradicts.

**Fixed.** The column was the reciprocal of mean latency — `1000.0 / method.Avg`,
with a `FIXME` already in the tree (`runner/metrics/results_collection.go`). The
reported numbers back-solve exactly: `1000/17.29 = 57.8ms` and
`1000/7.78 = 128.5ms`. It is now a rate: the summary path takes k6's own
per-submetric `http_reqs` rate (already parsed and thrown away), and the
Prometheus path, which has counters but no rate, divides the count by the elapsed
time — derived from the summary's top-level `http_reqs` count/rate, which is the
real run length, falling back to the configured duration. The client aggregate is
`total requests / elapsed` rather than `1000/avg`. When the elapsed time cannot
be determined, throughput stays zero and the CSV reports `NA` instead of
inventing a number.

Verified on a 10s/20rps two-client run against a stub: per-method throughput now
matches k6's submetric rates to the decimal (10.10 / 9.90 / 10.20 / 9.90) and the
client aggregates read 20.00 and 20.10, agreeing with the HTML report's
`ActualRPS`, which was already computed correctly and used to contradict the CSV.

**Also fixed while here:** `runner/api/client_metrics.go` derived request counts
from throughput (`mm.Count = int64(mm.Throughput)`), reporting 17 requests
instead of 3600. Per-method request counts are now persisted
(`types.MetricTotalReqs` in `runner/storage/historic.go`) and read back.

**Separate bug found during verification:** the failure rate read from
`summary.json` was always zero, so every run reported 100% success. k6 reports
`http_req_failed` as a Rate metric — `passes`/`fails` and a `value` ratio — but
the parser only looked for a `rate` field, which a Rate metric does not carry
(`runner/metrics/summary_fallback.go`). Confirmed against a stub injecting 429s
on a quarter of requests: k6 reported 24.94% failed and the CSV now shows
~73–79% success per method, where it previously read 100.00%.

## Bug 7 — per-method metrics silently collapse when `calls_file` names don't match declared calls

**Severity: medium** (silent, and the data is unrecoverable after the run).

With `calls_file` set, the exporter keys `method_metrics.csv` off the **names
declared in the config's `calls:` list**, not off the `method` column of the CSV
or the `rpc_method` tag k6 emits. If the CSV's `name` column (column 2) contains
anything else, the export degrades to a single row with `Count=0`:

```
Client,Method,Count,...
archive_genesis_01,multimethod,0,0.00,0.00,...
```

The run is otherwise healthy — 5,400 requests, 0 errors, correct aggregate
latency in `summary.json`. But the per-method breakdown is gone and **cannot be
reconstructed**: k6's `root_group` contains all 453 distinct request names yet
carries only `checks`, no per-group durations, and the only tagged sub-metrics
are the threshold ones derived from the declared call names
(`http_req_duration{scenario:...,req_name:multimethod}`).

**Suggested fix:** tag and aggregate on the CSV's `method` column (column 3),
which is already passed to k6 as `rpc_method`, rather than requiring the `name`
column to match a declared call. Failing that, validate at startup that every
distinct `name` in `calls_file` matches a declared call and fail loudly.

**Documentation fix either way:** `references/configuration.md` describes the
CSV as `columns: id, name, method, payload` without stating that `name` must
correspond to a declared call for per-method export to work.

**Fixed** (the suggested fix, not the fallback). Nothing in the repo had ever
opened the calls file — it was a pure passthrough to k6, never stat'ed, and
`validateConfig` actively skipped call validation when it was set. Now:

1. `runner/config/calls_file.go` parses the CSV and returns the distinct
   `method` values in first-seen order, and `validateConfig` fails loudly on a
   missing path, a wrong column count, an empty method or an empty file —
   previously a typo'd path only surfaced inside k6, minutes in.
2. `Config.MethodKeys()` reports the tag the breakdown is keyed on: `rpc_method`
   over the calls-file methods, or `req_name` over the declared calls as before.
   The k6 threshold registration (`runner/generator/k6_generator.go`), the
   summary lookup (`runner/metrics/summary_fallback.go`) and the Prometheus label
   read (`runner/metrics/results_collection.go`) all use it, so the two
   collection paths cannot drift apart. Registering the thresholds is what makes
   k6 emit the submetrics at all — the previous run's data really was
   unrecoverable.
3. `LoadWithBackwardCompatibility` silently dropped `CallsFile` from the
   converted struct; it no longer does.

Distinct *methods* are far fewer than distinct *names* (the reported run had 453
names), which is what makes the threshold set tractable; the run logs the method
count. Verified with a calls file whose `name` column is entirely unrelated to
the declared call: the export now has one row per real RPC method per client with
real counts and rates, where it previously had a single `multimethod,0` row.

Side effect, now documented: with `calls_file` the `Method` column holds real RPC
methods rather than call names — which is what the header always claimed.
