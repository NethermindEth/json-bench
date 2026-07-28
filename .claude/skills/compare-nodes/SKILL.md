---
name: compare-nodes
description: >-
  Cross-node / cross-client JSON-RPC CORRECTNESS comparison using this repo's
  `runner compare`. Use this whenever the user wants to check that two Ethereum
  RPC endpoints return the SAME responses — e.g. "does the new node behave like
  the old one", "compare these two archives / clients", "verify the resync /
  new implementation is correct", "diff the RPC responses", "check response
  equivalence", "did the upgrade change any results". Trigger it even when the
  user doesn't say "compare" — any equivalence/correctness/regression check
  between two endpoints qualifies. This is NOT performance testing; for latency
  or throughput use the run-benchmark skill instead.
---

# Compare nodes for correctness

Drive this repo's `runner compare` to check that two Ethereum JSON-RPC endpoints
return equivalent responses for the same calls, and produce a categorized
findings report that separates **real defects** from expected version /
configuration / sync-gap differences.

The goal is signal, not raw diffs. Two correct clients differ in benign ways
(serialization, gas-estimate rounding, error wording); a still-syncing node
lacks recent blocks; nodes have different config limits. The job is to run the
comparison so those are suppressed or classified, and only genuine correctness
problems survive to the report.

## When to use vs. not

- **Use** when the user wants response equivalence between two endpoints:
  old vs new node, two clients (Nethermind/Geth/Reth/…), pre/post upgrade, a
  fresh resync vs a trusted archive, a new implementation vs a reference.
- **Do not use** for latency/throughput/load — that's the `run-benchmark`
  skill. If the user wants both, do correctness here first, then benchmark.

## Prerequisites

- Build works: `go build ./runner/...` from the repo root.
- A clients registry (`config/clients/clients.yaml` or a purpose-made one). Each
  client is `{name, type, url, timeout, max_retries}`. **Order matters
  downstream:** the diff reference is the *first* client passed to
  `--client-refs`, so list the trusted/baseline endpoint first.

## Workflow

Follow these in order. Steps 1–2 are the "brain" (they decide what's even
comparable); the tool handles the mechanics from step 4.

### 1. Frame the comparison

Get the two endpoints and their roles from the user. Decide which is the
**baseline/reference** (usually the older, trusted, or fully-synced one) — it
goes first in `--client-refs`, and every difference is reported relative to it.

### 2. Probe the nodes and pick a static historical block

Never compare at `latest`: two nodes are almost never at the same head, so
head-relative calls diverge for reasons that aren't correctness. Instead pin
everything to one historical block that **both** nodes have.

Run the probe helper (it checks reachability, same chainId, both heads, sync
state, client versions, then proposes a pin block and verifies both nodes return
the *same block hash* there):

```bash
.claude/skills/compare-nodes/scripts/probe-nodes.sh <baseline-url> <candidate-url>
```

Rules for the pin block:
- Must be **≤ the lower of the two heads** (the still-syncing node is the
  limiter). The probe uses the lower head.
- Prefer **historical, not recent**: at least a few thousand blocks below the
  lower head, so it's settled state, not near-tip reorg territory. The probe
  targets a round number well under the lower head.
- Both nodes must return an identical block hash at that block (the probe
  asserts this). If they don't, stop — the nodes disagree on history and that
  itself is the finding.

Record the chosen block (hex) — you'll pass it as `--block-override`.

### 3. Choose the call set

Two ways to feed calls; prefer the corpus for breadth.

- **Corpus (default):** ingest the checked-in request corpora with
  `--from-jsonl rpc-calls --sample <N>`. It recurses, reads `*.jsonl` and
  `*.json` arrays, samples deterministically per method, and already excludes
  methods that can't be compared cross-node: `eth_getProof` (proofs may not be
  stored) and head-dependent zero-arg methods (`eth_gasPrice`, `eth_syncing`,
  `eth_blockNumber`, `eth_maxPriorityFeePerGas`). Start with `--sample 60–150`
  per method to keep runtime and output sane; raise it for a thorough pass.
- **Curated config:** a hand-written `config/compare/*.yaml` (`calls:` map) when
  the user wants a specific, small set. See `config/compare/example.yaml`.

`debug_*` / trace methods: the corpus loader skips the `debug_` prefix, and
these often need the `Debug` JSON-RPC namespace enabled on *both* nodes. Only
add them (via a curated config) after confirming both nodes answer them; trace
output can also differ by client version for non-correctness reasons.

### 4. Declare expected differences (rules file)

This is what turns a wall of diffs into a short findings list. Write a small
rules file and pass it with `--rules`; it merges into either config mode.

```yaml
# rules.yaml
comparison:
  block_override: "0x1406f40"   # optional; usually pass --block-override instead
  rules:
    - path: result.totalDifficulty      # benign serialization difference
      kind: ignore
    - method: eth_estimateGas            # gas estimates round differently
      path: result
      kind: numeric_tolerance
      abs: 32
      rel: 0.10
    - method: eth_call                   # benign error-wording drift
      kind: error_presence_only
```

