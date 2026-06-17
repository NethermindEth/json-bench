# Ethereum JSON-RPC Benchmarking Suite

A comprehensive benchmarking framework for Ethereum JSON-RPC clients, designed to evaluate and compare the performance and behavior of different client implementations like Geth and Nethermind.

## Overview

This project runs predefined RPC tests derived from the official Ethereum Execution APIs spec, generates standardized performance metrics, checks for response consistency, and provides both historic tracking and real-time analysis through a modern web dashboard.

## Features

- **Performance Benchmarking**: Benchmark and compare Ethereum clients under realistic load using k6
- **Historic Tracking**: Store and analyze performance trends over time with PostgreSQL + Grafana integration
- **Real-time Dashboard**: Modern React UI for viewing results, trends, and comparisons
- **Response Validation**: Validate RPC response compatibility with [ethereum/execution-apis](https://github.com/ethereum/execution-apis)
- **Schema Validation**: Check responses against official Ethereum JSON-RPC specifications
- **Baseline Management**: Set performance baselines and detect regressions automatically
- **Multiple Output Formats**: Generate HTML reports, CSV exports, and JSON data
- **WebSocket Updates**: Real-time updates for live monitoring
- **Grafana Integration**: Pre-built dashboards for time-series analysis and alerting

## Project Structure

```dir-tree
json-bench/
│
├── config/                  # YAML test configurations
│   ├── clients.yaml         # RPC clients configurations
|   ├── mixed.yaml           # Mixed workload benchmark
│   ├── read-heavy.yaml      # Read-heavy workload benchmark
│   ├── storage-example.yaml # Historic storage configuration
│   └── param_variations.yaml
│
├── runner/                   # Go benchmark runner with historic tracking
│   ├── main.go              # Thin entry point - delegates to cmd.Execute()
│   ├── cmd/                 # Cobra subcommands (benchmark, api, historic,
│   │                        #                    compare, compare-openrpc)
│   ├── api/                 # HTTP API server and WebSocket support
│   ├── storage/             # PostgreSQL integration
│   ├── analysis/            # Trend analysis and regression detection
│   └── generator/           # K6 script generation and HTML reports
│
├── dashboard/               # React dashboard for historic analysis
│   ├── src/
│   │   ├── pages/          # Dashboard pages (trends, comparisons, baselines)
│   │   ├── components/     # Reusable UI components
│   │   └── api/            # API client for backend integration
│   └── dist/               # Built dashboard files
│
├── metrics/                 # Postgres + Prometheus + Grafana configuration
│   ├── grafana-provisioning/
│   └── dashboards/         # Pre-built Grafana dashboards
│
└── cmd/                     # Legacy debug helpers (not built by default)
```

## Getting Started

### Prerequisites

- **Docker and Docker Compose** (for client nodes and infrastructure)
- **Go 1.20+** (for the benchmark runner)
- **Node.js 18+** (for the React dashboard)
- **k6** (for load testing - install from <https://k6.io/>)
- **PostgreSQL** (for historic tracking - included in Docker Compose)

### Quick Start

1. **Clone the repository:**

    ```bash
    git clone <repository-url>
    cd json-bench
    ```

2. **Start the infrastructure (PostgreSQL, Prometheus and Grafana):**

    ```bash
    # Start Grafana, PostgreSQL and Prometheus
    docker compose up -d grafana postgres prometheus

    # You can also start the runner API and the dashboard
    docker compose up -d runner dashboard

    # Or just start everything
    docker compose up -d
    ```

3. **Set up your client nodes:**

    ```bash
    # Configure your own endpoints in the config files
    nano config/clients.yaml
    ```

4. **Run a benchmark:**

    ```bash
    # Basic benchmark (no historic tracking)
    go run ./runner benchmark --config ./config/mixed.yaml --clients ./config/clients.yaml
    # With historic tracking (requires PostgreSQL)
    go run ./runner benchmark --config ./config/mixed.yaml --clients ./config/clients.yaml --historic --storage-config ./config/storage-example.yaml

    # View results
    open outputs/report.html
    ```

    **NOTE:** `storage-example.yaml` works out of the box with the docker containers deployed in the compose file.

5. **Access the services:**

   - **PostgreSQL**: localhost:5432 (postgres/postgres)
   - **Prometheus**: <http://localhost:9090>
   - **Grafana**: <http://localhost:3000> (admin/admin)
   - **RunnerAPI**: <http://localhost:8082> (if started)
   - **Dashboard**: <http://localhost:8080> (if started)

## Usage

The `runner` binary exposes its functionality through subcommands. Running
`runner` with no subcommand prints usage and exits with status 2.

```text
runner benchmark        Run a benchmark (k6 -> Prometheus -> reports)
runner compare          One-shot cross-client JSON-RPC response comparison
runner compare-openrpc  Cross-client comparison driven by an OpenRPC specification
runner api              Start the HTTP API server
runner historic         Generate a historic-analysis report from PostgreSQL
```

Global flags accepted by every subcommand:

- `--output` (default `outputs/`) - where artefacts are written.
- `--log-level` - `debug`, `info`, `warn`, or `error`. Falls back to the
  `LOG_LEVEL` environment variable, then `info`.

### Basic Benchmarking

```bash
# Run a mixed workload benchmark
go run ./runner benchmark --config ./config/mixed.yaml --clients ./config/clients.yaml

# Run a read-heavy benchmark
go run ./runner benchmark --config ./config/read-heavy.yaml --clients ./config/clients.yaml

# Run with a custom output directory and a non-default Prometheus endpoint
go run ./runner --output ./custom-results benchmark \
  --config ./config/mixed.yaml \
  --clients ./config/clients.yaml \
  --prometheus-rw http://prometheus:9090/api/v1/write

# Generate an HTML report alongside the JSON and CSV exports (off by default)
go run ./runner benchmark \
  --config ./config/mixed.yaml \
  --clients ./config/clients.yaml \
  --html-report
```

`benchmark` always writes `outputs/results.json` and `outputs/results.csv`.
The HTML report at `outputs/report.html` is opt-in via `--html-report`.
`compare` and `compare-openrpc` always produce their HTML report.

### Historic Tracking & Analysis

Enable historic tracking to store results in PostgreSQL and analyze trends over time:

```bash
# Persist this benchmark run to PostgreSQL
go run ./runner benchmark \
  --config ./config/mixed.yaml \
  --clients ./config/clients.yaml \
  --historic \
  --storage-config ./config/storage-example.yaml

# Generate a historic-analysis report (no new benchmark)
go run ./runner historic \
  --config ./config/mixed.yaml \
  --storage-config ./config/storage-example.yaml
```

### API Server for Real-time Access

Start the HTTP API server for real-time data access and WebSocket updates:

```bash
# Start the API server with historic storage
go run ./runner api \
  --storage-config ./config/storage-example.yaml \
  --api-addr :8081

# API will be available at http://localhost:8081
```

### One-shot Cross-Client Comparison

`runner compare` runs a one-shot JSON-RPC response comparison across a
set of clients defined in `clients.yaml`. It is flags-only (no YAML
schema for compare) and filesystem-only: results are not written to the
historic-runs database.

```bash
go run ./runner compare \
  --clients ./config/clients.yaml \
  --client-refs geth,nethermind \
  --methods eth_blockNumber,eth_chainId,eth_gasPrice \
  --output ./comparison-results
```

Both artefacts are always produced:

- `<output>/comparison-results.json`
- `<output>/comparison-report.html`

`compare` uses the comparator's default empty-params behaviour for each
method. Methods that need parameter variations should use
`compare-openrpc`.

### OpenRPC-Driven Comparison

`runner compare-openrpc` loads the method set from an OpenRPC
specification and runs the same cross-client comparison as `compare`,
optionally expanding individual methods into per-parameter variations
defined in a YAML file. Like `compare`, it is filesystem-only and does
not write to the historic-runs database.

```bash
go run ./runner compare-openrpc \
  --spec ./openrpc.json \
  --variations ./config/param_variations.yaml \
  --clients ./config/clients.yaml \
  --client-refs geth,nethermind \
  --filter eth_blockNumber,eth_getBlockByNumber \
  --output ./openrpc-results
```

Useful debug aids:

- `--filter <m1,m2,...>` - restrict the comparison to a whitelist of
  method names. Matches the base method name, so `--filter
  eth_getBlockByNumber` keeps every `eth_getBlockByNumber_variantN`
  generated from the variations file.
- `--curl` - log a curl-equivalent command for every JSON-RPC request
  the comparator makes, useful for reproducing a single call against a
  client by hand.

Both `comparison-results.json` and `comparison-report.html` are written
to `--output`.

### Migrating from the pre-refactor CLI

The flag-modal entry point (`runner -api`, `runner -historic-mode`,
single-dash long flags) has been replaced with explicit Cobra
subcommands. The old invocations will fail at flag-parse time with a
clear error. The mapping is:

| Old | New |
|---|---|
| `runner -config ... -prometheus-rw ...` | `runner benchmark --config ... --prometheus-rw ...` |
| `runner -api -storage-config ... -api-addr ...` | `runner api --storage-config ... --api-addr ...` |
| `runner -historic-mode -storage-config ... -config ...` | `runner historic --storage-config ... --config ...` |
| `runner -historic -storage-config ...` (with `-config ...`) | `runner benchmark --historic --storage-config ...` |
| `-config`, `-clients`, `-output`, `-prometheus-rw`, ... (single dash) | `--config`, `--clients`, `--output`, `--prometheus-rw`, ... (double dash) |

Additional behaviour changes worth noting:

- The benchmark `report.html` is opt-in via `--html-report`. JSON and CSV
  exports remain on by default.
- The legacy `endpoints + frequency` YAML schema is no longer accepted.
  Configs using it must be migrated by hand to the `calls:` schema (see
  `config/mixed.yaml` for the canonical shape). No migrator is provided.
- The `jsonrpc-benchmark.json` Grafana dashboard has been removed. Its
  Prometheus queries (`method_calls_*`, `rpc_errors_*`, `rpc_calls_*`)
  reference custom counters from the pre-refactor k6 template that are
  no longer emitted. The other three dashboards
  (`k6-dashboard.json`, `jsonrpc-benchmark-enhanced.json`,
  `baseline-comparison.json`) remain in place.

**Available API endpoints:**

- `GET /api/runs` - List historic benchmark runs
- `GET /api/runs/:id` - Get specific run details
- `GET /api/trends` - Get performance trend data
- `GET /api/baselines` - List performance baselines
- `GET /api/compare?run1=:id1&run2=:id2` - Compare two runs
- `POST /api/runs/:id/baseline` - Set run as baseline
- `WS /api/ws` - WebSocket for real-time updates

### React Dashboard

The modern React dashboard provides an intuitive interface for analyzing benchmark results and trends:

```bash
# Install dashboard dependencies
cd dashboard
npm install

# Start development server
npm run dev
# Dashboard available at http://localhost:3000

# Build for production
npm run build
npm run preview
```

**Dashboard Features:**

- **Dashboard Page**: Overview of recent runs and performance trends
- **Run Details**: Detailed analysis of individual benchmark runs
- **Comparison View**: Side-by-side comparison of multiple runs
- **Baseline Management**: Set and manage performance baselines
- **Trend Analysis**: Interactive charts showing performance over time
- **Regression Alerts**: Automatic detection of performance regressions

### Grafana Integration

For advanced time-series analysis and alerting, you can use Grafana:

1. **Setup Grafana locally or use Docker:**

   ```bash
   docker compose up -d grafana
   ```

2. **Open Grafana**: <http://localhost:3000> (admin/admin)

3. **Provisioned data sources**:
   - **Prometheus**:
     - URL: `http://localhost:9090` (or `http://prometheus:9090` if using Docker network)
     - Access: Server (default)
   - **PostgreSQL** (for historic data):
     - Host: `localhost:5432` (or `postgres:5432` if using Docker network)
     - Database: `jsonrpc_bench`
     - User: `postgres`
     - Password: `postgres`
     - SSL Mode: `disable`

4. **Provisioned dashboards** from `metrics/dashboards/`

   - K6 Performance
   - Client performance comparison
   - Method-specific latency trends
   - Error rate monitoring
   - System resource usage
   - Historic trend analysis

5. **Set up alerting** for performance regressions and system issues

### Storage Configuration

Configure PostgreSQL storage for historic tracking. Choose the appropriate configuration file based on your setup:

**Local Development (outside Docker):**

```bash
# Use storage-example.yaml for local PostgreSQL connection
go run ./runner benchmark --config ./config/mixed.yaml --historic --storage-config ./config/storage-example.yaml
```

**Docker Environment:**

```bash
# Use storage-docker.yaml when running inside Docker containers
# (This config uses 'postgres' hostname which only exists in Docker network)
docker run ... api --storage-config ./config/storage-docker.yaml
```

**Configuration Examples:**

```yaml
# config/storage-example.yaml (for local development)
historic_path: "./historic"
enable_historic: true

postgresql:
  host: "localhost"          # Use localhost when running outside Docker
  port: 5432
  database: "jsonrpc_bench"
  username: "postgres"
  password: "postgres"
  ssl_mode: "disable"
  
  grafana:
    metrics_table: "benchmark_metrics"
    runs_table: "benchmark_runs"
    retention_policy:
      metrics_retention: "30d"
      aggregated_retention: "90d"
```

```yaml
# config/storage-docker.yaml (for Docker environment)
historic_path: "/app/historic"
enable_historic: true

postgresql:
  host: "postgres"           # Use service name when running in Docker
  port: 5432
  database: "jsonrpc_bench"
  username: "postgres"
  password: "postgres"
  ssl_mode: "disable"
```

## Advanced Features

### Baseline Management

Set performance baselines to detect regressions:

```bash
# Set a run as baseline via API
curl -X POST http://localhost:8080/api/runs/20250103-120000-abc123/baseline \
  -H "Content-Type: application/json" \
  -d '{"name": "Production Baseline", "description": "Post-optimization baseline"}'

# Compare current run against baseline
curl "http://localhost:8080/api/compare?run1=baseline&run2=20250103-130000-def456"
```

### Custom Test Configurations

Create custom benchmark configurations:

```yaml
# Custom benchmark configuration
test_name: "Custom Load Test"
description: "High-load test for production validation"

clients:
  - name: "geth"
    url: "http://localhost:8545"
  - name: "nethermind"
    url: "http://localhost:8546"

rps: 500
duration: "5m"

calls:
  - name: "my_method_1"
    method: "eth_blockNumber"
    weight: 20
  - name: "my_method_2"
    method: "eth_getBalance"
    params: ["0x742d35Cc641C0532a7D4567bb19f68cE3FdD72cD", "latest"]
    weight: 40
```

### Parameter Variations

Test methods with different parameter sets:

```yaml
# config/param_variations.yaml
eth_call:
  - [{"to": "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"}, "latest"]
  - [{"to": "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"}, "pending"]

eth_getBalance:
  - ["0x742d35Cc641C0532a7D4567bb19f68cE3FdD72cD", "latest"]
  - ["0x742d35Cc641C0532a7D4567bb19f68cE3FdD72cD", "pending"]
```

### Docker Deployment

Deploy the entire stack using Docker:

```bash
# Full stack deployment
docker-compose up -d
```

## Environment Variables

Configure the application using environment variables:

```bash
# Create a copy of the .env.example file
cp .env.example .env

# Edit environment configuration
nano .env
```

## Monitoring and Alerting

### Grafana Alerts

Set up alerts for performance regressions:

1. **Configure notification channels** (Slack, Discord, email)
2. **Set alert rules** for:
   - Latency increases > 20%
   - Error rate > 1%
   - Throughput decreases > 15%
3. **Enable alert evaluation** in Grafana settings

### Webhook Integration

Configure webhooks for CI/CD integration:

```bash
# Environment variables for webhook notifications
export DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/..."
export SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..."
export GITHUB_WEBHOOK_URL="https://api.github.com/repos/owner/repo/dispatches"
```

## Output Formats

The benchmark runner generates multiple output formats:

- **HTML Reports**: Interactive reports with charts and metrics
- **JSON Data**: Machine-readable benchmark results
- **CSV Exports**: For spreadsheet analysis
- **Grafana Dashboards**: Time-series visualizations
- **Historic Analysis**: Trend reports and regression detection

## Contributing

1. **Follow Go conventions** for backend code
2. **Use TypeScript** for frontend development
3. **Add tests** for new features
4. **Update documentation** for API changes
5. **Ensure backward compatibility** for configuration files

## License

This project is licensed under the MIT License - see the LICENSE file for details.
