# Makefile for JSON-RPC Benchmark Integration Tests

.PHONY: help test test-integration test-containers test-performance test-coverage test-race clean setup deps

# Default target
help: ## Show this help message
	@echo "JSON-RPC Benchmark Integration Tests"
	@echo "===================================="
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Setup and dependencies
setup: ## Set up test environment and dependencies
	@echo "Setting up test environment..."
	go mod tidy
	go mod download
	@echo "Checking Docker availability..."
	@if command -v docker >/dev/null 2>&1; then \
		echo "Docker is available"; \
		docker --version; \
	else \
		echo "Warning: Docker not found. Testcontainers tests will be skipped."; \
	fi

deps: ## Install test dependencies
	@echo "Installing test dependencies..."
	go get -t ./...
	go mod tidy

# Test targets
test: ## Run basic unit tests
	@echo "Running basic unit tests..."
	go test -v ./...

test-integration: ## Run integration tests (requires PostgreSQL)
	@echo "Running integration tests..."
	@echo "Note: Requires PostgreSQL to be running locally"
	INTEGRATION_TESTS=1 go test -v ./runner -run TestIntegrationSuite

test-containers: ## Run testcontainers integration tests (requires Docker)
	@echo "Running testcontainers integration tests..."
	@echo "Note: Requires Docker to be running"
	TESTCONTAINERS_TESTS=1 go test -v ./runner -run TestTestContainersIntegrationSuite

test-containers-lifecycle: ## Run complete system lifecycle test with containers
	@echo "Running complete system lifecycle test..."
	TESTCONTAINERS_TESTS=1 go test -v ./runner -run TestTestContainersIntegrationSuite/TestCompleteSystemLifecycle

test-performance: ## Run performance and load tests
	@echo "Running performance tests..."
	INTEGRATION_TESTS=1 go test -timeout 15m -v ./runner -run "TestScenario6_LargeDatasetPerformance|TestConcurrentAccess"

test-database: ## Run database-specific tests
	@echo "Running database-specific tests..."
	TESTCONTAINERS_TESTS=1 go test -v ./runner -run "TestDatabaseMigrations|TestPostgreSQLSpecificFeatures|TestDatabasePerformanceUnderLoad"

test-api: ## Run API endpoint tests
	@echo "Running API endpoint tests..."
	INTEGRATION_TESTS=1 go test -v ./runner -run "TestScenario3_BaselineManagement|TestScenario5_GrafanaDashboardQueries"

test-websocket: ## Run WebSocket tests
	@echo "Running WebSocket tests..."
	INTEGRATION_TESTS=1 go test -v ./runner -run "TestScenario4_WebSocketNotifications|TestWebSocketConnectionLimits"

test-recovery: ## Run system recovery tests
	@echo "Running system recovery tests..."
	INTEGRATION_TESTS=1 go test -v ./runner -run "TestScenario7_SystemRecovery"

# Advanced test options
test-coverage: ## Run tests with coverage report
	@echo "Running tests with coverage..."
	INTEGRATION_TESTS=1 go test -cover -coverprofile=coverage.out ./runner
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-race: ## Run tests with race detection
	@echo "Running tests with race detection..."
	INTEGRATION_TESTS=1 go test -race -v ./runner

test-verbose: ## Run all tests with verbose output
	@echo "Running all tests with verbose output..."
	INTEGRATION_TESTS=1 TESTCONTAINERS_TESTS=1 go test -v ./runner

test-quick: ## Run quick subset of integration tests
	@echo "Running quick integration tests..."
	INTEGRATION_TESTS=1 go test -v ./runner -run "TestScenario1_FreshSystemSetup|TestScenario3_BaselineManagement"

# Test with specific scenarios
test-scenario-1: ## Run Scenario 1: Fresh System Setup
	INTEGRATION_TESTS=1 go test -v ./runner -run TestScenario1_FreshSystemSetup

test-scenario-2: ## Run Scenario 2: Trend Analysis
	INTEGRATION_TESTS=1 go test -v ./runner -run TestScenario2_TrendAnalysisAndRegressionDetection

test-scenario-3: ## Run Scenario 3: Baseline Management
	INTEGRATION_TESTS=1 go test -v ./runner -run TestScenario3_BaselineManagement

test-scenario-4: ## Run Scenario 4: WebSocket Notifications
	INTEGRATION_TESTS=1 go test -v ./runner -run TestScenario4_WebSocketNotifications

test-scenario-5: ## Run Scenario 5: Grafana Integration
	INTEGRATION_TESTS=1 go test -v ./runner -run TestScenario5_GrafanaDashboardQueries

test-scenario-6: ## Run Scenario 6: Performance Testing
	INTEGRATION_TESTS=1 go test -timeout 15m -v ./runner -run TestScenario6_LargeDatasetPerformance

test-scenario-7: ## Run Scenario 7: System Recovery
	INTEGRATION_TESTS=1 go test -v ./runner -run TestScenario7_SystemRecovery

# Development and debugging
test-debug: ## Run tests with debug logging
	@echo "Running tests with debug logging..."
	LOG_LEVEL=debug INTEGRATION_TESTS=1 go test -v ./runner

test-local-postgres: ## Run tests against local PostgreSQL (requires setup)
	@echo "Running tests against local PostgreSQL..."
	@echo "Ensure PostgreSQL is running with database 'jsonrpc_bench_test'"
	TEST_DB_HOST=localhost \
	TEST_DB_PORT=5432 \
	TEST_DB_NAME=jsonrpc_bench_test \
	TEST_DB_USER=postgres \
	TEST_DB_PASSWORD=postgres \
	INTEGRATION_TESTS=1 go test -v ./runner

