# Ethereum JSON-RPC Benchmarking Suite

A comprehensive benchmarking framework for Ethereum JSON-RPC clients, designed to evaluate and compare the performance and behavior of different client implementations like Geth and Nethermind.

## Overview

This project runs predefined RPC tests derived from the official Ethereum Execution APIs spec, generates standardized performance metrics, checks for response consistency, and exposes a live public HTML report.

## Features

- Benchmark and compare Ethereum clients under realistic load using a unified framework
- Track and visualize metrics like latency, throughput, and resource usage
- Validate RPC response compatibility with [ethereum/execution-apis](https://github.com/ethereum/execution-apis)
- Perform schema validation against official Ethereum JSON-RPC specifications
- Highlight inconsistencies across client responses to aid standardization
- Support multiple parameter variations per method to test response consistency
- Group comparison results by error status and API namespaces in HTML reports
- Expose an always-accessible public HTML report
- Provide pre-provisioned clients (no setup required before test runs)

## Project Structure

```
benchmark-suite/
│
├── clients/                  # docker-compose setups for each client
│   ├── geth/
│   └── nethermind/
│
├── config/                   # YAML test configurations
│   ├── read-heavy.yaml
│   ├── mixed.yaml
│   └── param_variations.yaml  # Parameter variations for methods
│
├── runner/                   # Golang benchmark runner
│   ├── main.go
│   ├── loader.go
│   ├── validator.go
│   └── html_report.go
│
├── scripts/                  # K6 generators and test JS
│   └── template.js
│
├── metrics/                  # Prometheus + Grafana setup
│   ├── prometheus.yml
│   └── dashboards/
│
└── web/                      # HTML report server
    └── serve.go
```

## Getting Started

### Prerequisites

- Docker and Docker Compose
- Go 1.20 or later
- k6 (for load testing)
- Prometheus and Grafana (for metrics visualization)

### Installation

1. Clone this repository
2. Set up the client nodes using Docker Compose
3. Configure your test scenarios in YAML
4. Run the benchmarks using the Go CLI tool

## Usage

```bash
# Run the clients to compare

# Update ./config/mixed.yaml or ./config/read-heavy.yaml with the endpoints to the nodes

# Run a benchmark
go run ./runner/main.go -config ./config/mixed.yaml

# View the results
open results/report.htm
```

### JSON-RPC Response Comparison

The suite includes tools to compare JSON-RPC responses across different clients:

```bash
# Compare responses using a YAML configuration
go run ./cmd/compare-config/main.go -config ./config/example-config.yaml

# Compare responses using an OpenRPC specification
go run ./cmd/compare-openrpc/main.go -spec https://raw.githubusercontent.com/ethereum/execution-apis/main/openrpc.json -clients "geth:http://localhost:8545,erigon:http://localhost:8546"

# Compare responses using a filter for methods
go run ./cmd/compare-openrpc/main.go -spec https://raw.githubusercontent.com/ethereum/execution-apis/main/openrpc.json -clients "geth:http://localhost:8545,erigon:http://localhost:8546" -filter "eth_call"

# Compare with parameter variations
go run ./cmd/compare-openrpc/main.go -spec https://raw.githubusercontent.com/ethereum/execution-apis/main/openrpc.json -variations ./config/param_variations.yaml -clients "geth:http://localhost:8545,erigon:http://localhost:8546"
```

### Parameter Variations

You can test methods with different parameter sets using a YAML configuration file:

```yaml
# Parameter variations for testing method compatibility between clients
# Format: method_name: [variation1, variation2, ...]

eth_createAccessList:
  - [{}, null]
  - [{"from": "0x0000000000000000000000000000000000000000"}, "latest"]

eth_call:
  - [{"to": "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"}, "latest"]
  - [{"to": "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"}, "pending"]
```

## License

This project is licensed under the MIT License - see the LICENSE file for details.
