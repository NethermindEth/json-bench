# Integration Tests for Historic Tracking System

This document provides comprehensive information about the integration tests for the historic tracking system in the JSON-RPC benchmark tool.

## Overview

The integration tests provide end-to-end testing of the entire historic tracking system, including:

- Database operations with PostgreSQL
- Historic data storage and retrieval
- Analysis components (baselines, trends, regression detection)
- HTTP API endpoints
- WebSocket real-time notifications
- Grafana SimpleJSON datasource integration
- Performance and scalability testing
- System recovery and resilience testing

## Test Files

### Primary Test Files

1. **`integration_test.go`** - Core integration test suite with basic PostgreSQL setup
2. **`integration_testcontainers_test.go`** - Enhanced tests using testcontainers for isolated PostgreSQL instances
3. **`test_config.yaml`** - Configuration templates and test parameters

### Supporting Files

- API test types and helpers are defined in `api/test_types.go`
- Database schema and migrations are in `storage/` package
- Configuration structures are in `config/` package

## Test Scenarios

### Scenario 1: Fresh System Setup and First Benchmark Run

**Purpose**: Tests the initial system setup and storage of the first benchmark run.

**Test Steps**:
1. Generate a sample benchmark result
2. Save the run to historic storage
3. Verify run can be retrieved from database
4. Verify files are saved to historic directory (if enabled)
5. Verify API endpoint returns the run data

**Validation**:
- Run ID is generated and stored correctly
- All required database fields are populated
- File storage works correctly
- API responds with correct data

### Scenario 2: Multiple Runs with Trend Analysis and Regression Detection

**Purpose**: Tests trend analysis and regression detection with multiple benchmark runs.

**Test Steps**:
1. Generate 10 benchmark runs with gradual performance degradation
2. Store runs with different timestamps
3. Calculate performance trends
4. Detect regressions using various comparison modes
5. Verify trend analysis API endpoints

**Validation**:
- Trend calculation identifies degradation patterns
- Regression detection works with different algorithms
- API endpoints return correct trend data
- Statistical analysis provides meaningful results

### Scenario 3: Baseline Management and Comparison Workflows

**Purpose**: Tests baseline creation, management, and comparison functionality.

**Test Steps**:
1. Create baselines from existing runs
2. Test baseline retrieval and listing
3. Compare runs against baselines
4. Test baseline deletion and cleanup
5. Test API endpoints for baseline management

**Validation**:
- Baselines are created and stored correctly
- Baseline comparisons provide accurate results
- API endpoints handle baseline operations properly
- Referential integrity is maintained

### Scenario 4: WebSocket Notifications During System Operations

**Purpose**: Tests real-time WebSocket notifications for system events.

**Test Steps**:
1. Establish WebSocket connections
2. Perform operations that should trigger notifications
3. Verify notifications are received
4. Test ping/pong message handling
5. Test connection management

**Validation**:
- WebSocket connections are established successfully
- Notifications are sent for relevant events
- Message format is correct
- Connection lifecycle is managed properly

### Scenario 5: Grafana Dashboard Data Queries

**Purpose**: Tests Grafana SimpleJSON datasource integration.

**Test Steps**:
1. Test Grafana root endpoint
2. Test metric search functionality
3. Test time-series data queries
4. Test tag key/value endpoints
5. Verify data format compatibility

**Validation**:
- All Grafana endpoints respond correctly
- Data is returned in SimpleJSON format
- Time-series queries work with various time ranges
- Metric filtering and search work correctly

### Scenario 6: Large Dataset Performance Testing

**Purpose**: Tests system performance with large amounts of data.

**Test Steps**:
1. Generate 100+ benchmark runs with complex data
2. Monitor memory usage during ingestion
3. Test query performance with large datasets
4. Verify system responsiveness under load
5. Test concurrent operations

**Validation**:
- Data ingestion completes within acceptable time
- Memory usage remains within limits
- Query performance is acceptable
- System remains responsive during high load

### Scenario 7: System Recovery After Failures

**Purpose**: Tests system resilience and recovery capabilities.

**Test Steps**:
1. Simulate database connection failures
2. Test filesystem permission issues
3. Test API server error handling
4. Verify graceful degradation
5. Test system recovery procedures

**Validation**:
- System handles failures gracefully
- Error messages are informative
- Recovery procedures work correctly
- Data integrity is maintained during failures

## Running the Tests

### Prerequisites

1. **Go 1.23+** - Required for running the tests
2. **PostgreSQL** - For basic integration tests
3. **Docker** - For testcontainers-based tests
4. **Make** (optional) - For using Makefile commands

### Environment Variables

```bash
# Enable basic integration tests
export INTEGRATION_TESTS=1

# Enable testcontainers tests (requires Docker)
export TESTCONTAINERS_TESTS=1

# Optional: Configure test database
export TEST_DB_HOST=localhost
export TEST_DB_PORT=5432
export TEST_DB_NAME=jsonrpc_bench_test
export TEST_DB_USER=postgres
export TEST_DB_PASSWORD=postgres
```

### Running Basic Integration Tests

```bash
# Run all integration tests
go test -v ./runner -run TestIntegrationSuite

# Run specific test scenario
go test -v ./runner -run TestIntegrationSuite/TestScenario1_FreshSystemSetup

# Run with race detection
go test -race -v ./runner -run TestIntegrationSuite

# Run with coverage
go test -cover -v ./runner -run TestIntegrationSuite
```

### Running Testcontainers Tests

```bash
# Run testcontainers-based tests
TESTCONTAINERS_TESTS=1 go test -v ./runner -run TestTestContainersIntegrationSuite

# Run complete system lifecycle test
TESTCONTAINERS_TESTS=1 go test -v ./runner -run TestTestContainersIntegrationSuite/TestCompleteSystemLifecycle

# Run database-specific tests
TESTCONTAINERS_TESTS=1 go test -v ./runner -run TestTestContainersIntegrationSuite/TestDatabaseMigrations
```

