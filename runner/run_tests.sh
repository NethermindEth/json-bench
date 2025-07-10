#!/bin/bash

# Integration Test Runner Script
# This script provides an easy way to run the integration tests with proper setup and validation

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to check prerequisites
check_prerequisites() {
    print_status "Checking prerequisites..."
    
    # Check Go version
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed or not in PATH"
        exit 1
    fi
    
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    print_status "Go version: $GO_VERSION"
    
    # Check Docker (optional)
    if command -v docker &> /dev/null; then
        if docker info &> /dev/null; then
            DOCKER_VERSION=$(docker --version | awk '{print $3}' | sed 's/,//')
            print_status "Docker version: $DOCKER_VERSION (available)"
            DOCKER_AVAILABLE=true
        else
            print_warning "Docker is installed but daemon is not running"
            DOCKER_AVAILABLE=false
        fi
    else
        print_warning "Docker not found - testcontainers tests will be skipped"
        DOCKER_AVAILABLE=false
    fi
    
    # Check PostgreSQL (optional)
    if command -v psql &> /dev/null; then
        print_status "PostgreSQL client tools available"
        POSTGRES_CLIENT_AVAILABLE=true
    else
        print_warning "PostgreSQL client tools not found"
        POSTGRES_CLIENT_AVAILABLE=false
    fi
}

# Function to setup test environment
setup_environment() {
    print_status "Setting up test environment..."
    
    # Ensure we're in the correct directory
    cd "$(dirname "$0")"
    
    # Install dependencies
    print_status "Installing Go dependencies..."
    go mod tidy
    go mod download
    
    # Create test directories if needed
    mkdir -p ../test-output
    
    print_success "Environment setup completed"
}

# Function to run specific test type
run_test_type() {
    local test_type=$1
    local description=$2
    
    print_status "Running $description..."
    
    case $test_type in
        "basic")
            if [ "$POSTGRES_CLIENT_AVAILABLE" = true ]; then
                export INTEGRATION_TESTS=1
                go test -v ./runner -run TestIntegrationSuite
            else
                print_warning "Skipping basic integration tests - PostgreSQL client not available"
                return 1
            fi
            ;;
        "containers")
            if [ "$DOCKER_AVAILABLE" = true ]; then
                export TESTCONTAINERS_TESTS=1
                go test -v ./runner -run TestTestContainersIntegrationSuite
            else
                print_warning "Skipping testcontainers tests - Docker not available"
                return 1
            fi
            ;;
        "quick")
            export INTEGRATION_TESTS=1
            go test -v ./runner -run "TestScenario1_FreshSystemSetup|TestScenario3_BaselineManagement"
            ;;
        "performance")
            export INTEGRATION_TESTS=1
            go test -timeout 15m -v ./runner -run "TestScenario6_LargeDatasetPerformance|TestConcurrentAccess"
            ;;
        "coverage")
            export INTEGRATION_TESTS=1
            go test -cover -coverprofile=coverage.out ./runner
            go tool cover -html=coverage.out -o coverage.html
            print_success "Coverage report generated: coverage.html"
            ;;
        *)
            print_error "Unknown test type: $test_type"
            return 1
            ;;
    esac
}

# Function to show usage
show_usage() {
    echo "Integration Test Runner"
    echo "======================"
    echo ""
    echo "Usage: $0 [OPTIONS] [TEST_TYPE]"
    echo ""
    echo "Test Types:"
    echo "  basic       - Run basic integration tests (requires PostgreSQL)"
    echo "  containers  - Run testcontainers tests (requires Docker)"
    echo "  quick       - Run quick subset of tests"
    echo "  performance - Run performance and load tests"
    echo "  coverage    - Run tests with coverage report"
    echo "  all         - Run all available tests"
    echo ""
    echo "Options:"
    echo "  -h, --help     Show this help message"
    echo "  -v, --verbose  Enable verbose output"
    echo "  -c, --check    Only check prerequisites, don't run tests"
    echo "  -s, --setup    Only setup environment, don't run tests"
    echo ""
    echo "Examples:"
    echo "  $0 quick                    # Run quick tests"
    echo "  $0 containers               # Run testcontainers tests"
    echo "  $0 -v performance           # Run performance tests with verbose output"
    echo "  $0 --check                  # Check prerequisites only"
    echo ""
    echo "Environment Variables:"
    echo "  LOG_LEVEL=debug             # Enable debug logging"
    echo "  TEST_DB_HOST=localhost      # PostgreSQL host (for basic tests)"
    echo "  TEST_DB_PORT=5432           # PostgreSQL port"
    echo "  TEST_DB_NAME=jsonrpc_bench_test # Database name"
    echo "  TEST_DB_USER=postgres       # Database user"
    echo "  TEST_DB_PASSWORD=postgres   # Database password"
}

