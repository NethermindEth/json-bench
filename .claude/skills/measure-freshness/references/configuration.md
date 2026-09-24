# Freshness configuration reference

Both configs expand `${VAR}`, `${VAR:-default}` and `${VAR:?message}`, and reject
unknown keys. The global `--output` flag overrides `output_directory`.

## Probe config (`runner freshness probe --config`)

Template: `config/freshness/probe.example.yaml`.

| Key | Default | Notes |
|---|---|---|
| `pair.id` | required | Unique across the probes of one review. Output dir is `<id>-<run_id>`. |
| `pair.host_id` | hostname | Clock domain. Same value ⇒ compared without clock widening. |
| `pair.labels` | — | Free-form (`el`, `cl`, …). Used by review `hold_constant`. |
| `pair.el.url` | required* | EL JSON-RPC. *Or `pair.el.client_ref` + `--clients <registry>`, which also brings headers/auth. |
| `pair.el.headers` | — | Sent on every call; values never written to artifacts. |
| `pair.cl.beacon_url` | — | Optional but recommended. Enables CL sync preflight, slot duration and epoch length from `/eth/v1/config/spec`, genesis time (slot numbers), and `head`/`block`/`block_gossip` SSE events that feed review's CL split. Check the CL's real HTTP port. |
| `pair.cl.events` | true | Set false to skip the SSE stream but keep the CL checks. |
| `chain.slot_duration_seconds` | 0 | 0 ⇒ beacon spec, else preset (1, 11155111, 17000, 560048 → 12; 100, 10200 → 5). Unknown chain without beacon ⇒ must set. |
| `start.block` | 0 | First measured block. Use the same value on every probe. Already passed ⇒ starts at next block with a warning. |
| `start.time` | "" | RFC3339 alternative to `start.block`; not both. |
| `block_count` | 1000 | Measured blocks after warm-up. |
| `warmup_blocks` | 0 (example: 32) | Extra blocks measured *before* `block_count`, starting at `start.block`, excluded by review. Use 2–4 for short pilot runs. |
| `max_run_duration_seconds` | 28800 | Hard stop ⇒ outcome `max_duration`. |
| `stall_slots` | 4 | No new block for this many slots ⇒ `stall` event (keeps waiting). |
| `poll_interval_ms` | 10 | Offered cadence per probe at full rate. Not a resolution guarantee. |
| `lookahead_poll_interval_ms` | 100 | Cadence for block N+2 before N+1 is served. |
| `head_watch_interval_ms` | slot/4 | Low-rate `eth_blockNumber` safety net (missed arming, jumps, stalls). |
| `request_timeout_ms` | 2000 | Per request; no retries — the next scheduled poll is the retry. |
| `max_inflight_per_probe` | 1 | Busy at a tick ⇒ counted as a skipped poll, never queued. |
| `logs_stable_polls` | 3 | Identical locally-valid answers before a logs / tx-receipt probe stops. |
| `probes.*` | state_number, logs_number | Others: `state_latest`, `state_hash_canonical`, `logs_hash`, `transaction_receipt`, `block_receipts`. |
| `clock.mode` | auto | `auto` (chronyc → timedatectl → budget fallback) or `budget`. |
| `clock.error_budget_ms` | 5 | Used in `budget` mode and as the auto fallback. |
| `clock.step_threshold_ms` | 5 | Wall-vs-monotonic drift per second that counts as a clock step. |
| `rtt_samples` | 50 | `eth_chainId` calls for the idle RTT baseline, at start and end. |
| `sample_interval_seconds` | 30 | Resource and clock sampling period. |
| `output_directory` | results/rpc-freshness | |

Flags: `--clients <registry>` (for `client_ref`), `--record-all-attempts`.

### Probe semantics worth knowing

- `state_number`: `eth_call` to `0x0000F90827F1C53a10cb7A02335B175320002935`,
  data = 32-byte big-endian N−1, at block N, from the zero address with gas
  `0x186a0`. Correct = hash of N−1.
- `state_latest`: same call at `"latest"` — "at least N" freshness.
- `logs_number`: `eth_getLogs` `{fromBlock: N, toBlock: N}`, no filters.
- `state_hash_canonical`, `logs_hash`, `block_receipts`, `transaction_receipt`
  (last tx of the block): start only after the probe's node served the header.
- Preflight captures each method's "block unknown" answer by querying
  head + 10000 (or an unknown hash). Only a match on that shape (or a known
  phrase, flagged weak) is treated as an explicit "not ready" lower bound.

## Review config (`runner freshness review --config`)

Template: `config/freshness/review.example.yaml`.

| Key | Default | Notes |
|---|---|---|
| `reference.rpc_url` | required online | Not a tested pair. Needs `eth_getBlockByHash`; `eth_getBlockReceipts` preferred (falls back to per-tx receipts, slower). |
| `reference.headers` | — | |
| `reference.request_timeout_ms` | 10000 | |
| `reference.concurrency` | 8 | Parallel block fetches. |
| `probes` | required | Probe output directories. Same chain, genesis and slot duration required. |
| `hold_constant` | — | Label keys; each adds comparison groups of probes sharing that label value. |
| `margin_ms` | 5 | Observed tie threshold; cross-host pairs use max(margin, combined clock error). |
| `deadlines_ms` | 250, 500, 1000, 2000, 4000 | Availability columns; `slot` is always added. |
| `include_warmup` | false | |
| `timeline_blocks` | 10 | Largest-spread blocks drilled into in `report.md`. |
| `output_directory` | results/rpc-freshness/review | Reference cache lives here, so reuse the same dir for `--offline`. |

Flags: `--offline` (cache only), `--refresh-reference` (re-fetch cached blocks).
