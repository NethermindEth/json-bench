# Integration Tests - Quick Start Guide

This directory contains comprehensive integration tests for the JSON-RPC Benchmark Historic Tracking System.

## 🚀 Quick Start

### Prerequisites

- Go 1.23+
- Docker (for testcontainers tests)
- PostgreSQL (for basic tests, optional if using testcontainers)

### Run All Tests

```bash
# Basic integration tests (requires local PostgreSQL)
make test-integration

# Advanced tests with testcontainers (requires Docker)
make test-containers

# Quick test subset
make test-quick
```

## 📋 Test Files Overview

| File | Description | Key Features |
|------|-------------|--------------|
| `integration_test.go` | Core integration test suite | Basic PostgreSQL setup, all 7 scenarios |
| `integration_testcontainers_test.go` | Enhanced testcontainers tests | Isolated PostgreSQL containers, advanced features |
| `test_config.yaml` | Test configuration | Scenarios, thresholds, data generation config |
| `INTEGRATION_TESTS.md` | Complete documentation | Detailed test descriptions and troubleshooting |
| `Makefile` | Test automation | 30+ make targets for different test types |

## 🧪 Test Scenarios

| Scenario | Description | Key Tests |
|----------|-------------|-----------|
| **1. Fresh System Setup** | Initial system setup and first run | Database setup, file storage, API responses |
| **2. Trend Analysis** | Multiple runs with performance tracking | Regression detection, trend calculation |
| **3. Baseline Management** | Baseline creation and comparison | CRUD operations, API endpoints |
| **4. WebSocket Notifications** | Real-time system notifications | Connection management, message delivery |
| **5. Grafana Integration** | Dashboard data queries | SimpleJSON datasource, time-series data |
| **6. Performance Testing** | Large dataset handling | Memory usage, query performance, concurrency |
| **7. System Recovery** | Failure resilience and recovery | Error handling, graceful degradation |

## 🎯 Common Use Cases

### Development Testing

```bash
# Test specific functionality
make test-scenario-1    # Fresh system setup
make test-scenario-3    # Baseline management
make test-api          # API endpoints only
make test-websocket    # WebSocket functionality
```

### Performance Validation

```bash
# Performance and load testing
make test-performance
make test-database
make bench-performance
```

### CI/CD Pipeline

```bash
# Suitable for CI environments
make ci-test          # Full CI test suite
make ci-quick         # Quick CI validation
```

### Debugging and Development

```bash
# Debug mode with verbose logging
make test-debug
make test-verbose
make test-race        # Race condition detection
```

## 📊 Test Coverage

### ✅ Functional Coverage

- Historic run storage and retrieval
- Trend analysis and regression detection
- Baseline management and comparisons
- API endpoint functionality
- WebSocket real-time notifications
- Grafana SimpleJSON integration
- Database migrations and schema management
- File storage operations

### ✅ Non-Functional Coverage

- Performance under load (100+ concurrent operations)
- Memory usage monitoring and limits
- Concurrent access and race condition testing
- Error handling and system recovery
- Data integrity and consistency validation
- Connection pool management
- Resource cleanup and garbage collection

### ✅ Database Coverage

- PostgreSQL-specific features (JSONB, arrays, full-text search)
- Transaction handling and rollback scenarios
- Migration up/down operations
- Index performance optimization
- Referential integrity constraints
- Backup and restore procedures

## 🔧 Configuration

### Environment Variables

```bash
export INTEGRATION_TESTS=1        # Enable basic integration tests
export TESTCONTAINERS_TESTS=1     # Enable testcontainers tests
export LOG_LEVEL=debug             # Enable debug logging
```

### Database Configuration

```bash
# For local PostgreSQL testing
export TEST_DB_HOST=localhost
export TEST_DB_PORT=5432
export TEST_DB_NAME=jsonrpc_bench_test
export TEST_DB_USER=postgres
export TEST_DB_PASSWORD=postgres
```

## 📈 Performance Expectations

| Metric | Expected Value | Test Scenario |
|--------|---------------|---------------|
| Data Ingestion | < 60s for 100 runs | Large Dataset Performance |
| Query Performance | < 10s for complex queries | Database Load Testing |
| Memory Usage | < 100MB increase | Performance Monitoring |
| Concurrent Operations | 0% error rate | Concurrency Testing |
| Load Testing | < 10% error rate | High Load Scenarios |

## 🛠️ Troubleshooting

### Common Issues

**Database Connection Failed**

```bash
# Check PostgreSQL is running
pg_ctl status
# Or use testcontainers instead
make test-containers
```

**Docker Not Available**

```bash
# Verify Docker installation
docker --version
docker info
# Run Docker test
make setup-docker
```

**Test Timeouts**

```bash
# Increase timeout for long-running tests
go test -timeout 20m -v ./runner
```

### Cleanup

```bash
# Clean up test artifacts and containers
make clean
# Complete cleanup including cache
make clean-all
```

## 📚 Documentation Links

- **[Complete Integration Test Documentation](INTEGRATION_TESTS.md)** - Detailed test descriptions, scenarios, and troubleshooting
- **[Test Configuration Reference](test_config.yaml)** - All configuration options and templates
- **[API Test Types](api/test_types.go)** - API testing structures and helpers
- **[Storage Schema](storage/)** - Database schema and migration files

## 🤝 Contributing

### Adding New Tests

1. Follow the `TestScenario{N}_{Description}` naming convention
2. Include setup, execution, and validation phases
3. Add configuration to `test_config.yaml`
4. Update documentation

### Performance Testing Guidelines

- Set realistic performance thresholds
- Monitor resource usage (memory, CPU, connections)
- Include both success and failure scenarios
- Document expected performance characteristics

## 📋 Make Targets Reference

### Essential Commands

```bash
make help               # Show all available targets
make setup              # Set up test environment
make test-integration   # Run basic integration tests
make test-containers    # Run testcontainers tests
make test-quick         # Run quick test subset
make clean              # Clean up test artifacts
```

### Scenario-Specific Tests

```bash
make test-scenario-1    # Fresh System Setup
make test-scenario-2    # Trend Analysis
make test-scenario-3    # Baseline Management
make test-scenario-4    # WebSocket Notifications
make test-scenario-5    # Grafana Integration
make test-scenario-6    # Performance Testing
make test-scenario-7    # System Recovery
```

### Advanced Testing

```bash
make test-performance   # Performance and load tests
make test-race          # Race condition detection
make test-coverage      # Generate coverage report
make test-debug         # Debug mode with verbose logging
make ci-test            # CI-suitable test suite
```

This integration test suite provides comprehensive validation of the historic tracking system, ensuring reliability, performance, and correctness in production-like environments.
