---
name: measure-freshness
description: >-
  Live new-block RPC FRESHNESS race between Ethereum node pairs (EL+CL) using
  this repo's `runner freshness probe` / `runner freshness review`. Use this
  whenever the user wants to know which node / client pair serves correct data
  for a NEW block first — e.g. "which setup exposes new state first", "how fast
  after the slot can I read the block", "compare Nethermind+Lighthouse vs
  Reth+Prysm on freshness", "time to first correct eth_call / getLogs after a
  block", "does the CL choice change when data is readable", "head-of-chain
  race", "block availability latency". Trigger it even when the user says
  "latency" or "benchmark" but the question is about how soon new blocks become
  readable rather than steady-state request latency. Not for load/throughput
  (run-benchmark) or response equivalence at a fixed block (compare-nodes).
---

# Measure new-block RPC freshness

Drive `runner freshness` to answer: for the same new block, which EL+CL pair lets
an application read **correct** new state and logs first, measured from the
block's slot start. Earlier block delivery is part of the advantage and is not
normalised away.

Two modes, run separately:

- **probe** — live, one process per pair, on (or next to) that pair's host.
  Pre-arms `eth_call` (EIP-2935 canary) and whole-block `eth_getLogs` for the
  next block, polls every 10 ms, writes raw data. Light and self-contained: it
  never talks to other pairs or to the reference.
- **review** — offline, anywhere. Verifies every block against a separate
  reference node (receipts-root check), picks each pair's earliest correct
  response, joins pairs by block hash, writes `report.md`.

The user's instructions override the defaults below (block count, probes,
hosts, placement). The tool does not set up nodes: pairs are whatever the user
points it at.

## When to use vs. not

- **Use** for "who serves the new block first / how soon after the slot is data
  readable", across any user-chosen set of EL+CL pairs.
- **Do not use** for steady-state latency or throughput (`run-benchmark`), or
  for "do these nodes return the same answers" (`compare-nodes`).

## Prerequisites

- `go build ./runner/...` works from the repo root.
- Every tested pair: synced EL with the **EIP-2935 history contract active**
  (Prague/Pectra or later — the state probe depends on it), reachable JSON-RPC,
  ideally the CL's beacon API too.
- A **reference node that is not one of the tested pairs**, retaining receipts
  for the measured range. Review, not probe, needs it.
- Synchronised clocks on every probe host (chrony/NTP; PTP is better). Cross-host
  comparisons are only as good as the clocks.

## Workflow

### 0. Start a feedback log

This skill is being validated on live nodes. Create
`outputs/<date>-freshness-<name>/FEEDBACK.md` at the start and append to it as
you go: every command that failed or needed a workaround, confusing output,
warnings you could not explain, missing options, wrong defaults, and time spent
per step. Record exact commands, runner commit (`git rev-parse --short HEAD`),
client versions and error text. Do not "fix" the tool silently mid-run — note
it and continue with a workaround, so the user can bring the log back for fixes.

### 1. Frame the run

Pin down with the user (ask only what is unspecified and consequential):

- **Pairs** — for each: id (e.g. `nethermind-lighthouse`), EL RPC URL, beacon
  URL, host, and labels (`el`, `cl`, optionally `host`). Labels drive review
  groupings: `hold_constant: [cl]` compares ELs under the same CL.
- **Placement** — where each probe runs (see step 3). Default: on the pair's host.
- **Reference node** — URL, and confirm it is not a tested pair.
- **Scope** — `block_count` (default 1000 ≈ 3.3 h on mainnet, plus 32 warm-up),
  probes (default `state_number` + `logs_number`; optional variants change the
  workload — run them as a separate, clearly named profile).

### 2. Preflight every pair

For each pair, from where its probe will run:

```bash
.claude/skills/measure-freshness/scripts/preflight-pair.sh <el-url> [beacon-url]
```

It checks chain id, client version, `eth_syncing`, head age, the EIP-2935
contract code **and** canary answer, whole-block `eth_getLogs`, CL sync /
optimistic / EL-offline, and the local clock. Any `FAIL` must be resolved
before probing; `WARN` goes into FEEDBACK.md and the analysis. Also confirm all
pairs report the **same chain id**, and preflight the reference URL too.

The probe repeats these checks itself at start and refuses unsynced nodes, but
catching problems here avoids wasted deployments.

### 3. Decide placement

Every probe response time includes the network path from the probe to its node.

