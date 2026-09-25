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

For each pair, from where its probe will run (copy the script there first,
e.g. `scp .claude/skills/measure-freshness/scripts/preflight-pair.sh <host>:`;
it only needs `bash`, `curl` and `python3`):

```bash
./preflight-pair.sh <el-url> [beacon-url]
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
   the CL's HTTP port. Check the actual port: defaults are Lighthouse/Nimbus
   5052, Prysm 3500, Teku 5051, Lodestar 9596, and deployment tools override
   them (sedge uses 4000).
   **Erigon with embedded Caplin:** the beacon API is off unless Erigon runs
   with `--beacon.api=…` including at least `beacon,config,node,events`; the
   port is `--beacon.api.port` (default 5555) and it binds to
   `--beacon.api.addr` (default localhost), so probe it on the host.
   Confirm events flow with
   `curl -N 'http://127.0.0.1:<port>/eth/v1/events?topics=head,block,block_gossip'`.
   Some hosts do not publish
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

**Warm-up is extra.** `warmup_blocks` are measured *before* the `block_count`
blocks, starting at `start.block`, and excluded from review. The example's 32 is
for long runs; use 2–4 for pilot runs of 20–50 blocks.

**Disk.** Budget about 70 KB per block per probe (~70 MB per 1000 blocks; response bodies are
gzipped); with `--record-all-attempts`, considerably more.

**Align the start.** Pick one `start.block` above the current head and put it
in every config, so all probes measure the same block range. Choose it only
once the binary and configs are on every host: a first deploy to several hosts
can take minutes (26 MB per host), so allow at least head + 15 then; head + 5
is enough for later runs. A probe started after that block begins at its next
block with a warning; review still compares the overlapping range.

Leave `pair.host_id` empty (defaults to hostname) unless two probes on the
**same machine** need an explicit shared id. Probes with the same `host_id` are
treated as one clock domain; never give different machines the same id.

### 5. Deploy and run the probes

Cross-compile once and ship binary + config to each host:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/runner-linux ./runner
rsync -az /tmp/runner-linux probe-<pair-id>.yaml <user>@<host>:<remote-dir>/
```

Runs last hours, so detach them from the SSH session. Use `;` (not `&&`) and
redirect stdin, otherwise ssh keeps the session open until the probe exits:

```bash
ssh <user>@<host> 'cd <remote-dir>; nohup ./runner-linux freshness probe \
  --config probe-<pair-id>.yaml --output results > probe.log 2>&1 < /dev/null &'
```

(`tmux new -d -s probe '…'` or `setsid` work too.)

- Start all probes before `start.block`; each waits for it and logs
  `waiting for start.block N (head M, ~Ts)` once per slot meanwhile.
- While running, `probe.log` prints one line per block
  (`block N 0xhash… state_number=+612ms logs_number=+745ms`). Spot-check early
  blocks on every host: values should be positive and a few hundred ms to a
  few seconds; negative values mean a clock problem.
- **Waiting for completion:** a probe is done when `probe.log` contains
  `probe output written to`. Do not poll with `pgrep -f "runner-linux freshness
  probe"` over ssh: it matches the ssh command line itself and never goes
  away. Use `pgrep -x runner-linux` or the log line.
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

For a final review, wait until the last measured block is finalized (about two
epochs, ~13 minutes on mainnet): earlier reviews are correct but report blocks
as `verified_not_finalized`. Reviewing right away for a first look is fine;
re-run with `--refresh-reference` later.

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
5. **Context** — per pair: optimistic imports (the CL imported before its EL
   validated, usually a busy EL), freshness on epoch-boundary blocks vs the
   rest, and for non-state probes the lag behind `state_number` (asynchronous
   log indexing shows up here: Erigon 3.6 trails by 0.6–0.9 s at p50).
6. **Split at CL milestones** (probes with a beacon URL) — per pair, when its
   beacon node emitted `block_gossip` / `head` / `block` and how long data took
   to become readable after each, plus paired deltas. This separates "this
   pair's CL saw the block earlier" from "this pair's EL served it faster
   after that". The milestone deltas are cross-host (clock error applies); the
   ready-after deltas are same-host and clock-error free. Event order and
   meaning differ by CL (Lighthouse emits `head` before `block`, Caplin
   `block` before `head`), so pairs with different CLs are compared at
   `block_gossip` only. A non-zero `Missing` count means the CL skipped an
   event on some blocks — e.g. no `head` after an optimistic import.
7. **Drill-down** — timelines of the largest-spread blocks; `block-results.jsonl`
   for anything specific. Blocks marked `E` are the first slot of an epoch:
   check epoch processing before attributing their delay to block contents
   (blobs, gas). Cells marked `O` are optimistic imports: look at that EL's
   logs around the slot (pruning, forkchoice stalls).

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

- **Node log volume.** Every not-ready poll is an RPC error on the node, and
  some clients log each one (Erigon: `WARN [rpc] served … block not found`,
  ~10k lines/min at the default cadence, ~2M lines per 1000-block run). Check
  the node's log rotation and disk before a long run.
- **Pruned nodes and genesis.** Nodes that pruned history cannot serve block 0,
  so the probe and review warn that genesis identity is unconfirmed. Chain id
  is still checked; the warning is harmless for pruned full nodes.

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
- **Number-addressed probes wait for fork choice.** `state_number` and
  `logs_number` succeed only after the CL's forkchoiceUpdated makes N
  canonical, so slow CL import (data availability, epoch processing) shows up
  as EL "freshness". Use the CL split before blaming the EL.
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
