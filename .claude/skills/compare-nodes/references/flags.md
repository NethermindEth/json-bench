# `runner compare` flag & config reference

Invocation:
```bash
go run ./runner --output <dir> compare [flags]
```
`--output` is the global flag (before the subcommand) and sets where all
artifacts are written.

## Input selection (choose exactly one)

| Flag | Meaning |
|---|---|
| `--config <file>` | Curated compare YAML (`calls:` map). See `config/compare/example.yaml`. |
| `--from-jsonl <dir>` | Build the config from a corpus dir (absolute paths fine). Recurses; reads `*.jsonl` and `*.json` arrays. Excludes `eth_getProof` and head-dependent zero-arg methods (`eth_gasPrice`, `eth_syncing`, `eth_blockNumber`, `eth_maxPriorityFeePerGas`) and the `debug_` prefix. Files that are not corpora (generator inputs, scenario files) are skipped with a warning naming each one; only an entirely unusable tree fails the run — read the load line (`loaded N calls from M files, … K files skipped`) before trusting the sample size. |
| `--sample <N>` | With `--from-jsonl`, keep at most N calls per method (0 = all). |
| `--sample-seed <n>` | Deterministic seed for `--sample` (default 42). |

Required always: `--clients <file>` and `--client-refs a,b` (first = diff
reference/baseline).

## Expected-difference rules & pinning

| Flag | Meaning |
|---|---|
| `--rules <file>` | YAML file with a `comparison:` block (`rules` + optional `block_override`) merged into either input mode. |
| `--block-override <hex>` | Rewrite `latest`/`pending` tags to this static block and append a block arg to calls that omit one. Overrides config/`--rules`. |
| `--skip-above-head` | Skip calls pinned to a numeric block above the lowest client head. |
| `--strict-response-comparison` | Drop the comparator's implicit normalizations (see below). Off by default. |

### `--strict-response-comparison`

For replaying recorded traffic against two builds of the same client, where any
byte of difference is a finding. It removes three behaviours that no rule can
switch off, and changes nothing else — explicit `ignore` and
`numeric_tolerance` rules, the built-in `eth_estimateGas` tolerance and the
error classification all behave exactly as they do by default:

| Default | Under `--strict-response-comparison` |
|---|---|
| `"0x"` equals any all-zero hex string (`0x0`, `0x00`, `0x00…00`), in `result` and inside `error.data` | They are different values. A client that omits a value and one that returns zero are not giving the same answer |
| JSON numbers decode as `float64`, so two integers above 2⁵³ that differ can compare equal | Numbers are decoded and compared by their literal digits |
| `--block-override` adds `fromBlock`/`toBlock` to an `eth_getLogs` filter that carries `blockHash`, turning a single-block query into a range query | A filter carrying `blockHash` is sent unchanged |

The run also records, in `comparison-provenance.json`, `strict_response_comparison`
and a `wire_transformations` list giving the original and effective params of
every call the block override rewrote — so the request the answers belong to is
recorded, not only the one the corpus held. That list is written in both modes.

`comparison:` block schema (in `--config` or `--rules`):
```yaml
comparison:
  block_override: "0x1406f40"     # optional
  rules:
    - method: <rpc-method>        # optional; omit = all methods
      path:   <json-path>         # e.g. result.totalDifficulty; supports [*]
      kind:   ignore | numeric_tolerance | error_code_only | error_presence_only
      abs:    <number>            # numeric_tolerance only
      rel:    <fraction 0..1>     # numeric_tolerance only
```
Rule kinds:
- `ignore` — drop a JSON path (and subtree) from comparison. `[*]` matches any
  array index, e.g. `result.transactions[*].v`.
- `numeric_tolerance` — treat two `0x` hex quantities as equal within `abs`
  units and/or `rel` fraction. `eth_estimateGas` gets a built-in 10% `rel`
  unless a tolerance rule for it is already present.
- `error_code_only` — when both responses are errors, compare only the code.
- `error_presence_only` — when both are errors, treat as equal regardless.

## Output size control

| Flag | Meaning |
|---|---|
| `--diff-only` | Exclude identical calls; also caps response bodies (unless `--keep-response-bodies`). The main lever for small reports. |
| `--keep-response-bodies` | With `--diff-only`, keep full bodies for differing calls instead of truncating. |
| `--omit-matching-responses` | Drop full responses entirely; keep only diff entries. |
| `--max-response-bytes <N>` | Truncate embedded response bodies larger than N bytes (0 = no limit). |

## Reliability

