# Security policy

## Reporting a vulnerability

If you discover a security vulnerability in `json-bench`, please **do not**
open a public GitHub issue. Instead, report it privately via one of:

- Email: `security@nethermind.io`
- GitHub's private vulnerability reporting form for this repository
  (Security tab → Report a vulnerability)

Please include:

- A description of the vulnerability and its impact.
- A reproduction (PoC, configs, or commands) that demonstrates the issue.
- The commit SHA or release tag you reproduced it against.
- Your assessment of severity.

We aim to acknowledge the report within **3 business days** and to ship a
fix or mitigation within **30 days** for high/critical-severity issues.

## Scope

This repository ships a benchmarking and comparison tool for JSON-RPC
endpoints. In-scope components include:

- The Go runner (`runner/`) and its subcommands (`benchmark`, `compare`,
  `compare-openrpc`, `api`, `historic`).
- The React dashboard (`dashboard/`) and its API surface.
- Configuration parsing (`runner/config/`) and the YAML-driven payload
  loaders.
- The Grafana / Prometheus / Postgres deployment in `docker-compose.yml`.

Out of scope:

- Vulnerabilities in upstream dependencies (report those to their owners;
  we track them via Dependabot).
- Issues that require an authenticated operator on the host running the
  tool, since the tool is intended to be invoked by trusted operators
  rather than exposed to anonymous users.

## Hardening already in place

- API input validation and log-injection sanitization
  (`runner/api/inputvalidation.go`).
- HTML report endpoint XSS branch disabled (`runner/api/handlers.go`).
- Path-traversal guard on YAML-supplied file paths
  (`runner/config/safe_path.go`).
- SSRF guard on `compare-openrpc --spec` URLs
  (`runner/comparator/openrpc_loader.go`, override with
  `JSON_BENCH_ALLOW_PRIVATE_SPEC_URL=1`).
- CodeQL (Go + JavaScript) and Trivy scans run on every push and PR.

## Supported versions

The `main` branch receives security fixes. Older tagged releases are
fixed only for critical issues, on a case-by-case basis.
