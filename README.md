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

```
json-bench/
│
├── clients/                  # Docker Compose setups for each client
│   ├── geth/
│   └── nethermind/
│
├── config/                   # YAML test configurations
│   ├── mixed.yaml           # Mixed workload benchmark
│   ├── read-heavy.yaml      # Read-heavy workload benchmark
│   ├── storage-example.yaml # Historic storage configuration
│   └── param_variations.yaml
│
├── runner/                   # Go benchmark runner with historic tracking
│   ├── main.go              # Main CLI application
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
├── metrics/                 # Prometheus + Grafana configuration
│   ├── grafana-provisioning/
│   ├── dashboards/         # Pre-built Grafana dashboards
│   └── docker-compose.grafana.yml
│
└── cmd/                     # Additional tools
    ├── compare/            # Response comparison utilities
    └── compare-openrpc/    # OpenRPC-based comparison tools
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

1. **Start the infrastructure (PostgreSQL, Prometheus and Grafana):**

    ```bash
    # Start PostgreSQL and Prometheus for data storage
    docker-compose -f metrics/docker-compose.grafana.yml up -d
    ```

1. **Set up your client nodes:**

    ```bash
    # Start Ethereum client containers
    docker-compose up -d geth nethermind

    # Or configure your own endpoints in the config files
    ```

1. **Run a benchmark:**

    ```bash
    # Basic benchmark (no historic tracking)
    go run ./runner/main.go -config ./config/mixed.yaml
    ```

1. **Access the services:**
    - **PostgreSQL**: localhost:5432 (postgres/postgres)
    - **Prometheus**: <http://localhost:9090>
    - **Grafana**: Manual setup required (see Grafana Integration section)

## Usage

### Basic Benchmarking

```bash
# Run a mixed workload benchmark
go run ./runner/main.go -config ./config/mixed.yaml

# Run a read-heavy benchmark
go run ./runner/main.go -config ./config/read-heavy.yaml

# Run with custom parameters
go run ./runner/main.go \
  -config ./config/mixed.yaml \
  -output ./custom-results
```

### Grafana Integration

For time-series analysis and alerting, you can use Grafana:

1. **Open Grafana**: <http://localhost:3000> (admin/admin)

1. **Configured data sources**:
    - **Prometheus**:
      - URL: `http://localhost:9090` (default)
      - Access: Server (default)
    - **PostgreSQL**:
      - Host: `localhost:5432` (default)
      - Database: `jsonrpc_bench`
      - User: `postgres`
      - Password: `postgres`
      - SSL Mode: `disable`

1. **Provided dashboards** for:
    - K6 Benchmarks results comparison
    - Client performance comparison
    - Method-specific latency trends  
    - Error rate monitoring
    - System resource usage
    - Historic trend analysis

1. **Set up alerting** for performance regressions and system issues

## Advanced Features

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

load_test:
  target_rps: 500
  duration: "5m"
  ramp_duration: "30s"

methods:
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
docker-compose -f metrics/docker-compose.grafana.yml up -d
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

- **Grafana Dashboards**: Time-series visualizations

## Contributing

1. **Follow Go conventions** for backend code
2. **Use TypeScript** for frontend development
3. **Add tests** for new features
4. **Update documentation** for API changes
5. **Ensure backward compatibility** for configuration files

## License

This project is licensed under the MIT License - see the LICENSE file for details.
