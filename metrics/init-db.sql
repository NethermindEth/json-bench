-- JSON-RPC Benchmark Database Schema
-- This script initializes the database with required tables and indexes

-- Create Grafana database if it doesn't exist
CREATE EXTENSION IF NOT EXISTS dblink;

DO $$
BEGIN
    PERFORM dblink_exec('', 'CREATE DATABASE grafana');
EXCEPTION
    WHEN duplicate_database THEN
        RAISE NOTICE 'Database "grafana" already exists, skipping creation.';
END $$;

-- Create user for Grafana if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'grafana') THEN
        CREATE USER grafana WITH PASSWORD 'grafana';
    END IF;
END
$$;

-- Grant privileges to grafana user in the grafana database
DO $$
BEGIN
    PERFORM dblink_exec('dbname=grafana', 'GRANT ALL PRIVILEGES ON SCHEMA public TO grafana');
    PERFORM dblink_exec('dbname=grafana', 'GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO grafana');
    PERFORM dblink_exec('dbname=grafana', 'GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO grafana');
    PERFORM dblink_exec('dbname=grafana', 'GRANT ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public TO grafana');
    PERFORM dblink_exec('dbname=grafana', 'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO grafana');
    PERFORM dblink_exec('dbname=grafana', 'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO grafana');
    PERFORM dblink_exec('dbname=grafana', 'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON FUNCTIONS TO grafana');
EXCEPTION
    WHEN OTHERS THEN
        RAISE NOTICE 'Error granting permissions to grafana user: %', SQLERRM;
END $$;

-- Switch to main database context
\c jsonrpc_bench;

-- Create extensions if they don't exist
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Create historic_runs table if it doesn't exist
CREATE TABLE IF NOT EXISTS historic_runs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    test_name VARCHAR(255) NOT NULL,
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    git_commit VARCHAR(255),
    git_branch VARCHAR(255),
    duration VARCHAR(255),
    total_requests INTEGER,
    total_errors INTEGER,
    overall_error_rate DECIMAL(5,2),
    avg_latency_ms DECIMAL(10,3),
    p95_latency_ms DECIMAL(10,3),
    p99_latency_ms DECIMAL(10,3),
    best_client VARCHAR(255),
    performance_scores JSONB,
    client_metrics JSONB,
    error_details JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create baselines table if it doesn't exist
CREATE TABLE IF NOT EXISTS baselines (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    test_name VARCHAR(255) NOT NULL,
    run_id UUID NOT NULL REFERENCES historic_runs(id),
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(name, test_name)
);

-- Create regressions table if it doesn't exist
CREATE TABLE IF NOT EXISTS regressions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    run_id UUID NOT NULL REFERENCES historic_runs(id),
    baseline_id UUID REFERENCES baselines(id),
    client VARCHAR(255) NOT NULL,
    metric VARCHAR(255) NOT NULL,
    current_value DECIMAL(10,3),
    baseline_value DECIMAL(10,3),
    deviation DECIMAL(10,3),
    severity VARCHAR(50) NOT NULL CHECK (severity IN ('minor', 'major', 'critical')),
    status VARCHAR(50) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'resolved', 'ignored')),
    detected_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    resolved_at TIMESTAMP WITH TIME ZONE,
    notes TEXT
);

-- Create performance_alerts table if it doesn't exist
CREATE TABLE IF NOT EXISTS performance_alerts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    run_id UUID NOT NULL REFERENCES historic_runs(id),
    alert_type VARCHAR(50) NOT NULL,
    severity VARCHAR(50) NOT NULL,
    message TEXT NOT NULL,
    details JSONB,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    resolved_at TIMESTAMP WITH TIME ZONE
);

