package storage

// Grafana-optimized table schemas
const GrafanaRunsTable = `
CREATE TABLE IF NOT EXISTS benchmark_runs (
    id VARCHAR(255) PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL,
    git_commit VARCHAR(40),
    git_branch VARCHAR(255),
    test_name VARCHAR(255),
    description TEXT,
    config_hash VARCHAR(64),
    result_path TEXT,
    duration INTERVAL,
    total_requests BIGINT,
    success_rate DOUBLE PRECISION,
    avg_latency DOUBLE PRECISION,
    p95_latency DOUBLE PRECISION,
    clients JSONB,
    methods JSONB,
    tags JSONB,
    is_baseline BOOLEAN DEFAULT FALSE,
    baseline_name VARCHAR(255),
    error_rate_semantics VARCHAR(64),
    metadata JSONB
);`

// AddErrorRateSemantics backfills the column on databases created before runs
// recorded what their error rate counted.
const AddErrorRateSemantics = `
ALTER TABLE benchmark_runs ADD COLUMN IF NOT EXISTS error_rate_semantics VARCHAR(64);`

const GrafanaMetricsTable = `
CREATE TABLE IF NOT EXISTS benchmark_metrics (
    time TIMESTAMPTZ NOT NULL,
    run_id VARCHAR(255) NOT NULL,
    client VARCHAR(255) NOT NULL,
    method VARCHAR(255) NOT NULL,
    metric_name VARCHAR(255) NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    tags JSONB,
    PRIMARY KEY (time, run_id, client, method, metric_name)
);`

// Create hypertable for time-series data (if using TimescaleDB)
const CreateHypertable = `SELECT create_hypertable('benchmark_metrics', 'time', if_not_exists => TRUE);`
