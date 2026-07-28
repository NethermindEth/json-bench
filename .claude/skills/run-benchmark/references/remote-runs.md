# Running on the target host over SSH

Running the benchmark *on* the node it measures removes network latency from the results. The general procedure — adapt paths, user, and ports to the actual host:

## 1. Preflight the node

Before burning a long run, over SSH check that the node is fit to measure:

- RPC responds locally (`curl` an `eth_blockNumber` against the node's local RPC).
- The node is synced and its head is fresh (compare `eth_getBlockByNumber("latest")` timestamp to now).
- Enough free disk for run artifacts (a few GB).
- `k6` is available on the host, or plan to ship a static binary.

## 2. Build and deploy

The runner cross-compiles cleanly:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/benchmark-linux ./runner
```

`rsync` to the host everything a run needs: the binary, the benchmark config, the clients registry, the pre-generated `requests.csv` referenced by the config's `calls_file` (generate it once locally, before any run — every host must replay the identical request set or results aren't comparable), and any `rpc-calls/` files the config references (paths in the config are relative — preserve the layout).

The clients registry used on the host must point at the node's local RPC (`http://127.0.0.1:8545` or wherever the node listens locally), not the public URL.

## 3. Run remotely

```bash
ssh <user>@<host> 'cd <remote-dir> && ./benchmark --output results/ benchmark \
  --config <config>.yaml --clients <clients>.yaml --html-report'
```

**Prometheus export from a remote host**: the node usually can't reach the local Prometheus, so open an SSH reverse tunnel and point the runner at it:

```bash
ssh -R 127.0.0.1:9091:127.0.0.1:9090 <user>@<host> \
  'cd <remote-dir> && ./benchmark --output results/ benchmark \
     --config <config>.yaml --clients <clients>.yaml \
     --prometheus http://127.0.0.1:9091'
```

(9090 = local Prometheus from this repo's `docker compose`; 9091 = loopback port on the remote end.)

## 4. Fetch and clean up

`rsync` the remote results directory back into this repo's `outputs/<benchmark-name>/<client>/`, then remove the remote run directory unless the user wants it kept.

## Multiple targets

Repeat 1–4 per host, one at a time, with the *same* benchmark config and the *same* pre-generated `calls_file` so results are comparable — only the clients registry (and host) changes. Keep each target's results in its own subdirectory; the cross-target comparison happens locally in the analysis step.

Before scripting any of this by hand, check `scripts/` — the repo may already contain an orchestration helper for this flow.
