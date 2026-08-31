# Known differences catalog

Which cross-node differences are **expected** (suppress or classify) vs. a
**real defect** (investigate and report). Read before writing a rules file.
Append to this as you learn new ones — it's the accumulated memory of the skill.

## Expected — benign version / serialization

These are the same underlying data rendered differently by two client versions.
Suppress with an `ignore` rule so they don't drown the real findings.

- **`result.totalDifficulty`** — present on some versions, `null`/absent on
  others (post-merge it became optional). Seen flooding ~every block query
  (`eth_getBlockByNumber`, `eth_getBlockByHash`). Rule:
  ```yaml
  - path: result.totalDifficulty
    kind: ignore
  ```
  If you find other version-only header fields, add them the same way (use
  `result.transactions[*].<field>` for per-tx fields).

## Expected — numeric drift within tolerance

- **`eth_estimateGas`** — estimates round differently between versions; observed
  drift of ~1–21 gas units, always small. `eth_estimateGas` gets a **built-in
  10% relative tolerance** automatically. Tighten/loosen explicitly if needed:
  ```yaml
  - method: eth_estimateGas
    path: result
    kind: numeric_tolerance
    abs: 32
    rel: 0.10
  ```

## Expected — benign error wording / code drift

- **Execution-rejection errors** (`eth_call`, `eth_estimateGas`) — same
  rejection, different phrasing/code across versions, e.g. code `-32003` → `-32000`
  and `"insufficient sender balance for gas * price + value"` →
  `"insufficient funds for gas * price + value"`. Both nodes correctly reject.
  Suppress with:
  ```yaml
  - method: eth_call
    kind: error_presence_only     # any two errors are equal
  ```
  Use `error_code_only` instead if you want to still catch a *code* change but
  ignore message wording (note: it will NOT suppress a code drift like the one
  above — use `error_presence_only` for that).

## Expected — configuration / capability deltas (NOT defects)

One node refuses by policy while the other serves. Report these as
**config to align**, not bugs. The tool auto-classifies them as env/capability
errors (they appear in the summary's env buckets, not as real diffs under
`--fail-on-diff`).

