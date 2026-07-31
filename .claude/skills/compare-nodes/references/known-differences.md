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

## Tooling trap — `--skip-above-head` drops hash-addressed calls

`pinnedBlock` claims hash-addressed calls return `ok=false`, but `hexBlock`
(`runner/comparator/block_override.go:95`) accepts any `0x` string and returns
`b.Uint64()`. Methods that take *either* a number or a hash at a `blockArgIndex`
position — notably **`eth_getBlockReceipts`** — therefore have a 32-byte hash
read as a huge integer truncated to its low 64 bits, which always exceeds the
head, and the call is silently skipped:

```
eth_getBlockReceipts_...-by-hash  skipped: "pinned block above lowest client
head", block 0x5db9f1cd715a1bdd     ← tail of the block hash, not a block number
```

Symptom: a suspiciously round number of skipped calls, all `*-by-hash`. Always
read `comparison-provenance.json` → `skipped` and sanity-check the `block`
values; anything near 2^63 is a hash, not a block. Workaround: omit
`--skip-above-head` when the call set contains hash-addressed
`eth_getBlockReceipts` and all blocks are known to be below the lower head.

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

## Repro snippet template

```bash
curl -s -X POST <endpoint> -H 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"<method>","params":<params>}'
```
