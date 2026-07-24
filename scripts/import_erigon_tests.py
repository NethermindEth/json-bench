#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Port the erigontech/rpc-tests corpus into json-bench `compare` configs.

json-bench's `compare` is *differential* (it diffs each client's response
against the others, not against a golden file), so this importer keeps only the
requests from the Erigon corpus and drops the Erigon-captured expected
responses. The value it adds over hand-written compare configs is coverage
(100+ methods, thousands of real mainnet requests) and — the point of this
tool — **block-awareness**: every test is bucketed by what the target node must
hold to answer it, so a run against a full/head node uses only the safe subset
while an archive node can run everything.

Buckets (one config per bucket):
  stateless            no block/state dependence (eth_chainId, eth_blockNumber …)
  head                 block tag is latest/pending/safe/finalized
  historical-immutable numeric/hash block, reads block/tx/receipt/log/header/raw
                       — a full node still has this (ancients / static_files)
  historical-state     numeric/hash block, reads state (balance/code/call/trace …)
                       — needs an ARCHIVE node; a full/head node prunes it
  divergent            namespaces/methods that do not compare cleanly across
                       clients (erigon_/ots_/parity_/engine_/admin_/txpool_/
                       trace_, node-specific, gas-price oracle) — kept separate

Also emits a `--rules` file (ignore totalDifficulty, error_code_only) to damp
benign cross-client differences, and a MANIFEST with per-bucket / per-method
counts and which node type runs which bucket.

Usage:
  python scripts/import_erigon_tests.py \
      --corpus /path/to/rpc-tests/integration/mainnet \
      --network mainnet --out config/compare/erigon