- **`Debug` namespace disabled** (`-32601`/`-32600`, "namespace 'Debug' is
  disabled for http") — all `debug_trace*` rejected on that node. Can't compare
  traces until it's enabled on both. Class: `namespace_disabled`.
- **`eth_getLogs` block-range cap** (`-32602`, "Block range N exceeds the
  maximum of 1000 blocks per logs request … increase Receipt.MaxBlockDepth") —
  one node caps per-request range, the other doesn't. Log *data* matches within
  the allowed range. Class: `range_cap`. To align, raise `Receipt.MaxBlockDepth`
  on the stricter node. Note both nodes may share a separate
  `Max logs per response is 20000` cap that is already aligned.
- **No state for a block** (`-32002`, "No state available for block N") — the
  node doesn't retain state for that block (e.g. genesis after a state-sync).
  Class: `no_state`. Expected for non-archive / still-syncing nodes.
- **Pruned state under a reused `-32000`** — Geth's "missing trie node 0x…" and
  Nethermind's "Historical state for block N is unavailable" are the same
  condition reported under a generic code. Both classify as `no_state`. Matching
  is by phrase, because `-32000` also carries genuine execution errors
  (`max fee per gas less than block base fee`, `insufficient funds`) which stay
  real differences.
- **Pruned log/receipt history** (`4444`, "Pruned history unavailable") — the
  node dropped the history index the call needs. Class: `pruned_history`. Expect
  it on a flat-history / windowed-retention node.
- **Node-side RPC timeout** (`-32016`, "eth_getLogs request was canceled due to
  enabled timeout") — the node aborted the request on its own internal deadline,
  which is a capacity/config delta, not a wrong answer. Class:
  `internal_timeout`. Narrow the range or raise the node's RPC timeout.
- **HTTP 429 is not an error class.** A rate-limited provider never returns a
  response, so it lands in `transport_errors` with
  `transport_error_class: rate_limited` and the call is excluded from the
  identical/differ tallies. Pace the run with `--rate-limit` instead of reading
  those calls as findings.

## Expected — sync gap

- Candidate still syncing → a block/tx above its head is absent
  (`eth_getBlockByHash` → `null`, tx lookups fail). `--skip-above-head` drops the
  numeric-block calls automatically; **hash-addressed** calls can't be
  pre-filtered and will show as diffs — recognize them as sync-gap by resolving
  the referenced block number and confirming it's above the candidate's head.

## Real defects — investigate and report

Anything not in the categories above. Before reporting:
1. **Minimize** — vary one parameter at a time (block, account, storage slot,
   adjacent method) to find the smallest reproducing input and the true scope
   (e.g. "only at block 0", "only this method, not its siblings").
2. **Confirm determinism** — re-run. Some failures are transient races. Observed
   once: `eth_getStorageAt` at `earliest` returned `-32603 Internal error …
   State … no longer exists; concurrently removed` (FlatDb path) while sibling
   state methods returned a clean `-32002`; on re-probe it had resolved. A single
   transient occurrence is not yet a confirmed bug — say so.
3. **Capture a curl** — exact reproduction against the failing endpoint.
4. **Contrast siblings** — if `eth_getStorageAt` fails where `eth_getBalance`/
   `eth_getCode`/`eth_call` degrade gracefully at the same block, that
   divergence (wrong error code, uncaught exception, leaked stack trace in
   `error.data`) is itself the finding.

## Tooling trap (FIXED) — `--skip-above-head` dropped hash-addressed calls

`hexBlock` used to accept any `0x` string, so methods taking *either* a number
or a hash at a `blockArgIndex` position — notably **`eth_getBlockReceipts`** —
had a 32-byte hash read as a huge integer, which always exceeds the head, and
the call was silently skipped.

**Fixed:** `maxQuantityHexLen` (`runner/comparator/block_override.go`) now caps
the accepted length at `0x` + 16 hex digits, so a 66-char hash returns
`ok=false` and the call is compared instead of skipped. Verified 2026-08-31 on a
Gnosis run containing 76 `eth_getBlockByHash` and 150 `eth_getBlockReceipts`
fixtures: `comparison-provenance.json` → `skipped: null`.

Still worth the habit: read `skipped` in the provenance and sanity-check the
`block` values; anything near 2^63 would be a hash, not a block.

## Tooling trap (FIXED) — a truncated body silently dropped the call

Symptom: `transport-error` counts in the hundreds, all
`failed to parse response: unexpected end of JSON input`, classed as `other`,
concentrated on the methods with the largest responses (`trace_*`). Replaying
the same calls serially succeeds, and the responses turn out to be *small*
(hundreds of bytes) — so it is neither a bad fixture nor a size limit.

Cause was two bugs compounding in `runner/comparator/transport.go`:

1. The client used `http.DefaultTransport`, whose 90s `IdleConnTimeout` hands
   out keep-alive connections the node has already half-closed. Go does not
   silently retry a POST the way it retries idempotent methods, so the read
   comes back cut short mid-JSON.
2. The `json.Unmarshal` failure `return`ed immediately instead of `continue`-ing
   the retry loop the way transport errors, read errors and 5xx/429 do — so the
   call spent **none** of its attempt budget and was dropped from the comparison.

**Fixed 2026-08-31:** idle connections are retired after 5s
(`freshConnTransport`), and an unparseable 200 is now retried and classified as
`TransportTruncated` (`truncated_body`) rather than the anonymous `other`.
Observed before/after on the same 1354-call Gnosis run at `--concurrency 3`:
192 lost calls (64% of all `trace_*`) → 0.

**The general lesson survives the fix:** always read the transport-error count
before the diffs, and always pass `--fail-on-transport-error`. Without that flag
a run that lost an arbitrary fraction of its calls still exits 0 and reports a
clean "0 differ".

## Real defect seen — derived receipts failing consensus validation (Nethermind)

Nethermind run with `--Receipt.DeriveFromState=true` (no stored receipt
payloads) re-executes blocks to rebuild receipts, then checks the derived
receipts root against the header's `receiptsRoot`. On mismatch it refuses to
serve. The RPC error is **misleading** about the cause:

- `eth_getBlockReceipts` / `eth_getTransactionReceipt` → `-32602`
  `"Receipts for block N (0x…) are neither stored nor reproducible from state history."`
- `eth_getLogs` → `4444` `"Pruned history unavailable"`

Both read as *missing history*; the real reason is only in the node log:

```
ERROR|Consensus.Receipts.ReceiptsRegenerator| Regenerated receipts for block N
hash to 0x<derived> but the header commits to 0x<expected>. Refusing to serve them.
```

**Do not classify these as `no_state` / pruning / sync-gap env errors.** Check
the node's own log before deciding. Observed 2026-07-31 on a v1.40.0-unstable
mainnet archive: 0/150 pre-merge failures vs 37/500 post-merge (7.4%), failures
deterministic and clustered in contiguous windows, the first beginning exactly
at the Merge block 15,537,394. Ruled out by direct probe at affected blocks:
`PREVRANDAO`, `BLOCKHASH`, missing state, and block structure — all identical to
the baseline.

Blast radius is wider than the receipt methods: Nethermind resolves
`eth_getTransactionByHash` through `BlockchainBridge.TryGetCanonicalTransaction`,
which loads receipts, so plain transaction lookup fails too; and `eth_getLogs`
fails for **any range overlapping a single affected block**.

Corollary for call-set design: the checked-in corpora contain **no receipt
methods at all**, so `--from-jsonl` will not exercise receipts. Testing a
receipt-storage or receipt-derivation change needs a hand-built config anchored
to blocks spanning every receipt-format era (pre-Byzantium `root`, Byzantium
`status`, Berlin typed txs, London `effectiveGasPrice`, the Merge, Shanghai
withdrawals) plus contract-creation txs for the computed `contractAddress`.
See `outputs/2026-07-31-archives-receipts/gen-receipts-config.py` for a generator.

## Real defect seen — Erigon returns `status` instead of `root` pre-Byzantium

Below Byzantium (block 4,370,000) consensus receipts carry a post-state `root`
and have **no** `status` field (EIP-658 introduced `status` at Byzantium).
Nethermind follows this; **Erigon 3.5.4 omits `root` and emits a synthesized
`status` instead**, on every pre-Byzantium receipt:

```
block 4,000,000, 69 receipts
  nethermind : root present 69, status present  0
  erigon     : root present  0, status present 69
```

The status is accurate, not blindly `0x1` — genuinely failed pre-Byzantium txs
come back `0x0` — so this is a **serialization / spec-compliance deviation, not
wrong data**. It still matters: the post-state root is unrecoverable from Erigon,
so the pre-Byzantium receipts trie cannot be rebuilt from its output.

Do **not** blanket-ignore this; it fires on every pre-Byzantium receipt and will
swamp a broad historical run. Scope any suppression to the affected range, and
keep it visible if pre-Byzantium receipt fidelity is what's being tested.

## Expected — `eth_getLogs` response cap differs by client

Erigon: `-32602 "query returns too many logs, narrow your filter: 20000"`.
Nethermind served the identical queries. Distinct from the *block-range* cap
already documented above — this one is on the **response size**. Around the Merge
a single block can emit >10,000 logs (block 15,537,392 → 10,058), so even a
10-block range trips it. Class: `range_cap`, config delta, not a defect.

## Real defect seen — Nethermind emits malformed JSON for concurrent `trace_*`

Under concurrent load a `trace_*` request returns **HTTP 200 with a body that is
not valid JSON** — `Content-Length` matches what arrives, so this is the node
emitting a bad response, not a truncated read:

```
HTTP/1.1 200 OK
Content-Length: 28

{"jsonrpc":"2.0","result":[]        ← no closing brace, no "id"
```

A successful response uses `Transfer-Encoding: chunked`; the failing one
switches to a buffered `Content-Length` path, so the serializer appears to bail
out after writing the `result` array opening.

Observed 2026-08-31 on Gnosis, in **both** Nethermind v2.0.0-rc and
v1.40.0-unstable at identical rates, so it is neither a regression nor a
cross-node difference:

| condition | truncated |
| --- | --- |
| `trace_block` conc 1 / 4 / 8 | 0/1, 2/4, 6/8 |
| `eth_getBlockByNumber` (full txs), same block, conc 8 | 0/8 |
| `eth_getBlockReceipts`, same block, conc 8 | 0/8 |

Trace-namespace-specific — other methods returning comparably large payloads for
the same block are clean at the same concurrency. Distinguish it from the fixed
tooling trap above by the byte count in the error message (a fixed, small,
repeating size) and by `Content-Length` agreeing with the delivered body.
Workaround while it stands: compare `trace_*` at `--concurrency 1`.

## Expected — `eth_call` without `gas` inherits the node's default cap

An `eth_call` whose params omit a `gas` field is executed with whatever cap the
node is configured with, so any contract that observes `gasleft()` — routers,
aggregators and anything doing a gas-budgeted sub-call — returns a **different
answer per node**. It reads as a real correctness difference and it is not.

Seen 2026-08-31 comparing two Nethermind versions on Gnosis: 2 of 1304 replayed
`eth_call` fixtures differed, in the final 32-byte word only, with the first
words identical:

```
trie_archive (v2.0.0-rc)        0x23bd7860 = 598,738,528   (cap ~600M)
flat_history (v1.40.0-unstable) 0x05f01360 =  99,881,824   (cap ~100M)
```

Diagnostic: re-issue the call with an explicit `gas`. If both nodes then agree,
it is the cap, not the execution. In the case above, adding `"gas":"0x1c9c380"`
made both return `0x01c3f5e0` identically.

Fix it in the corpus rather than suppressing it with a rule — a fixture whose
result depends on node configuration is not a stable comparison input.
`generate-from-chain` pins each replayed transaction's own gas limit for exactly
this reason. If you cannot regenerate, scope an `ignore` rule to the affected
path, never to `result` wholesale.

## Expected — head-relative calls race with block production

`eth_getProof` at `latest` (and any other head-targeted call) compares two nodes
against whatever block each is on. A run that straddles a block change reports
differences in `result.accountProof[0]` — the proof **root**, i.e. two different
state roots, not different data. Storage values, balance and nonce still match.

Do not "fix" this with `--block-override`: rewriting `latest` to a pinned height
turns every call into a *historical* proof, which a flat-state archive cannot
serve at all. Instead read both heads immediately before and after the run and
re-run until it lands inside one block; confirm by replaying a differing call
directly against both nodes while their heads agree.

## Repro snippet template

```bash
curl -s -X POST <endpoint> -H 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"<method>","params":<params>}'
```