Rule kinds: `ignore` (drop a JSON path; supports `[*]` array wildcards),
`numeric_tolerance` (`abs` and/or `rel` on hex quantities), `error_code_only`
(compare only the error code), `error_presence_only` (any two errors are equal).
`eth_estimateGas` already gets a built-in 10% tolerance even with no rule.

Which differences are expected vs. real is accumulated knowledge — read
`references/known-differences.md` before deciding what to ignore, and add to it
when you learn something new.

### 5. Run the comparison

Canonical invocation (corpus mode). Write everything to a dedicated, dated
output folder so configs, results, provenance, and analysis stay together:

```bash
OUT=outputs/$(date +%Y-%m-%d)-<slug>
go run ./runner --output "$OUT" compare \
  --from-jsonl rpc-calls --sample 100 \
  --clients "$OUT/clients.yaml" --client-refs baseline,candidate \
  --block-override 0x1406f40 \
  --rules "$OUT/rules.yaml" \
  --skip-above-head \
  --diff-only \
  --concurrency 4 --timeout 60 \
  --fail-on-diff
```

Why these flags:
- `--concurrency 4` (low): remote nodes commonly throttle connection bursts;
  high concurrency trips per-IP limits and causes flaky transport errors.
  Retries with backoff are built in and absorb the occasional blip.
- `--skip-above-head`: drops calls pinned to a numeric block above the lower
  head so a still-syncing node doesn't generate false "missing block" diffs.
  (Hash-addressed calls can't be checked this way — see gotchas.)
- `--diff-only`: excludes identical calls and caps response bodies, so the
  report stays small. Without it, embedding every full block body from both
  nodes can produce a multi-hundred-MB HTML. Add `--keep-response-bodies` only
  if you need the full payloads for a differing call.
- `--fail-on-diff`: exit non-zero when *real* (non-environment) differences
  remain — useful as a CI gate. Add `--fail-on-env-diff` for strict mode.

Outputs written to `$OUT/`: `comparison-results.json`, `comparison-report.html`,
`comparison-provenance.json` (the effective config — rules, block override,
skipped calls), plus a run summary logged to stdout.

Full flag reference: `references/flags.md`.

### 6. Analyze and categorize

The tool's summary line tallies `identical / differ / transport-error /
schema-error / skipped` plus env/capability buckets. Don't stop at the count —
open `comparison-results.json` and put every surviving difference into one of:

1. **Benign version difference** — same data, different serialization
   (e.g. `totalDifficulty` present on one). Add an `ignore` rule and re-note.
2. **Within-tolerance numeric drift** — `eth_estimateGas` and friends. Covered
   by tolerance rules.
3. **Config / capability** — one node rejects by policy: namespace disabled
   (`-32601`/`-32600`), `getLogs` range cap (`-32602`), no state for a block
   (`-32002`). The tool classes these as env errors; they're **not** correctness
   defects. Report them as configuration deltas to align, not bugs.
4. **Sync gap** — candidate is still syncing; a block/tx above its head is
   absent. Expected; `--skip-above-head` catches the numeric ones.
5. **Real defect** — anything left. Root-cause it before reporting: probe the
   boundary (vary the block, the account, the slot, adjacent methods) to find
   the smallest reproducing case and confirm it's deterministic (some failures
   are transient races that vanish on retry). Capture a copy-paste `curl`.

`references/known-differences.md` catalogs the categories seen so far with
example rules and repro snippets.

### 7. Write ANALYSIS.md

Put a concise findings doc in the output folder. Structure:

```markdown
# <baseline> vs <candidate> correctness comparison
**Endpoints / versions / chain / date. Pin block.**
## What was tested   (call set, sample size, exclusions, rules applied)
## Result            (identical / differ counts; one-line verdict)
## Findings          (table: category | count | verdict; then a section per
                      REAL defect with repro curl + root cause)
## Config observations (namespace/limits deltas that aren't defects)
## Bottom line
```

Lead with the verdict (equivalent or not), keep benign categories to one line
each, and give every real defect its own section with a reproduction command.

## Gotchas

- **Diff reference is `--client-refs`[0].** First client = baseline; all diffs
  are relative to it. Get the order right.
- **Report size.** The report embeds full responses; a block-heavy run without
  `--diff-only` is enormous. Keep `--diff-only` on; the JSON is the source of
  truth, the HTML is a convenience.
- **Throttling nodes.** If you see transport errors, lower `--concurrency`, not
  raise it. The old/baseline node in past runs refused bursts and recovered
  after a cooldown.
- **`--skip-above-head` is numeric-only.** Hash-addressed calls
  (`eth_getBlockByHash`, `debug_traceTransaction`) referencing a block above the
  candidate's head can't be pre-filtered and will legitimately show as diffs
  (candidate returns `null`); recognize these as sync-gap, not defects.
- **`earliest` = block 0.** Genesis-state calls exercise a distinct code path
  and have surfaced transient errors; treat a single occurrence skeptically and
  re-probe before calling it a bug.

## Reference files

- `references/known-differences.md` — catalog of expected vs. real differences,
  example rules, and repro snippets. Read before writing rules.
- `references/flags.md` — full `runner compare` flag and `comparison:` config
  reference.
- `scripts/probe-nodes.sh` — endpoint probe + pin-block selection/verification.