1. **On the pair's host** (preferred): EL at `http://127.0.0.1:8545`, beacon at
   `http://127.0.0.1:5052` (or the client's port). Some hosts do not publish
   8545 publicly or bind it to loopback / a docker bridge — on-host avoids it.
2. **Nearby machine** (fallback): allowed, but the manifest's idle RTT baseline
   will show the added distance and review warns when RTT differences exceed the
   margin. State it in the analysis.

Never point probes at public or rate-limited RPC providers: two probes at 10 ms
are ~200 requests/s per node.

### 4. Write one probe config per pair

Copy `config/freshness/probe.example.yaml` next to the run outputs (not into
`config/`) as `probe-<pair-id>.yaml`. Set `pair.id`, `labels`, `el.url`,
`cl.beacon_url`, and leave the workload keys identical across pairs — review
warns when workloads differ. Key settings: `references/configuration.md`.

**Align the start.** Pick one `start.block` a few blocks above the current head
(e.g. head + 10 ≈ 2 minutes on mainnet) and put it in every config, so all
probes measure the same block range. A probe started after that block begins at
its next block with a warning; review still compares the overlapping range.

Leave `pair.host_id` empty (defaults to hostname) unless two probes on the
**same machine** need an explicit shared id. Probes with the same `host_id` are
treated as one clock domain; never give different machines the same id.

### 5. Deploy and run the probes

Cross-compile once and ship binary + config to each host:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/runner-linux ./runner
rsync -az /tmp/runner-linux probe-<pair-id>.yaml <user>@<host>:<remote-dir>/
```

Runs last hours, so detach them from the SSH session (`tmux`/`nohup`):

```bash
ssh <user>@<host> 'cd <remote-dir> && nohup ./runner-linux freshness probe \
  --config probe-<pair-id>.yaml --output results > probe.log 2>&1 &'
```

- Start all probes before `start.block`; each waits for it.
- While running, `probe.log` prints one line per block
  (`block N 0xhash… state_number=+612ms logs_number=+745ms`). Spot-check early
  blocks on every host: values should be positive and a few hundred ms to a
  few seconds; negative values mean a clock problem.
- The last stdout line is the probe's output directory
  (`results/<pair-id>-<run-id>/`). Ctrl-C / SIGTERM finalises it with outcome
  `interrupted`; it is still reviewable.
- `--record-all-attempts` logs every poll (large files) — only for calibration
  or debugging a specific pair.

Fetch every probe directory into `outputs/<date>-freshness-<name>/probes/`.
Check each `run-manifest.json`: `outcome` should be `completed`, `dropped_records`
all zero, `clock.source` not a fallback.

### 6. Review

Write `review.yaml` next to the outputs from `config/freshness/review.example.yaml`:
`reference.rpc_url`, the probe directories, `hold_constant` (e.g. `[cl, el]`),
`margin_ms`.

```bash
go run ./runner freshness review --config review.yaml
```

Review fetches reference data once into `<output>/reference-data/`. Re-render
without any node (same results byte-for-byte) with `--offline`; force a
re-fetch with `--refresh-reference`. Run review soon after the probes if the
reference prunes receipts.

### 7. Analyze and write ANALYSIS.md

Read `report.md` and `summary.json` (vocabulary in `references/outputs.md`).
Check in this order:

1. **Warnings** — workload mismatches, clock fallbacks/steps, RTT differences,
   dropped records, non-completed runs. Each qualifies every conclusion below it.
2. **Coverage** — blocks verified vs orphaned/unverified; per pair: measured
   denominator, `incorrect`, `timeout`, `late_armed`, `not_applicable`. A pair
   with many failures cannot "win" on percentiles alone.
3. **Freshness** — p50/p95/p99 per pair and probe; availability by deadline.
4. **Pairwise** — observed wins/ties vs inferred wins/unresolved, with
   denominators and median winning margin. Prefer `hold_constant` groups over
   the `all` group: the pair matrix is uneven, so never state an unconditional
   EL ranking from pooled results.
5. **Drill-down** — timelines of the largest-spread blocks; `block-results.jsonl`
   for anything specific.

Structure:

```markdown
# Freshness: <pairs> on <chain>
**Dates, block range, runner commit, client versions, placement per probe, reference.**
## Setup           (probes enabled, workload, clock quality per host, RTT baselines)
## Coverage        (verified/orphaned/unverified; per-pair failure counts)
## Results         (per probe: percentiles, availability, pairwise tables by group)
## Notable blocks  (largest spreads, failures, reorgs, with timelines)
## Caveats         (warnings, single-run variability, host placement)
## Bottom line
```

Say plainly that slot-relative freshness includes proposer timing and delivery,
and that one live run is one sample: repeat runs and rotate hosts before
attributing persistent differences to client software.

## Gotchas

- **Reference ≠ tested pair.** Using a tested node as reference makes it the
  definition of correct. Review cannot detect this; check the URL.
- **EIP-2935 required.** Pre-Pectra chains fail the state probes' preflight
  (`unsupported`), and the probe exits if no probe remains.
- **Negative freshness** on a live chain means the probe host's wall clock is
  ahead (data cannot exist before its slot). Check `clock` in the manifest.
- **Clock fallback.** `clock.mode: auto` needs `chronyc` or `timedatectl`;
  without them it uses `error_budget_ms` and review warns. For cross-host pairs
  the tie margin is raised to the combined clock error.
- **Local log check is necessary, not sufficient.** The probe stops on a
  bloom-consistent, stable answer; only review decides correctness. A missing
  log whose address/topics are already in the bloom passes locally and is caught
  by review as `incorrect`.
- **`late_armed`** means the node was already past a block when it would have
  been armed (probe lagging, node catching up). No latency is reported for it.
- **Warm-up blocks** (`warmup_blocks`) are excluded from review unless
  `include_warmup: true`.
- **Optional probes add load.** Hash-addressed variants start only once the
  probe's own node served the header, so they are usually left-censored
  (available by the first request, not a measured transition).

## Reference files

- `references/configuration.md` — probe and review config keys, defaults, when to change them.
- `references/outputs.md` — every output file, status vocabulary, and quick queries.
- `scripts/preflight-pair.sh` — per-pair readiness check before probing.
