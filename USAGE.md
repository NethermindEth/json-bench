# JSON-RPC Benchmarking Suite Usage Guide

This guide will help you get started with the Ethereum JSON-RPC Benchmarking Suite.

## Prerequisites

- Docker and Docker Compose
- Go 1.20 or later
- k6 (for load testing)

## Quick Start

### 1. Start the Client Nodes

You can start the Ethereum client nodes using Docker Compose:

```bash
# Start Geth
docker-compose -f clients/geth/docker-compose.yml up -d

# Start Nethermind
docker-compose -f clients/nethermind/docker-compose.yml up -d
```

### 2. Run a Benchmark

Use the Go CLI tool to run a benchmark:

```bash
# Build the benchmark tool
go build -o benchmark ./runner/main.go

# Run a benchmark with a predefined configuration
./benchmark -config ./config/read-heavy.yaml
```

### 3. View the Results

After running a benchmark, you can view the HTML report:

```bash
# Start the report server
go run ./web/serve.go

# Open the report in your browser
open http://localhost:8080/report/latest
```

## Configuration

The benchmark suite uses YAML files for configuration. Here's an example:

```yaml
test_name: "Read-heavy RPC benchmark"
description: "Benchmark focusing on read operations like eth_call and eth_getLogs"
clients:
  - name: "geth"
    url: "http://localhost:8545"
  - name: "nethermind"
    url: "http://localhost:8545"
duration: "5m"
rps: 200
endpoints:
  - method: "eth_call"
    params:
      - to: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"
        data: "0x70a08231000000000000000000000000000000000000000000000000000000000000000a"
    frequency: 60%
  - method: "eth_getLogs"
    params:
      - fromBlock: "0x1000000"
        toBlock: "0x1000100"
        address: ["0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"]
    frequency: 30%
  - method: "eth_getBlockByNumber"
    params:
      - "latest"
      - true
    frequency: 10%
validate_responses: true
```

## Using Docker Compose for the Full Stack

You can start the entire benchmarking stack using the main Docker Compose file:

```bash
docker-compose up -d
```

This will start:
- Ethereum clients (Geth and Nethermind)
- Prometheus for metrics collection
- Grafana for metrics visualization
- A web server for viewing HTML reports

## Metrics Visualization

Access the Grafana dashboard at http://localhost:3000 (default credentials: admin/admin).

## Adding New Clients

To add a new client:

1. Create a new Docker Compose file in the `clients/` directory
2. Add the client to your YAML configuration files
3. Update the main Docker Compose file to include the new client

## Custom Test Scenarios

You can create custom test scenarios by creating new YAML configuration files in the `config/` directory.

## Response Validation

The benchmark suite can validate responses from different clients to identify inconsistencies. Enable this feature by setting `validate_responses: true` in your YAML configuration.

## Troubleshooting

- If clients are not accessible, check that they are running and that the URLs in your configuration are correct
- If k6 fails to run, ensure it's installed and in your PATH
- For Docker-related issues, check the container logs using `docker-compose logs`