-- Create test_configurations table if it doesn't exist
CREATE TABLE IF NOT EXISTS test_configurations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    test_name VARCHAR(255) NOT NULL,
    config_hash VARCHAR(255) NOT NULL,
    configuration JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(test_name, config_hash)
);

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_historic_runs_test_name ON historic_runs(test_name);
CREATE INDEX IF NOT EXISTS idx_historic_runs_timestamp ON historic_runs(timestamp);
CREATE INDEX IF NOT EXISTS idx_historic_runs_git_commit ON historic_runs(git_commit);
CREATE INDEX IF NOT EXISTS idx_historic_runs_git_branch ON historic_runs(git_branch);
CREATE INDEX IF NOT EXISTS idx_historic_runs_best_client ON historic_runs(best_client);
CREATE INDEX IF NOT EXISTS idx_historic_runs_composite ON historic_runs(test_name, timestamp);

CREATE INDEX IF NOT EXISTS idx_baselines_test_name ON baselines(test_name);
CREATE INDEX IF NOT EXISTS idx_baselines_name ON baselines(name);
CREATE INDEX IF NOT EXISTS idx_baselines_run_id ON baselines(run_id);

CREATE INDEX IF NOT EXISTS idx_regressions_run_id ON regressions(run_id);
CREATE INDEX IF NOT EXISTS idx_regressions_baseline_id ON regressions(baseline_id);
CREATE INDEX IF NOT EXISTS idx_regressions_client ON regressions(client);
CREATE INDEX IF NOT EXISTS idx_regressions_metric ON regressions(metric);
CREATE INDEX IF NOT EXISTS idx_regressions_severity ON regressions(severity);
CREATE INDEX IF NOT EXISTS idx_regressions_status ON regressions(status);
CREATE INDEX IF NOT EXISTS idx_regressions_detected_at ON regressions(detected_at);

CREATE INDEX IF NOT EXISTS idx_performance_alerts_run_id ON performance_alerts(run_id);
CREATE INDEX IF NOT EXISTS idx_performance_alerts_alert_type ON performance_alerts(alert_type);
CREATE INDEX IF NOT EXISTS idx_performance_alerts_severity ON performance_alerts(severity);
CREATE INDEX IF NOT EXISTS idx_performance_alerts_status ON performance_alerts(status);
CREATE INDEX IF NOT EXISTS idx_performance_alerts_created_at ON performance_alerts(created_at);

CREATE INDEX IF NOT EXISTS idx_test_configurations_test_name ON test_configurations(test_name);
CREATE INDEX IF NOT EXISTS idx_test_configurations_config_hash ON test_configurations(config_hash);

-- Create GIN indexes for JSONB columns
CREATE INDEX IF NOT EXISTS idx_historic_runs_performance_scores_gin ON historic_runs USING gin(performance_scores);
CREATE INDEX IF NOT EXISTS idx_historic_runs_client_metrics_gin ON historic_runs USING gin(client_metrics);
CREATE INDEX IF NOT EXISTS idx_historic_runs_error_details_gin ON historic_runs USING gin(error_details);
CREATE INDEX IF NOT EXISTS idx_performance_alerts_details_gin ON performance_alerts USING gin(details);
CREATE INDEX IF NOT EXISTS idx_test_configurations_configuration_gin ON test_configurations USING gin(configuration);

-- Create function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create trigger to automatically update updated_at
DROP TRIGGER IF EXISTS update_historic_runs_updated_at ON historic_runs;
CREATE TRIGGER update_historic_runs_updated_at
    BEFORE UPDATE ON historic_runs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Create views for common queries
CREATE OR REPLACE VIEW latest_runs AS
SELECT 
    hr.*,
    RANK() OVER (PARTITION BY test_name ORDER BY timestamp DESC) as rank
FROM historic_runs hr;

CREATE OR REPLACE VIEW regression_summary AS
SELECT 
    r.id,
    r.detected_at,
    r.severity,
    r.status,
    r.client,
    r.metric,
    r.deviation,
    hr.test_name,
    hr.git_commit,
    hr.git_branch,
    b.name as baseline_name
FROM regressions r
JOIN historic_runs hr ON r.run_id = hr.id
LEFT JOIN baselines b ON r.baseline_id = b.id;

