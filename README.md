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
│   └── mixed.yaml
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
# Start the client nodes
docker-compose -f clients/geth/docker-compose.yml up -d
docker-compose -f clients/nethermind/docker-compose.yml up -d

# Run a benchmark
go run ./runner/main.go -config ./config/read-heavy.yaml

# View the results
open http://localhost:8080/report/latest
```

## License

This project is licensed under the MIT License - see the LICENSE file for details.