# Function to run all available tests
run_all_tests() {
    print_status "Running all available tests..."
    
    local tests_run=0
    local tests_failed=0
    
    # Try quick tests first
    if run_test_type "quick" "Quick Integration Tests"; then
        tests_run=$((tests_run + 1))
        print_success "Quick tests passed"
    else
        tests_failed=$((tests_failed + 1))
        print_error "Quick tests failed"
    fi
    
    # Try basic integration tests
    if run_test_type "basic" "Basic Integration Tests"; then
        tests_run=$((tests_run + 1))
        print_success "Basic integration tests passed"
    else
        tests_failed=$((tests_failed + 1))
        print_warning "Basic integration tests skipped or failed"
    fi
    
    # Try testcontainers tests
    if run_test_type "containers" "Testcontainers Tests"; then
        tests_run=$((tests_run + 1))
        print_success "Testcontainers tests passed"
    else
        tests_failed=$((tests_failed + 1))
        print_warning "Testcontainers tests skipped or failed"
    fi
    
    # Try performance tests
    if run_test_type "performance" "Performance Tests"; then
        tests_run=$((tests_run + 1))
        print_success "Performance tests passed"
    else
        tests_failed=$((tests_failed + 1))
        print_warning "Performance tests skipped or failed"
    fi
    
    print_status "Test Summary: $tests_run tests run, $tests_failed failed/skipped"
    
    if [ $tests_run -eq 0 ]; then
        print_error "No tests could be run. Check prerequisites."
        exit 1
    elif [ $tests_failed -gt 0 ]; then
        print_warning "Some tests failed or were skipped."
        exit 1
    else
        print_success "All tests passed!"
    fi
}

# Function to show system information
show_system_info() {
    print_status "System Information:"
    echo "  OS: $(uname -s) $(uname -r)"
    echo "  Architecture: $(uname -m)"
    if command -v go &> /dev/null; then
        echo "  Go: $(go version | awk '{print $3}')"
    fi
    if command -v docker &> /dev/null; then
        echo "  Docker: $(docker --version 2>/dev/null | awk '{print $3}' | sed 's/,//' || echo "not available")"
    fi
    if command -v psql &> /dev/null; then
        echo "  PostgreSQL Client: $(psql --version | awk '{print $3}')"
    fi
    echo "  PWD: $(pwd)"
}

# Main script logic
main() {
    local verbose=false
    local check_only=false
    local setup_only=false
    local test_type=""
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_usage
                exit 0
                ;;
            -v|--verbose)
                verbose=true
                shift
                ;;
            -c|--check)
                check_only=true
                shift
                ;;
            -s|--setup)
                setup_only=true
                shift
                ;;
            basic|containers|quick|performance|coverage|all)
                test_type=$1
                shift
                ;;
            *)
                print_error "Unknown option: $1"
                show_usage
                exit 1
                ;;
        esac
    done
    
    # Enable verbose output if requested
    if [ "$verbose" = true ]; then
        set -x
    fi
    
    # Show system information
    show_system_info
    echo ""
    
    # Check prerequisites
    check_prerequisites
    echo ""
    
    # Exit if only checking
    if [ "$check_only" = true ]; then
        print_success "Prerequisites check completed"
        exit 0
    fi
    
    # Setup environment
    setup_environment
    echo ""
    
    # Exit if only setting up
    if [ "$setup_only" = true ]; then
        print_success "Environment setup completed"
        exit 0
    fi
    
    # Default to quick tests if no type specified
    if [ -z "$test_type" ]; then
        test_type="quick"
        print_warning "No test type specified, running quick tests"
    fi
    
    # Run tests
    echo ""
    if [ "$test_type" = "all" ]; then
        run_all_tests
    else
        case $test_type in
            "basic")
                run_test_type "basic" "Basic Integration Tests"
                ;;
            "containers")
                run_test_type "containers" "Testcontainers Tests"
                ;;
            "quick")
                run_test_type "quick" "Quick Integration Tests"
                ;;
            "performance")
                run_test_type "performance" "Performance Tests"
                ;;
            "coverage")
                run_test_type "coverage" "Coverage Tests"
                ;;
        esac
    fi
    
    echo ""
    print_success "Test execution completed!"
}

# Run main function with all arguments
main "$@"