CREATE OR REPLACE VIEW performance_trends AS
SELECT 
    test_name,
    timestamp,
    avg_latency_ms,
    p95_latency_ms,
    p99_latency_ms,
    overall_error_rate,
    LAG(avg_latency_ms) OVER (PARTITION BY test_name ORDER BY timestamp) as prev_avg_latency,
    LAG(p95_latency_ms) OVER (PARTITION BY test_name ORDER BY timestamp) as prev_p95_latency,
    LAG(overall_error_rate) OVER (PARTITION BY test_name ORDER BY timestamp) as prev_error_rate
FROM historic_runs
ORDER BY test_name, timestamp;

-- Insert some sample data if tables are empty
INSERT INTO historic_runs (test_name, git_commit, git_branch, total_requests, total_errors, overall_error_rate, avg_latency_ms, p95_latency_ms, p99_latency_ms, best_client, performance_scores)
SELECT 
    'mixed',
    'abc123',
    'main',
    1000,
    5,
    0.5,
    150.0,
    250.0,
    350.0,
    'geth',
    '{"geth": {"avg_latency": 150, "p95_latency": 250}, "nethermind": {"avg_latency": 180, "p95_latency": 280}}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM historic_runs);

-- Create a sample baseline
INSERT INTO baselines (name, test_name, run_id, description)
SELECT 
    'stable',
    'mixed',
    hr.id,
    'Stable baseline for mixed workload tests'
FROM historic_runs hr
WHERE hr.test_name = 'mixed'
AND NOT EXISTS (SELECT 1 FROM baselines WHERE name = 'stable' AND test_name = 'mixed')
LIMIT 1;

-- Grant necessary permissions
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO postgres;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO postgres;
GRANT ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public TO postgres;

-- Create read-only user for Grafana
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'grafana_reader') THEN
        CREATE USER grafana_reader WITH PASSWORD 'grafana_reader';
    END IF;
END
$$;

GRANT CONNECT ON DATABASE jsonrpc_bench TO grafana_reader;
GRANT USAGE ON SCHEMA public TO grafana_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO grafana_reader;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO grafana_reader;

-- Ensure future tables are also accessible
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO grafana_reader;

-- Create a function to clean up old data
CREATE OR REPLACE FUNCTION cleanup_old_data(days_to_keep INTEGER DEFAULT 90)
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    -- Delete old historic runs (keeps baselines)
    DELETE FROM historic_runs 
    WHERE timestamp < NOW() - INTERVAL '1 day' * days_to_keep
    AND id NOT IN (SELECT run_id FROM baselines);
    
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    
    -- Delete resolved regressions older than 30 days
    DELETE FROM regressions 
    WHERE status = 'resolved' 
    AND resolved_at < NOW() - INTERVAL '30 days';
    
    -- Delete old performance alerts
    DELETE FROM performance_alerts 
    WHERE created_at < NOW() - INTERVAL '1 day' * days_to_keep;
    
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

-- Create a function to calculate performance scores
CREATE OR REPLACE FUNCTION calculate_performance_score(
    avg_latency DECIMAL,
    p95_latency DECIMAL,
    error_rate DECIMAL
) RETURNS DECIMAL AS $$
BEGIN
    -- Simple scoring algorithm: lower is better
    -- Score = (1000 / avg_latency) * (1000 / p95_latency) * (100 - error_rate)
    RETURN GREATEST(0, 
        (1000.0 / GREATEST(avg_latency, 1)) * 
        (1000.0 / GREATEST(p95_latency, 1)) * 
        (100.0 - LEAST(error_rate, 100.0))
    );
END;
$$ LANGUAGE plpgsql;

-- Log successful initialization
INSERT INTO performance_alerts (run_id, alert_type, severity, message, details, status)
SELECT 
    hr.id,
    'system',
    'info',
    'Database initialized successfully',
    jsonb_build_object('component', 'database', 'action', 'initialization', 'timestamp', NOW()),
    'resolved'
FROM historic_runs hr
LIMIT 1
ON CONFLICT DO NOTHING;