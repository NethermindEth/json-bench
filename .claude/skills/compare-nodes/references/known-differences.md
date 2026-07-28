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

## Repro snippet template

```bash
curl -s -X POST <endpoint> -H 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"<method>","params":<params>}'
```
