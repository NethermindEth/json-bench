# RPC freshness

`runner freshness` answers: for the same new block, which EL+CL pair lets an
application read **correct** new data first? Freshness is the time from the
block's slot start until the pair first returned data for it that was later
verified correct. Lower is better. It includes proposer timing, block
delivery, CL validation, EL execution and RPC exposure, so it is what an
application waiting on the chain actually experiences, not a pure
propagation or execution time.

Operational guide (placement, configs, deployment): the `measure-freshness`
skill in `.claude/skills/`. Config keys and output fields:
`.claude/skills/measure-freshness/references/`.

## How it works

**`freshness probe`** runs once per pair, on or next to its host. Before block
N exists, it polls every 10 ms:

- `state_number`: an `eth_call` to the EIP-2935 history contract at block N.
  The correct answer is the hash of N−1, which the probe already knows.
- `logs_number`: `eth_getLogs` for the whole of block N.

Early answers are the node's "block not known yet" error, captured at startup.
The first answer with data fixes the block's hash and timestamp. As soon as the
probe's own node serves N, it arms N+1; probes never wait for each other.

With a beacon URL it also records the CL's Beacon API events for each slot:

| Event | Meaning |
|---|---|
| `block_gossip` | The CL received the block and it passed gossip validation. Defined the same way for every CL. |
| `head` | The CL's fork choice selected the block as head. |
| `block` | The CL finished importing the block. |

These are post-validation milestones as seen by the probe, not network
arrival times. When `head` and `block` fire is client-specific: Lighthouse
emits `head` before `block`, Caplin `block` before `head`.

**`freshness review`** runs later, offline. It checks every block against a
separate reference node (the receipts must reproduce the header's receipts
root), then takes each pair's earliest response that matches, keeping its
original receive time. Pairs are matched by block hash.

## Reading the report

### Summary

- **Where the time goes**: per pair, median ms from slot start until its CL
  had the block (`block_gossip`), how long after that its data became
  readable, and the total. The bar shows the same split. Medians of the parts
  do not add up exactly to the median total.
- **Head to head**: one sentence per pair of pairs. It says who returned
  correct data first on how many blocks, and the median per-block difference.
  With beacon events, it then splits that difference into "the CL had the
  block earlier/later" and "after that, data was readable sooner/later".
  - **Same EL** (e.g. `erigon-caplin` vs `erigon-lighthouse`): the difference
    after `block_gossip` is attributed to the CL side. That covers validating
    the block, handing the payload to the EL, and fork choice.
  - **Same CL**: attributed mostly to the EL, plus host differences.
  - **Both differ**: not attributed.

### Details: per probe

**Freshness and coverage**

| Column | Meaning |
|---|---|
| Measured | Blocks the pair was measured on: correct, wrong, or no answer. Excludes empty blocks, blocks the probe armed too late, and unverified blocks. |
| Matched | Blocks where the pair returned correct data. |
| p50 / p95 / p99 / Min / Max | Freshness over matched blocks, ms from slot start. |
| Left-censored | Blocks where the first request already succeeded, so the data was ready at some unknown earlier time. |
| Outside slot | Matched, but only after the block's slot had ended. |
| Coverage | Count of every status (`matched`, `incorrect`, `timeout`, `not_applicable`, …). |

**Availability by deadline**: share of measured blocks answered correctly
within each deadline. Failures count against it.

**Pairwise**: one row per pair of pairs (A, B). Each count is a number of
blocks.

| Column | Meaning |
|---|---|
| Compared | Blocks both pairs were measured on. |
| First correct A / B / tie | Whose correct answer arrived first. A tie means the two were closer than the margin. |
| Certain A / B / unclear | Same, but it only counts a win when the two availability windows do not overlap. A window runs from the last "not ready" request to the first correct answer, widened by clock error. Overlapping windows are "unclear". |
| Only one correct A / B | Only that side ever returned correct data for the block. |
| Both failed | Neither returned correct data. |
| Median Δ A−B | Median of A's freshness minus B's, per block. Negative means A was earlier. |
| Median lead when A / B first | How far ahead the winner was, on the blocks it won. |
| Margin | Tie threshold. For pairs on different hosts it is at least their combined clock error. |
| Excluded | Blocks left out, by reason (e.g. `not_applicable`). |