"""
from __future__ import annotations

import argparse
import json
import os
import sys
from collections import defaultdict

try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required: pip install pyyaml")

HEAD_TAGS = {"latest", "pending", "safe", "finalized"}
# 'earliest' is genesis state -> historical, not head.

# Namespaces / methods that do not compare cleanly across Geth/Reth/Nethermind:
# non-standard namespaces, auth-gated engine API, node-local state, and outputs
# that legitimately differ per client (client version, gas-price oracle).
DIVERGENT_PREFIXES = ("erigon_", "ots_", "parity_", "engine_", "admin_", "txpool_", "trace_")
DIVERGENT_METHODS = {
    "web3_clientVersion", "eth_coinbase", "eth_mining", "eth_getWork",
    "eth_submitWork", "eth_submitHashrate", "eth_syncing", "eth_hashrate",
    "net_listening", "net_peerCount", "eth_getFilterChanges", "eth_getFilterLogs",
    "eth_sendRawTransaction", "eth_fillTransaction", "eth_gasPrice",
    "eth_maxPriorityFeePerGas", "unknown_method",
}

# Reads chain state — needs the state trie at the referenced block (archive for
# historical blocks).
STATE_METHODS = {
    "eth_call", "eth_callBundle", "eth_callMany", "eth_estimateGas",
    "eth_createAccessList", "eth_getBalance", "eth_getCode", "eth_getStorageAt",
    "eth_getStorageValues", "eth_getProof", "eth_getTransactionCount",
    "eth_simulateV1",
    "debug_traceCall", "debug_traceCallMany", "debug_traceBlockByNumber",
    "debug_traceBlockByHash", "debug_traceTransaction", "debug_accountAt",
    "debug_accountRange", "debug_storageRangeAt",
    "debug_getModifiedAccountsByNumber", "debug_getModifiedAccountsByHash",
}

# Reads immutable chain data (blocks/txs/receipts/logs/headers) — a full node
# retains this for any block regardless of state pruning.
IMMUTABLE_METHODS = {
    "eth_getBlockByNumber", "eth_getBlockByHash", "eth_getBlockReceipts",
    "eth_getBlockTransactionCountByNumber", "eth_getBlockTransactionCountByHash",
    "eth_getTransactionByHash", "eth_getTransactionByBlockNumberAndIndex",
    "eth_getTransactionByBlockHashAndIndex", "eth_getTransactionReceipt",
    "eth_getRawTransactionByHash", "eth_getRawTransactionByBlockNumberAndIndex",
    "eth_getRawTransactionByBlockHashAndIndex", "eth_getUncleByBlockNumberAndIndex",
    "eth_getUncleByBlockHashAndIndex", "eth_getUncleCountByBlockNumber",
    "eth_getUncleCountByBlockHash", "eth_getLogs", "eth_feeHistory",
    "debug_getRawBlock", "debug_getRawHeader", "debug_getRawReceipts",
    "debug_getRawTransaction",
}

STATELESS_METHODS = {
    "eth_chainId", "eth_blockNumber", "eth_baseFee", "eth_blobBaseFee",
    "eth_protocolVersion", "eth_capabilities", "net_version", "web3_sha3",
    "eth_accounts",
}

# Positional index of the block argument, for head-vs-historical detection and
# optional head-remapping. Mirrors json-bench's comparator blockArgIndex and
# extends it. eth_getLogs is handled specially (fromBlock/toBlock in the filter).
BLOCK_ARG_INDEX = {
    "eth_call": 1, "eth_estimateGas": 1, "eth_createAccessList": 1,
    "eth_getBalance": 1, "eth_getCode": 1, "eth_getTransactionCount": 1,
    "eth_getStorageAt": 2, "eth_getProof": 2, "eth_getBlockByNumber": 0,
    "eth_getBlockReceipts": 0, "eth_getBlockTransactionCountByNumber": 0,
    "eth_getTransactionByBlockNumberAndIndex": 0,
    "eth_getRawTransactionByBlockNumberAndIndex": 0,
    "eth_getUncleByBlockNumberAndIndex": 0, "eth_getUncleCountByBlockNumber": 0,
    "eth_feeHistory": 1, "eth_simulateV1": 1,
    "debug_traceBlockByNumber": 0, "debug_traceCall": 1,
    "debug_getModifiedAccountsByNumber": 0, "debug_storageRangeAt": 0,
}


def is_divergent(method: str) -> bool:
    return method in DIVERGENT_METHODS or method.startswith(DIVERGENT_PREFIXES)


def block_ref(method: str, params: list):
    """Return ('tag', v) | ('number', v) | ('hash',) | None for the block this call targets."""
    if not isinstance(params, list):
        return None
    if method == "eth_getLogs" and params and isinstance(params[0], dict):
        for key in ("fromBlock", "toBlock"):
            v = params[0].get(key)
            if isinstance(v, str) and v in HEAD_TAGS:
                return ("tag", v)
        for key in ("fromBlock", "toBlock"):
            v = params[0].get(key)
            if isinstance(v, str) and v.startswith("0x"):
                return ("number", v)
        if params[0].get("blockHash"):
            return ("hash",)
        return None
    idx = BLOCK_ARG_INDEX.get(method)
    if idx is not None and idx < len(params):
        v = params[idx]
        if isinstance(v, str):
            if v in HEAD_TAGS or v == "earliest":
                return ("tag", v)
            if v.startswith("0x"):
                return ("number", v)
    # Hash-addressed methods (by block hash or tx hash) target a historical point
    # the full node still holds.
    if "ByHash" in method or method in (
        "eth_getTransactionByHash", "eth_getTransactionReceipt",
        "eth_getRawTransactionByHash", "debug_traceTransaction",
    ):
        return ("hash",)
    return None


def classify(method: str, params: list) -> str:
    if is_divergent(method):
        return "divergent"
    ref = block_ref(method, params)
    if method in STATELESS_METHODS or (ref is None and method not in STATE_METHODS and method not in IMMUTABLE_METHODS):
        return "stateless"
    if ref is not None and ref[0] == "tag" and ref[1] in HEAD_TAGS:
        return "head"
    # historical (numeric / hash / implicit-by-hash / earliest)
    if method in STATE_METHODS:
        return "historical-state"
    return "historical-immutable"


def load_requests(path: str):
    """Yield request dicts from a test file (an array of {request,...} or a single obj)."""
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (json.JSONDecodeError, OSError):
        return
    entries = data if isinstance(data, list) else [data]
    for entry in entries:
        if isinstance(entry, dict) and isinstance(entry.get("request"), dict):
            yield entry["request"]


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--corpus", required=True, help="path to rpc-tests integration/<network> dir")
    ap.add_argument("--network", default="mainnet")
    ap.add_argument("--out", required=True, help="output directory for generated compare configs")
    ap.add_argument("--source-ref", default="", help="rpc-tests commit the corpus was taken from (for provenance)")
    args = ap.parse_args()

    if not os.path.isdir(args.corpus):
        return f"corpus dir not found: {args.corpus}"

    # bucket -> method -> list of {id, params}
    buckets: dict[str, dict[str, list]] = defaultdict(lambda: defaultdict(list))
    seen: dict[str, set] = defaultdict(set)  # dedupe (method,params) per bucket
    per_method_bucket: dict[str, str] = {}
    counts = defaultdict(int)

    for method in sorted(os.listdir(args.corpus)):
        mdir = os.path.join(args.corpus, method)
        if not os.path.isdir(mdir):
            continue
        for fname in sorted(os.listdir(mdir)):
            if not (fname.startswith("test_") and fname.endswith(".json")):
                continue
            fpath = os.path.join(mdir, fname)
            for i, req in enumerate(load_requests(fpath)):
                rpc_method = req.get("method", method)
                params = req.get("params", []) or []
                bucket = classify(rpc_method, params)
                key = json.dumps([rpc_method, params], sort_keys=True)
                if key in seen[bucket]:
                    continue
                seen[bucket].add(key)
                stem = fname[:-5]  # drop .json
                cid = f"{stem}_{i}" if i else stem
                buckets[bucket][rpc_method].append({"id": cid, "params": params})
                counts[bucket] += 1
                per_method_bucket.setdefault(rpc_method, bucket)

    os.makedirs(args.out, exist_ok=True)
    prov = f" (source rpc-tests {args.source_ref})" if args.source_ref else ""

    node_hint = {
        "stateless": "any node",
        "head": "any synced node (full or archive)",
        "historical-immutable": "full node (has block/tx/receipt/log history) or archive",
        "historical-state": "ARCHIVE node only (full/head nodes prune historical state)",
        "divergent": "informational — will not compare cleanly across clients; curate before use",
    }

    for bucket, methods in sorted(buckets.items()):
        cfg = {
            "name": f"erigon-{args.network}-{bucket}",
            "description": (
                f"Ported from erigontech/rpc-tests{prov}; bucket '{bucket}'. "
                f"Runs on: {node_hint[bucket]}. Differential (client-vs-client) — "
                f"pair with erigon-{args.network}-rules.yaml."
            ),
            "calls": {m: methods[m] for m in sorted(methods)},
        }
        out_path = os.path.join(args.out, f"erigon-{args.network}-{bucket}.yaml")
        with open(out_path, "w", encoding="utf-8", newline="\n") as f:
            yaml.safe_dump(cfg, f, sort_keys=False, default_flow_style=False, width=4096)

    # Cross-client noise-damping rules (differential mode).
    rules = {
        "comparison": {
            "rules": [
                # Nethermind still returns totalDifficulty in block responses; Geth/Reth dropped it post-merge.
                {"method": "eth_getBlockByNumber", "path": "totalDifficulty", "kind": "ignore"},
                {"method": "eth_getBlockByHash", "path": "totalDifficulty", "kind": "ignore"},
                # Error wording differs across clients; compare the JSON-RPC error code only.
                {"kind": "error_code_only"},
            ]
        }
    }
    with open(os.path.join(args.out, f"erigon-{args.network}-rules.yaml"), "w", encoding="utf-8", newline="\n") as f:
        yaml.safe_dump(rules, f, sort_keys=False, default_flow_style=False)

    lines = [
        f"# Erigon rpc-tests -> json-bench compare ({args.network})",
        "",
        f"Source: erigontech/rpc-tests{prov}",
        "",
        "| bucket | calls | runs on |",
        "|---|---:|---|",
    ]
    for bucket in ["stateless", "head", "historical-immutable", "historical-state", "divergent"]:
        if bucket in counts:
            lines.append(f"| `erigon-{args.network}-{bucket}.yaml` | {counts[bucket]} | {node_hint[bucket]} |")
    lines += [
        "", f"Total requests: {sum(counts.values())}", "",
        "Run against the 25490000 backups (full/head) with the safe buckets:",
        "",
        "```bash",
        f"runner compare --config config/compare/erigon/erigon-{args.network}-<bucket>.yaml \\",
        "  --clients config/clients/clients.yaml --client-refs nethermind,geth,reth \\",
        f"  --rules config/compare/erigon/erigon-{args.network}-rules.yaml",
        "```",
        "",
        "`stateless` + `head` + `historical-immutable` are safe on full/head nodes; "
        "`historical-state` needs an archive node; `divergent` is informational.",
    ]
    with open(os.path.join(args.out, f"MANIFEST-{args.network}.md"), "w", encoding="utf-8", newline="\n") as f:
        f.write("\n".join(lines) + "\n")

    print(f"Wrote {sum(counts.values())} requests to {args.out}")
    for bucket in ["stateless", "head", "historical-immutable", "historical-state", "divergent"]:
        if bucket in counts:
            print(f"  {bucket:22} {counts[bucket]:5}  ({node_hint[bucket]})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
