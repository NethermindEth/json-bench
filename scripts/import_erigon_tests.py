#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Port the erigontech/rpc-tests corpus into json-bench `compare` configs.

json-bench's `compare` is *differential* (it diffs each client's response
against the others, not against a golden file), so this importer keeps only the
requests from the Erigon corpus and drops the Erigon-captured expected
responses. The value it adds is coverage (100+ methods, thousands of real
mainnet requests) and, crucially, **every ported test is runnable on any synced
node**: the Erigon tests pin specific historical blocks (which only an archive
node can answer), so this importer rewrites each test's block reference to a
runnable target (default `latest`) — no archive dependency.

Block handling:
  * A method's block-number / block-tag argument is rewritten to the target
    (via a per-method arg map, mirroring the comparator's blockArgIndex and
    extended; eth_getLogs' fromBlock/toBlock are set to the target too).
  * Requests addressed by an immutable HASH (block hash, tx hash) that only
    FETCH data (getBlockByHash, getTransactionByHash, receipts, …) are kept
    as-is — any full node retains that data.
  * Requests that REPLAY a fixed point by hash (trace_transaction,
    debug_traceTransaction, debug_traceBlockByHash, …) cannot be retargeted to
    a live block and are DROPPED (reported in the manifest).

Output buckets (both any-block runnable):
  core       standard eth_/debug_ methods — compare cleanly across clients
  divergent  namespaces/methods that do not compare cleanly cross-client
             (erigon_/ots_/parity_/engine_/admin_/txpool_/trace_, node-local,
             gas-price oracle) — kept separate, informational

Usage:
  python scripts/import_erigon_tests.py --corpus /path/to/rpc-tests/integration/mainnet \
      --network mainnet --out config/compare/erigon [--target-block latest]
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
from collections import defaultdict

try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required: pip install pyyaml")

BLOCK_TAGS = {"latest", "pending", "safe", "finalized", "earliest"}
HASH_RE = re.compile(r"^0x[0-9a-fA-F]{64}$")   # 32-byte block/tx hash
NUM_RE = re.compile(r"^0x[0-9a-fA-F]{1,15}$")  # block number (short hex)

DIVERGENT_PREFIXES = ("erigon_", "ots_", "parity_", "engine_", "admin_", "txpool_", "trace_")
DIVERGENT_METHODS = {
    "web3_clientVersion", "eth_coinbase", "eth_mining", "eth_getWork",
    "eth_submitWork", "eth_submitHashrate", "eth_syncing", "eth_hashrate",
    "net_listening", "net_peerCount", "eth_getFilterChanges", "eth_getFilterLogs",
    "eth_sendRawTransaction", "eth_fillTransaction", "eth_gasPrice",
    "eth_maxPriorityFeePerGas", "unknown_method",
}

# Replay/state addressed only by a fixed hash — cannot be retargeted to a live
# block, so not runnable on an arbitrary node. Dropped from the suite.
DROP_HASH_REPLAY = {
    "trace_transaction", "trace_replayTransaction", "debug_traceTransaction",
    "debug_traceBlockByHash", "debug_getModifiedAccountsByHash",
    "ots_traceTransaction", "ots_getInternalOperations", "ots_getTransactionError",
}

# Positional index of the block-number/tag argument (rewritten to the target).
# eth_getLogs is handled specially (fromBlock/toBlock in the filter object).
BLOCK_ARG_INDEX = {
    "eth_call": 1, "eth_estimateGas": 1, "eth_createAccessList": 1,
    "eth_getBalance": 1, "eth_getCode": 1, "eth_getTransactionCount": 1,
    "eth_getStorageAt": 2, "eth_getProof": 2, "eth_getBlockByNumber": 0,
    "eth_getBlockReceipts": 0, "eth_getBlockTransactionCountByNumber": 0,
    "eth_getTransactionByBlockNumberAndIndex": 0,
    "eth_getRawTransactionByBlockNumberAndIndex": 0,
    "eth_getUncleByBlockNumberAndIndex": 0, "eth_getUncleCountByBlockNumber": 0,
    "eth_feeHistory": 1, "eth_simulateV1": 1,
    "debug_traceBlockByNumber": 0, "debug_traceCall": 1, "debug_accountAt": 0,
    "debug_accountRange": 0, "debug_storageRangeAt": 0,
    "debug_getModifiedAccountsByNumber": 0, "debug_getRawBlock": 0,
    "debug_getRawHeader": 0, "debug_getRawReceipts": 0,
    # divergent namespaces, best-effort so they are any-block too:
    "trace_block": 0, "trace_call": 2, "trace_replayBlockTransactions": 0,
    "erigon_getHeaderByNumber": 0, "erigon_getBalanceChangesInBlock": 0,
    "ots_getBlockDetails": 0, "ots_getBlockTransactions": 0,
}


def is_divergent(method: str) -> bool:
    return method in DIVERGENT_METHODS or method.startswith(DIVERGENT_PREFIXES)


def remap_block(method: str, params: list, target: str) -> list:
    """Return params with any block-number/tag argument rewritten to target."""
    if not isinstance(params, list):
        return params
    out = list(params)
    if method == "eth_getLogs" and out and isinstance(out[0], dict):
        f = dict(out[0])
        if "blockHash" not in f:  # a blockHash filter is an immutable point — leave it
            for key in ("fromBlock", "toBlock"):
                if key in f or "blockHash" not in f:
                    f[key] = target
        out[0] = f
        return out
    idx = BLOCK_ARG_INDEX.get(method)
    if idx is not None and idx < len(out):
        v = out[idx]
        if isinstance(v, str) and (v in BLOCK_TAGS or NUM_RE.match(v)):
            out[idx] = target
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--corpus", required=True, help="path to rpc-tests integration/<network> dir")
    ap.add_argument("--network", default="mainnet")
    ap.add_argument("--out", required=True)
    ap.add_argument("--target-block", default="latest",
                    help="block tag/number every test is retargeted to (default: latest)")
    ap.add_argument("--source-ref", default="")
    args = ap.parse_args()

    if not os.path.isdir(args.corpus):
        return f"corpus dir not found: {args.corpus}"

    buckets: dict[str, dict[str, list]] = {"core": defaultdict(list), "divergent": defaultdict(list)}
    seen: dict[str, set] = {"core": set(), "divergent": set()}
    counts = defaultdict(int)
    dropped: dict[str, int] = defaultdict(int)

    for method in sorted(os.listdir(args.corpus)):
        mdir = os.path.join(args.corpus, method)
        if not os.path.isdir(mdir):
            continue
        for fname in sorted(os.listdir(mdir)):
            if not (fname.startswith("test_") and fname.endswith(".json")):
                continue
            try:
                data = json.load(open(os.path.join(mdir, fname), encoding="utf-8"))
            except (json.JSONDecodeError, OSError):
                continue
            for i, entry in enumerate(data if isinstance(data, list) else [data]):
                if not (isinstance(entry, dict) and isinstance(entry.get("request"), dict)):
                    continue
                rpc = entry["request"].get("method", method)
                params = entry["request"].get("params", []) or []
                if rpc in DROP_HASH_REPLAY:
                    dropped[rpc] += 1
                    continue
                params = remap_block(rpc, params, args.target_block)
                bucket = "divergent" if is_divergent(rpc) else "core"
                key = json.dumps([rpc, params], sort_keys=True)
                if key in seen[bucket]:
                    continue
                seen[bucket].add(key)
                stem = fname[:-5]
                cid = f"{stem}_{i}" if i else stem
                buckets[bucket][rpc].append({"id": cid, "params": params})
                counts[bucket] += 1

    os.makedirs(args.out, exist_ok=True)
    prov = f" (source rpc-tests {args.source_ref})" if args.source_ref else ""
    desc = {
        "core": "standard eth_/debug_ methods; compares cleanly across clients",
        "divergent": "non-standard namespaces / node-local / oracle methods; informational (noisy cross-client)",
    }
    for bucket in ("core", "divergent"):
        methods = buckets[bucket]
        cfg = {
            "name": f"erigon-{args.network}" + ("" if bucket == "core" else f"-{bucket}"),
            "description": (
                f"Ported from erigontech/rpc-tests{prov}. {desc[bucket]}. "
                f"Every test retargeted to block '{args.target_block}' — runnable on any synced node. "
                f"Differential; pair with erigon-{args.network}-rules.yaml."
            ),
            "calls": {m: methods[m] for m in sorted(methods)},
        }
        name = f"erigon-{args.network}.yaml" if bucket == "core" else f"erigon-{args.network}-{bucket}.yaml"
        with open(os.path.join(args.out, name), "w", encoding="utf-8", newline="\n") as f:
            yaml.safe_dump(cfg, f, sort_keys=False, default_flow_style=False, width=4096)

    rules = {"comparison": {"rules": [
        {"method": "eth_getBlockByNumber", "path": "totalDifficulty", "kind": "ignore"},
        {"method": "eth_getBlockByHash", "path": "totalDifficulty", "kind": "ignore"},
        {"kind": "error_code_only"},
    ]}}
    with open(os.path.join(args.out, f"erigon-{args.network}-rules.yaml"), "w", encoding="utf-8", newline="\n") as f:
        yaml.safe_dump(rules, f, sort_keys=False, default_flow_style=False)

    lines = [
        f"# Erigon rpc-tests -> json-bench compare ({args.network})", "",
        f"Source: erigontech/rpc-tests{prov}", "",
        f"Every test is retargeted to block `{args.target_block}`, so the whole suite "
        f"runs on any synced node (no archive dependency).", "",
        "| config | calls | notes |", "|---|---:|---|",
        f"| `erigon-{args.network}.yaml` | {counts['core']} | standard methods, compares cleanly across clients |",
        f"| `erigon-{args.network}-divergent.yaml` | {counts['divergent']} | informational (noisy cross-client) |",
        "", f"Total runnable: {counts['core'] + counts['divergent']}", "",
    ]
    if dropped:
        lines += [
            "## Dropped (not adaptable to an arbitrary block)", "",
            "Replay/state addressed only by a fixed hash — no block argument to retarget, "
            "and replaying the referenced point needs archive state:", "",
        ]
        for m in sorted(dropped):
            lines.append(f"- `{m}` ({dropped[m]})")
        lines.append("")
    lines += [
        "## Run", "", "```bash",
        f"go run ./runner compare --config config/compare/erigon/erigon-{args.network}.yaml \\",
        "  --clients config/clients/clients.yaml --client-refs nethermind,geth,reth \\",
        f"  --rules config/compare/erigon/erigon-{args.network}-rules.yaml",
        "```", "",
        f"For a moving-head node set `--target-block` to a fixed recent number when regenerating "
        f"(default `latest` is deterministic across nodes parked at the same head).",
    ]
    with open(os.path.join(args.out, f"MANIFEST-{args.network}.md"), "w", encoding="utf-8", newline="\n") as f:
        f.write("\n".join(lines) + "\n")

    print(f"Wrote {counts['core'] + counts['divergent']} runnable requests (target block '{args.target_block}') to {args.out}")
    print(f"  core       {counts['core']}")
    print(f"  divergent  {counts['divergent']}")
    if dropped:
        print(f"  dropped    {sum(dropped.values())} (hash-addressed replay: {', '.join(sorted(dropped))})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