Compare pairs by the median Δ and win counts, not by the difference between
their p50s. The medians of two distributions can order differently from the
per-block results.

**Context**

| Column | Meaning |
|---|---|
| Optimistic imports | Blocks the CL imported before its EL had validated them, usually because the EL was busy. Marked `O` in the per-block table. |
| Epoch-boundary / Other blocks | Freshness on the first slot of each epoch (CL epoch processing) vs all other blocks. Epoch-boundary blocks are marked `E`. |
| Lag vs state | This probe's freshness minus `state_number`'s on the same block, e.g. log indexing that runs after execution. |

**Split at CL milestones**: one row per pair and event.

| Column | Meaning |
|---|---|
| Blocks / Missing | Measured blocks with and without that event from the pair's CL. For example, a CL may emit no `head` after an optimistic import. |
| Milestone p50 / p95 | When the event arrived, ms from slot start. |
| Ready after p50 / p95 | Freshness minus the event time: how long data took to become readable counting from that event. Both times come from the same host, so this has no cross-host clock error. |

The paired table below it compares A and B per block, over blocks where both
answered correctly and both CLs emitted the event.

| Column | Meaning |
|---|---|
| Median milestone Δ A−B | A's event time minus B's. Negative means A's CL got there first. |
| Faster after milestone A / B / tie | Whose data became readable in less time, each counted from its **own** event. |
| Median Δ after milestone A−B | Median of A's "ready after" minus B's. Negative means A was faster from that point. |

"Faster after milestone" is only meaningful when the event marks the same
point in both clients. That is why pairs with **different CLs are compared at
`block_gossip` only**. An older report, from before that rule, showed why it
matters. The run compared `erigon-caplin` with `erigon-lighthouse`, the same
Erigon behind different CLs, over 50 blocks:

| Milestone | Milestone Δ | Faster after A / B / tie | What happened |
|---|---|---|---|
| `block_gossip` | +28.6 | 50/0/0 | Caplin had the block 29 ms later, yet its pair was readable sooner after it on every block. Genuine: the Caplin path is faster from gossip onwards. |
| `head` | −85.6 | 50/0/0 | Caplin emits `head` at the end of its import, 51 ms before data is readable. Lighthouse emits it earlier in its import, 237 ms before. The rows start the clock at different points, so the 50/0 says little. |
| `block` | −332.9 | 12/38/0 | Caplin emits `block` *before* fork choice (median +2024 ms), Lighthouse *after* `head` (+2381 ms). Lighthouse's event sits right next to readiness, so "faster after `block`" favours Lighthouse, even though the Caplin pair was readable ~290 ms sooner overall. |

The `head` and `block` rows measure where each client emits its event, not
which pair is faster, so the report no longer prints them across CLs.

### Per-block and timelines

The per-block table shows each pair's freshness (ms) or status for the first
probe. `*` means left-censored, `O` an optimistic import, and `E` in its own
column an epoch boundary. The timelines drill into the blocks where the pairs
differed most. For each probe they show the first request, the last
"not ready" answer and the first correct one, next to the CL events.

## What it cannot measure yet

**When the EL received the payload.** The report cannot split "after
`block_gossip`" into CL validation, payload delivery to the EL, EL execution
and fork choice. Doing it from outside the clients would mean polling the EL
by the block's execution hash from the moment the CL has the block. The
Beacon API does not expose that hash in time, though: `block_gossip` carries
only the beacon block root, and the block endpoints serve a block only after
import, which already includes handing the payload to the EL.

Instrumentation inside the clients could provide it, for example EL log or
metrics adapters, or an Engine API proxy for CLs that talk to the EL over HTTP.
Caplin calls Erigon internally, so it would need a hook inside Erigon. Any
"EL received the payload" milestone should only be compared between pairs with
the **same EL**. When an EL records a payload as received, and what it has
done with it by then, differs between clients. With the same EL, the
milestone isolates the CL's delivery; across different ELs it would mix CL
delivery with EL internals.
