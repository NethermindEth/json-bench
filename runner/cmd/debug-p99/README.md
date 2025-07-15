# P99 Debugging Tool

A command-line tool for debugging P99 latency metrics in JSON-RPC benchmark runs.

## Purpose

This tool helps diagnose issues with P99 metrics collection and storage by:
- Querying the database for P99 metrics associated with a specific run
- Checking for K6 raw results files
- Displaying all latency-related metrics from the benchmark_metrics table
- Providing diagnostic information and recommendations

## Building

```bash
./build.sh
```

Or manually:
```bash
cd ../../
go build -o cmd/debug-p99/debug-p99 ./cmd/debug-p99
```

## Usage

```bash
./debug-p99 -run-id <RUN_ID> [-storage-config <CONFIG_FILE>] [-verbose]
```

### Arguments

- `-run-id` (required): The UUID of the benchmark run to debug
- `-storage-config` (optional): Path to storage configuration file. If not provided, uses default PostgreSQL settings
- `-verbose` (optional): Enable verbose logging output

## Example

```bash
# Debug a specific run with default database settings
./debug-p99 -run-id "123e4567-e89b-12d3-a456-426614174000"

# Debug with custom storage configuration
./debug-p99 -run-id "123e4567-e89b-12d3-a456-426614174000" -storage-config ../../config/storage.yaml

# Debug with verbose output
./debug-p99 -run-id "123e4567-e89b-12d3-a456-426614174000" -verbose
```

## Output Sections

The tool provides information in the following sections:

### 1. Run Information
Basic information about the benchmark run including:
- Run ID, test name, timestamp
- Git commit and branch
- Total requests and success rate
- Average and P95 latency
- Result file path

### 2. P99 Metrics from Database
All `latency_p99` metrics stored in the benchmark_metrics table for this run.

### 3. K6 Raw Results
Attempts to locate and parse K6 results files to find P99 metrics in the raw data.

### 4. All Latency P99 Metrics
Broader search for any metrics that might contain P99 data, including:
- Metrics with "p99" or "P99" in the name
- HTTP request duration metrics
- General latency metrics

### 5. Summary
Diagnostic summary with recommendations if no P99 metrics are found.

## Database Requirements

The tool expects the following tables:
- `benchmark_runs`: Contains run metadata
- `benchmark_metrics`: Contains time-series metrics data

## Troubleshooting

If the tool reports no P99 metrics found:

1. **Check K6 Script Configuration**: Ensure the K6 script is configured to collect P99 metrics
2. **Verify Metrics Pipeline**: Check that metrics are being properly collected and stored
3. **Review Run Logs**: Look for errors during the benchmark execution
4. **Database Connection**: Verify the database connection settings in your storage configuration