| Flag | Meaning |
|---|---|
| `--concurrency <N>` | Concurrent requests (default 5). Keep low (≤4) against throttling remote nodes. |
| `--timeout <s>` | Per-request timeout seconds (default 30; use 60 for heavy blocks/logs). |
| `--max-retries <N>` | Max transport attempts per request (0 = client's `max_retries` from clients.yaml, else 5). Retries cover transport errors, 5xx and 429, honouring `Retry-After` (capped 30s). |
| `--retry-base-delay <dur>` | Base backoff between retries (0 = 200ms), jittered and capped at 30s. |
| `--rate-limit <rps>` | Cap requests per second **per client** (fractional allowed, e.g. `2.4`). Overrides the client's own `rate_limit` block in clients.yaml. Use this for a rate-limited reference (Infura free tier ≈2.5 req/s) — retries alone cannot absorb a sustained limit, and `--concurrency` caps calls in flight, not the rate. |

## Exit code / CI

| Flag | Meaning |
|---|---|
| `--fail-on-diff` | Exit non-zero when *real* (non-environment) differences remain. |
| `--fail-on-env-diff` | Also exit non-zero on environment/capability differences (compose with `--fail-on-diff` for strict mode). |
| `--fail-on-transport-error` | Exit non-zero when any call lost a client to a transport error, so a run decimated by throttling cannot pass as green. |

## Other

| Flag | Meaning |
|---|---|
| `--validate-schema` | Validate responses against the OpenRPC schema. |

## Artifacts written to `--output`

- `comparison-results.json` — `{schema_version: 2, name, description, generated_at, client_refs, summary, results}`. The per-call entries (responses subject to output-size flags, differences, `transport_errors`, `transport_error_class`, `error_class`) are under `results`; `summary` always describes the whole run even when `--diff-only` drops identical calls from `results`. A response too large to embed is replaced by a marker whose every key is underscore-prefixed: `{"_truncated":true,"_bytes":N,"_kind":"result"|"error","_error":{...}}` — never mistake it for a response. Every entry also carries `rpc_method` (the wire method the `_variantN` identifier resolves to) and `request_id` (see **Request identity** below).
- `comparison-report.html` — human-facing report (honors the output-size flags).
- `comparison-provenance.json` — effective config: client refs, block override, rules, skipped calls, counts. Makes a run self-describing and reproducible. Each entry of `skipped` carries `rpc_method` and `request_id` too — a skipped call produces no result, so this is the only place it is named.
- `corpus-load-report.json` — written by `--from-jsonl` only, and written **even when the load fails**, because a corpus that yielded nothing is exactly what a caller has to notice. `{schema_version: 1, generated_at, corpus_dir, identity, sampling, totals, files, skipped_files, requests}`. `totals` splits files into loaded / excluded-only / skipped and calls into selected / sample-dropped, and each split adds up. `files` is the per-file entry count and what became of it; `skipped_files` is `{path, reason}` per file that did not parse. `requests` names every call by identity in the bucket that explains its fate: `selected` (handed to the comparator, with its `call_id`), `excluded` (dropped by the method policy) and `sample_dropped` (not chosen by `--sample`). The same information is still logged, but the log is no longer the only account of it.

- Run summary logged to stdout: `identical / differ (real) / differ (env/expected) / transport-error (of which rate-limited) / schema-error / skipped` plus env/capability buckets. A call that lost any client to a transport error is counted only as transport-error: it was never compared, so it cannot be a difference.

### Request identity

A `request_id` is the lowercase hex SHA-256 of the canonical request string: the
two-element JSON array `["<method>", <params>]`, compact, with object keys
sorted, absent or null params normalized to `[]`, and UTF-8 left unescaped. It
is computed from the params **as supplied**, before `--block-override` rewrites
them, so it identifies the request a corpus recorded rather than the call that
went on the wire — which is what lets a run be reconciled against the manifest
the corpus was published under. String case and hex spelling are significant:
`0xABC`, `0xabc` and `0x0abc` are three requests.

The encoding is shared with `NethermindEth/rpc-corpus-tools` (its CONTRACT.md
§4), and `runner/comparator/identity_test.go` checks this implementation against
digests that project produced. Paths and identities are operator data: the load
report is a private artifact, not something to publish alongside a summary.

With `--block-override` in play, `request_id` and the `wire_transformations`
list in `comparison-provenance.json` are the two halves of one picture: the
identity names the request as recorded, `wire_transformations` gives the
effective params that request was actually sent as.

## Environment/capability error classes

A classified error is treated as configuration, not a correctness finding, and
lands in `differ (env/expected)` rather than `differ (real)`.

| Class | Matched on |
|---|---|
| `namespace_disabled` | codes `-32601`, `-32600` |
| `no_state` | code `-32002`; messages `missing trie node`, `historical state for block` |
| `range_cap` | code `-32602` with `range`/`logs`/`limit`; message `query returned more than …` (`-32005`) |
| `pruned_history` | message `pruned history unavailable` (Nethermind code `4444`) |
| `internal_timeout` | message `canceled due to enabled timeout` (Nethermind `-32016`) |

Matching is by phrase, not bare code: clients reuse `-32000` for pruned state
*and* for genuine execution errors (`max fee per gas less than block base fee`,
`insufficient funds`), which stay real differences.

A 429 is never an error class — it never produces a response — so it is recorded
in `transport_errors` with `transport_error_class: rate_limited`.
