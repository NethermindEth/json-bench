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
| `--from-jsonl <dir>` | Build the config from a corpus dir. Recurses; reads `*.jsonl` and `*.json` arrays. Excludes `eth_getProof` and head-dependent zero-arg methods (`eth_gasPrice`, `eth_syncing`, `eth_blockNumber`, `eth_maxPriorityFeePerGas`) and the `debug_` prefix. |
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
| `--max-retries <N>` | Max transport attempts per request (0 = client's `max_retries` from clients.yaml, else 5). |
| `--retry-base-delay <dur>` | Base backoff between retries (0 = 200ms). |

## Exit code / CI

| Flag | Meaning |
|---|---|
| `--fail-on-diff` | Exit non-zero when *real* (non-environment) differences remain. |
| `--fail-on-env-diff` | Also exit non-zero on environment/capability differences (compose with `--fail-on-diff` for strict mode). |

## Other

| Flag | Meaning |
|---|---|
| `--validate-schema` | Validate responses against the OpenRPC schema. |

## Artifacts written to `--output`

- `comparison-results.json` — per-call responses (subject to output-size flags) and differences.
- `comparison-report.html` — human-facing report (honors the output-size flags).
- `comparison-provenance.json` — effective config: client refs, block override, rules, skipped calls, counts. Makes a run self-describing and reproducible.
- Run summary logged to stdout: `identical / differ / transport-error / schema-error / skipped` plus env/capability buckets.