### Running Performance Tests

```bash
# Run performance and load tests
go test -v ./runner -run "TestScenario6_LargeDatasetPerformance|TestConcurrentAccess|TestDatabasePerformanceUnderLoad"

# Run with extended timeout for long-running tests
go test -timeout 10m -v ./runner -run TestScenario6_LargeDatasetPerformance
```

## Test Configuration

### Database Configuration

The tests require a PostgreSQL database. You can either:

1. **Use an existing PostgreSQL instance**:
   ```yaml
   postgresql:
     host: "localhost"
     port: 5432
     database: "jsonrpc_bench_test"
     username: "postgres"
     password: "postgres"
     ssl_mode: "disable"
   ```

2. **Use testcontainers (recommended)**:
   - Tests automatically start a PostgreSQL container
   - No manual database setup required
   - Isolated test environment
   - Automatic cleanup

### Test Data Generation

Tests generate realistic benchmark data with:

- Multiple clients (client-1, client-2, client-3)
- Various JSON-RPC methods (eth_getBalance, eth_getBlockByNumber, etc.)
- Realistic latency distributions
- Error rates and status codes
- Time-series data points

### Performance Thresholds

Current performance expectations:

- **Data Ingestion**: < 60 seconds for 100 runs
- **Query Performance**: < 10 seconds for complex queries
- **Memory Usage**: < 100MB increase during large dataset tests
- **Concurrent Operations**: 0% error rate expected
- **Load Tests**: < 10% error rate under high load

## Test Coverage

### Functional Coverage

- ✅ Historic run storage and retrieval
- ✅ Trend analysis and calculation
- ✅ Regression detection algorithms
- ✅ Baseline management
- ✅ API endpoint functionality
- ✅ WebSocket notifications
- ✅ Grafana integration
- ✅ Database migrations
- ✅ File storage operations

### Non-Functional Coverage

- ✅ Performance under load
- ✅ Concurrent access handling
- ✅ Memory usage monitoring
- ✅ Error handling and recovery
- ✅ Data integrity and consistency
- ✅ Connection pool management
- ✅ Resource cleanup

### Database Coverage

- ✅ PostgreSQL-specific features (JSONB, arrays, full-text search)
- ✅ Transaction handling
- ✅ Migration up/down operations
- ✅ Index performance
- ✅ Referential integrity
- ✅ Backup and restore procedures

## Troubleshooting

### Common Issues

1. **Database Connection Failures**
   ```
   Error: failed to connect to PostgreSQL
   ```
   - Ensure PostgreSQL is running
   - Check connection parameters
   - Verify database exists and user has permissions

2. **Docker Not Available**
   ```
   Error: Docker not available for testcontainers
   ```
   - Install Docker and ensure it's running
   - Check Docker daemon permissions
   - Try running basic Docker commands manually

3. **Test Timeouts**
   ```
   Error: test timed out
   ```
   - Increase test timeout with `-timeout` flag
   - Check system resources
   - Verify database performance

4. **Memory Issues**
   ```
   Error: out of memory
   ```
   - Reduce test data size in configuration
   - Check for memory leaks
   - Increase available system memory

### Debug Mode

Enable verbose logging:

```bash
export LOG_LEVEL=debug
go test -v ./runner -run TestIntegrationSuite
```

### Test Cleanup

If tests fail and leave resources:

```bash
# Stop any running containers
docker ps -a | grep postgres | awk '{print $1}' | xargs docker rm -f

# Clean up test directories
rm -rf /tmp/jsonrpc-bench-integration-*

# Reset test database (if using persistent DB)
psql -h localhost -U postgres -c "DROP DATABASE IF EXISTS jsonrpc_bench_test;"
psql -h localhost -U postgres -c "CREATE DATABASE jsonrpc_bench_test;"
```

## Contributing

### Adding New Test Scenarios

1. Create a new test method in the integration suite
2. Follow the naming convention: `TestScenario{N}_{Description}`
3. Include proper setup, execution, and validation phases
4. Add configuration to `test_config.yaml`
5. Update this documentation

### Performance Benchmarks

When adding performance tests:

1. Set realistic performance thresholds
2. Monitor resource usage (memory, CPU, connections)
3. Include both success and failure scenarios
4. Document expected performance characteristics

### Test Data

For consistent test data:

1. Use the `generateBenchmarkResult()` helper method
2. Follow realistic data patterns
3. Include edge cases and boundary conditions
4. Ensure data cleanup in teardown methods

## Continuous Integration

### CI Configuration

For CI environments, use environment variables:

```yaml
env:
  INTEGRATION_TESTS: 1
  TESTCONTAINERS_TESTS: 1
  GO_TEST_TIMEOUT: 20m
```

### Docker Requirements

Ensure CI environment has:
- Docker daemon running
- Sufficient memory (4GB+ recommended)
- Network access for pulling container images

### Test Parallelization

Tests can be run in parallel, but database tests should use separate schemas or databases to avoid conflicts.

## Metrics and Monitoring

### Test Metrics

The integration tests collect:

- Execution time for each scenario
- Memory usage throughout test execution
- Database query performance
- API response times
- WebSocket message delivery times

### Performance Reports

After test execution, check logs for:

```
INFO[...] Large dataset performance metrics  
  avg_memory_bytes=52428800 
  duration=45.2s 
  max_memory_bytes=67108864 
  memory_increase=14680064 
  num_runs=100 
  ops_per_second=2.21
```

This comprehensive integration test suite ensures the historic tracking system works correctly in production-like conditions and provides confidence for deployment and scaling decisions.