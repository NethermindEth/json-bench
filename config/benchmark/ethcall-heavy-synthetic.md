# Heavy synthetic eth_call corpus

The supplied `eth_call_payloads_heavy_synthetic.jsonl` is included byte-for-byte as
`rpc-calls/eth-call-corpus-heavy-synthetic-100.jsonl`. Keep its parameters unchanged; the
filename identifies a synthetic heavy workload,
not a byte-for-byte reproduction of the production traffic in the customer report.

The supplied file contains 100 unique `eth_call` requests, each with three
parameters: a call, `latest`, and code overrides for 17-30 accounts. Request sizes
are 369,032-855,919 bytes. Every request specifies 2,000,000,000 gas.
SHA-256: `c97d229b049c98fa06aed9ba2c3a471ed2005813938825fc9ac9fcc331c0048a`.

Use an isolated remote mainnet snapshot, record the snapshot block and node commit,
and disable synchronization so `latest` stays fixed. Set Nethermind
`--JsonRpc.GasCap=2000000000`; the RPC benchmark harness otherwise caps calls at
1,000,000,000. Ensure the body-size limit accepts the largest request.

Start with the default 10 rps, 32 VUs, 120 seconds and seed 7. Capture every
request once with the RPC harness's `deep_check` option, and inspect all 100 full
responses for transport failures and JSON-RPC errors before treating timings as
successful execution. A successful HTTP status alone is insufficient.

Collect an unprofiled baseline and a separate dotTrace sampling run on the same
commit, snapshot and settings. Increase offered load only after the initial run
establishes valid responses. Record achieved rps, dropped iterations, p50/p95/p99,
RPC errors and response sizes. Capacity requires at least 99% valid responses,
at least 90% achieved/offered rps and p95 below one second.

The current generator materializes a payload for every scheduled request. This
corpus is about 47.5 MB for 100 requests, so a high-rate long run can generate
many GB of CSV and load-generator data. Check generator memory and CPU before
attributing saturation to the node. Profiling adds overhead; use unprofiled runs
for performance comparisons.