# Cleanup
clean: ## Clean up test artifacts and containers
	@echo "Cleaning up test artifacts..."
	rm -f coverage.out coverage.html
	rm -rf /tmp/jsonrpc-bench-integration-*
	@echo "Stopping and removing test containers..."
	@if command -v docker >/dev/null 2>&1; then \
		docker ps -a | grep postgres | awk '{print $$1}' | xargs -r docker rm -f; \
		docker ps -a | grep testcontainers | awk '{print $$1}' | xargs -r docker rm -f; \
		echo "Cleaned up Docker containers"; \
	else \
		echo "Docker not available, skipping container cleanup"; \
	fi

clean-all: clean ## Clean everything including Go module cache
	go clean -modcache
	go clean -testcache

# Continuous Integration targets
ci-test: ## Run tests suitable for CI environment
	@echo "Running CI tests..."
	INTEGRATION_TESTS=1 TESTCONTAINERS_TESTS=1 go test -timeout 20m -race -cover ./runner

ci-quick: ## Run quick CI tests
	@echo "Running quick CI tests..."
	INTEGRATION_TESTS=1 go test -timeout 10m ./runner -run "TestScenario1_FreshSystemSetup|TestScenario3_BaselineManagement|TestScenario5_GrafanaDashboardQueries"

# Benchmarking
bench: ## Run benchmark tests
	@echo "Running benchmark tests..."
	go test -bench=. -benchmem ./runner

bench-performance: ## Run performance benchmarks
	@echo "Running performance benchmarks..."
	INTEGRATION_TESTS=1 go test -bench=BenchmarkLargeDataset -benchtime=30s ./runner

# Documentation and validation
validate-config: ## Validate test configuration
	@echo "Validating test configuration..."
	@if [ -f test_config.yaml ]; then \
		echo "test_config.yaml exists"; \
		head -10 test_config.yaml; \
	else \
		echo "Warning: test_config.yaml not found"; \
	fi

docs: ## Generate test documentation
	@echo "Test documentation available in:"
	@echo "  - INTEGRATION_TESTS.md"
	@echo "  - test_config.yaml"
	@echo "  - API documentation in api/ package"

# Environment setup helpers
setup-postgres: ## Set up local PostgreSQL for testing
	@echo "Setting up local PostgreSQL..."
	@echo "Creating test database..."
	@if command -v psql >/dev/null 2>&1; then \
		psql -h localhost -U postgres -c "CREATE DATABASE IF NOT EXISTS jsonrpc_bench_test;" || true; \
		echo "Database created: jsonrpc_bench_test"; \
	else \
		echo "psql not found. Please install PostgreSQL client tools."; \
	fi

setup-docker: ## Verify Docker setup for testcontainers
	@echo "Verifying Docker setup..."
	@if command -v docker >/dev/null 2>&1; then \
		docker --version; \
		docker info > /dev/null 2>&1 && echo "Docker daemon is running" || echo "Docker daemon is not running"; \
		docker run --rm hello-world && echo "Docker is working correctly" || echo "Docker test failed"; \
	else \
		echo "Docker not found. Please install Docker for testcontainers support."; \
	fi

# Monitoring and metrics
test-metrics: ## Run tests and collect performance metrics
	@echo "Running tests with performance metrics..."
	@echo "Memory usage will be monitored during test execution"
	INTEGRATION_TESTS=1 go test -v ./runner -run TestScenario6_LargeDatasetPerformance | grep -E "(memory|duration|ops_per_second)"

monitor-resources: ## Monitor system resources during tests
	@echo "Starting resource monitoring..."
	@echo "Run 'make test-performance' in another terminal"
	@while true; do \
		echo "Memory: $$(free -h | grep Mem | awk '{print $$3 "/" $$2}') CPU: $$(top -bn1 | grep "Cpu(s)" | awk '{print $$2}' | sed 's/%us,//')"; \
		sleep 5; \
	done

# Default PostgreSQL connection parameters for local development
export TEST_DB_HOST ?= localhost
export TEST_DB_PORT ?= 5432
export TEST_DB_NAME ?= jsonrpc_bench_test
export TEST_DB_USER ?= postgres
export TEST_DB_PASSWORD ?= postgres

# Go test parameters
export GO_TEST_TIMEOUT ?= 10m
export GO_TEST_PARALLEL ?= 4

# Show current configuration
show-config: ## Show current test configuration
	@echo "Current Test Configuration:"
	@echo "=========================="
	@echo "TEST_DB_HOST:      $(TEST_DB_HOST)"
	@echo "TEST_DB_PORT:      $(TEST_DB_PORT)"
	@echo "TEST_DB_NAME:      $(TEST_DB_NAME)"
	@echo "TEST_DB_USER:      $(TEST_DB_USER)"
	@echo "GO_TEST_TIMEOUT:   $(GO_TEST_TIMEOUT)"
	@echo "GO_TEST_PARALLEL:  $(GO_TEST_PARALLEL)"
	@echo ""
	@echo "Environment Variables:"
	@echo "INTEGRATION_TESTS: $${INTEGRATION_TESTS:-not set}"
	@echo "TESTCONTAINERS_TESTS: $${TESTCONTAINERS_TESTS:-not set}"
	@echo "LOG_LEVEL: $${LOG_LEVEL:-not set}"