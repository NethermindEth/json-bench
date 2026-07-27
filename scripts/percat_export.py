#!/usr/bin/env python3
# Per-category top-N export from an EthCallChaos corpus (read-only copy).
# Selects the top-N slowest test_cases (by fitness) in each shape/family category
# and reconstructs the eth_call request for each, so a regression sweep replays a
# DIVERSE set (single-contract families + multicall fan-out buckets) instead of the
# fitness-monopolising all-multicall top-N.
import sqlite3, json, sys, hashlib

MULTICALL3 = "0xcA11bde05977b3631167028862bE2a173976CA11"
AGG3_SELECTOR = "82ad56cb"
FAMILY = {a.lower(): f for a, f in {
    "0xb27308f9F90D607463bb33eA1BeBb41C27CE5AB6": "Uniswap",
    "0x61fFE014bA17989E743c5F6cB21bF9697530B21e": "Uniswap",
    "0xbEbc44782C7dB0a1A60Cb6fe97d0b483032FF1C7": "Curve",
    "0xDC24316b9AE028F1497c275EB9192a3Ea0f67022": "Curve",
    "0xD51a44d3FaE010294C616388b506AcdA1bfAAE46": "Curve",
    "0x87870Bca3F3fD6335C3F4ce8392D69350B4fA4E2": "Aave",
    "0x3d9819210A31b4961b30EF54bE2aeD79B9c9Cd3B": "Compound",
    "0xBA12222222228d8Ba445958a75a0704d566BF2C8": "Balancer",
    "0x83F20F44975D03b1b09e64809B757c47f942BEeA": "Maker",
    "0xcA11bde05977b3631167028862bE2a173976CA11": "Infrastructure",
}.items()}


def h64(n):
    return format(n, "064x")


def strip0x(s):
    return s[2:] if s and s.lower().startswith("0x") else (s or "")


def encode_single_call(sub):
    addr = strip0x(sub["Target"]).rjust(64, "0")
    allow = h64(1) if sub.get("AllowFailure") else h64(0)
    cd = strip0x(sub.get("Calldata", ""))
    cdbytes = len(cd) // 2
    pad = (32 - cdbytes % 32) % 32
    return addr + allow + h64(96) + h64(cdbytes) + cd + ("0" * (pad * 2))


def encode_aggregate3(subcalls):
    # Mirrors MulticallEncoder.EncodeCalls: offset(32) + count + head(tail offsets) + tails.
    head = h64(32) + h64(len(subcalls))
    tail_off = len(subcalls) * 32
    heads, tails = [], []
    for sub in subcalls:
        heads.append(h64(tail_off))
        enc = encode_single_call(sub)
        tails.append(enc)
        tail_off += len(enc) // 2
    return "0x" + AGG3_SELECTOR + head + "".join(heads) + "".join(tails)


def category(row):
    if row["use_multicall"]:
        try:
            n = len(json.loads(row["multicall_targets_json"] or "[]"))
        except Exception:
            n = 0
        bucket = "small" if n <= 5 else "med" if n <= 15 else "large"
        return f"multicall/{bucket}"
    fam = FAMILY.get((row["to_address"] or "").lower())
    return f"single:{fam}" if fam else "single:other"


def request(row):
    if row["use_multicall"]:
        to = MULTICALL3
        data = encode_aggregate3(json.loads(row["multicall_targets_json"] or "[]"))
    else:
        to = row["to_address"]
        data = row["calldata"] or "0x"
    return {
        "to": to,
        "data": data,
        "gas": "0x%x" % (row["gas_limit"] or 30_000_000),
        "value": row["value"] or "0x0",
        "from": row["from_address"] or "0x0000000000000000000000000000000000000000",
    }


def main():
    DB = sys.argv[1]
    OUT_DIR = sys.argv[2]
    N_PER_CAT = int(sys.argv[3]) if len(sys.argv) > 3 else 3
    c = sqlite3.connect(DB)
    c.row_factory = sqlite3.Row
    rows = c.execute("SELECT to_address, calldata, gas_limit, value, from_address, "
                     "use_multicall, multicall_targets_json, fitness FROM test_cases").fetchall()
    by_cat = {}
    for r in rows:
        by_cat.setdefault(category(r), []).append(r)

    scenarios = []
    for cat in sorted(by_cat):
        top = sorted(by_cat[cat], key=lambda r: r["fitness"] or 0, reverse=True)[:N_PER_CAT]
        for i, r in enumerate(top, 1):
            req = request(r)
            scenarios.append({
                "name": f"{cat}#{i}",
                "category": cat,
                "rank": i,
                "fitness": round(r["fitness"] or 0, 1),
                "request": req,
            })

    import os
    os.makedirs(OUT_DIR, exist_ok=True)
    with open(os.path.join(OUT_DIR, "scenarios.jsonl"), "w") as f:
        for s in scenarios:
            f.write(json.dumps(s) + "\n")

    # json-bench benchmark config: one named call per scenario (per-name k6 metrics).
    with open(os.path.join(OUT_DIR, "ethcallchaos-percategory.yaml"), "w") as f:
        f.write('test_name: "EthCallChaos per-category regression set"\n')
        f.write("clients:\n  - nethermind\n")
        f.write('duration: "60s"\nrps: 30\nvus: 100\ncalls:\n')
        for s in scenarios:
            r = s["request"]
            f.write(f'  - name: "{s["name"]}"\n    method: "eth_call"\n    params:\n')
            f.write(f'      - to: "{r["to"]}"\n        data: "{r["data"]}"\n        gas: "{r["gas"]}"\n')
            f.write(f'        value: "{r["value"]}"\n        from: "{r["from"]}"\n')
            f.write('      - "latest"\n    weight: 1\n    thresholds: ["p(99)<600000"]\n')

    print(f"exported {len(scenarios)} scenarios across {len(by_cat)} categories")
    print(f"{'category':22s} {'count':>6s} {'top-fitness':>12s}")
    for cat in sorted(by_cat):
        top = sorted(by_cat[cat], key=lambda r: r["fitness"] or 0, reverse=True)[:N_PER_CAT]
        print(f"  {cat:20s} {len(by_cat[cat]):6d}   picked {len(top)}, top_fit={round(top[0]['fitness'] or 0,1) if top else '-'}")


if __name__ == "__main__":
    main()
