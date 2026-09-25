# Freshness outputs reference

Timestamps are `{wall_ns, mono_ns}` with values as decimal strings. `wall_ns`
is comparable across hosts within the clock error; `mono_ns` only within one
probe process. All freshness figures are milliseconds from the block's slot
start (`header.timestamp`).

## Probe directory (`<pair-id>-<run-id>/`)

| File | Content |
|---|---|
| `run-manifest.json` | Redacted config, EL/CL versions, chain id, genesis, slot duration + source, block range, `outcome` (`completed`, `max_duration`, `interrupted`, `error` + reason), `clock` (mode, source, error_ms, max_error_ms, steps), `rtt_start`/`rtt_end` (`samples`, `failures`, `p50_us`, `p95_us`, `max_us` — microseconds), `slots_per_epoch`, `dropped_records`, counts. |
| `capabilities.json` | Preflight: genesis hash (or `genesis_error` on pruned nodes), EIP-2935 code/canary, EL/CL sync checks, per-probe `supported` + reason + `not_ready_signature`. |
| `targets.jsonl` | One line per block: hash, parent, timestamp, slot, `missed_slots_before`, tx count, empty-bloom flag, header observed time/source, `late_armed`, `warmup`, `parent_mismatch`, `clock_step`, and per-probe results (status, match time, attempts, skipped polls, scheduler lag). |
| `rpc-attempts.jsonl` | Outcome transitions per probe/block: `edge` `first`/`last` of each run of identical outcomes (`repeat` = run length), class, local verdict, digest, error, bytes. `edge: all` with `--record-all-attempts`. |
| `events.jsonl` | `run_started/finished`, `target_armed/promoted`, `header_observed`, `missed_slot`, `parent_mismatch`, `late_armed`, `stall`, `clock_sample`, `clock_step`, `resource_sample`, `cl_event`, `cl_stream_disconnected`. |
| `responses/<digest>.json.gz` | Each distinct canonical answer once, gzipped (`zcat`). Review uses only digests; these are for forensics. |

Probe-side statuses (local view, not verified): `matched`, `not_applicable`
(`not_applicable_empty_logs` / `_empty_transactions`), `deadline_exceeded`,
`late_armed`, `aborted`. Attempt classes: `result`, `not_ready`, `rpc_error`,
`null_result`, `transport_error`, `timeout`, `http_error`, `invalid_body`.

## Review directory

| File | Content |
|---|---|
| `reference-data/` | Cached raw reference answers (`<hash>.json`, `_chain.json`). Enables `--offline`. |
| `block-results.jsonl` | Per block identity: reference verdict, `epoch_boundary`, per pair × probe outcome (freshness, lower bound, first sent, left-censored, within slot), and `timeline.<pair>`: `header_observed_ms`, `cl_events[]` as `{topic, ms}` (ms from slot start), `epoch_transition`, `execution_optimistic`. |
| `summary.json` | Probes, warnings, block counts, per probe: per-pair stats and pairwise comparisons. |
| `report.md` | Human report of the above. |

Reference verdicts: `verified` (receipts reproduce the receipts root),
`orphaned` (reference has another block at that height, or does not know it),
`unverified` (fetch failed, no cache, or verification failed — never success).

Review outcomes per pair × probe:

| Status | Meaning | In denominator |
|---|---|---|
| `matched` | Earliest response equal to the verified data; time = its receive time | yes |
| `incorrect` | Answered with data, never the correct data | yes |
| `timeout` | No data answer before the probe stopped | yes |
| `aborted` | Run ended while the target was open | yes |
| `incorrect_not_applicable` | Probe judged the block empty; reference has data | yes |
| `not_applicable` | Block has no logs / transactions for this probe | no |
| `late_armed` | Not polled before it was available | no |
| `unsupported` | Probe failed preflight on this pair | no |
| `orphaned` / `unverified` | See reference verdicts | no |
| `different_block` | This pair served another block at that height | no |
| `not_observed` / `out_of_range` / `warmup` | Not measured by this pair | no |

`left_censored`: the first request already succeeded, or no explicit "not
ready" preceded it — availability happened at an unknown earlier time.
Inferred comparisons treat its lower bound as unbounded.

Pairwise fields: `compared`, `observed_a_wins/b_wins/ties`,
`inferred_a_wins/b_wins/unresolved`, `coverage_a_wins/b_wins` (only one side
correct), `both_failed`, `excluded` (by reason), `delta_a_minus_b`,
`effective_margin_ms`, `cross_host`.

Per-pair context: `optimistic_imports`, `freshness_epoch_boundary` /
`freshness_other_blocks` (present when the range has epoch boundaries),
`lag_vs_state_number` (non-state probes: this probe's freshness minus
`state_number`'s on blocks where both matched).

CL split (only with beacon events), keyed by milestone `block_gossip` / `head`
/ `block`:

- per pair, `pairs.<id>.cl_split.<topic>`: `blocks_with_event`,
  `measured_without_event`, `milestone_from_slot_start`, `ready_after_milestone`
  (distributions, ms);
- per comparison, `same_cl` and `cl_split.<topic>` (only `block_gossip` when
  `same_cl` is false): `compared`, `milestone_delta_a_minus_b`,
  `ready_after_a_faster` / `_b_faster` / `_ties` (configured margin, no clock
  widening: both sides are same-host differences), `ready_after_delta_a_minus_b`.

## Quick queries

```bash
# Per-pair state freshness p50/p95 and denominators
jq '.results.state_number.pairs | map_values({n: .denominator, p50: .freshness.p50_ms, p95: .freshness.p95_ms, status})' summary.json

# Blocks where a pair did not match
jq -c 'select(.results["<pair>"].logs_number.status != "matched") | {block_number, status: .results["<pair>"].logs_number.status, ref: .reference.status}' block-results.jsonl

# Transitions for one block on one probe directory
jq -c 'select(.block_number == <N>) | {probe, seq, edge, repeat, class, local, local_reason, error}' <probe-dir>/rpc-attempts.jsonl

# Where each pair spends its time after the block reaches its CL
jq '.results.state_number.pairs | map_values(.cl_split.block_gossip | {milestone_p50: .milestone_from_slot_start.p50_ms, after_p50: .ready_after_milestone.p50_ms})' summary.json

# Probe health at a glance
jq '{outcome, outcome_reason, clock, rtt_start, dropped_records, counts}' <probe-dir>/run-manifest.json
```
