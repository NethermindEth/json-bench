-- Debug P99 metrics for a specific run
-- Usage: psql -d your_database -f debug_p99_direct.sql -v run_id='20250708-114911-57172b6'

-- Show all p99 metrics for the run
SELECT 
    run_id,
    method,
    metric_name,
    value,
    time
FROM benchmark_metrics
WHERE run_id = :'run_id'
  AND metric_name = 'latency_p99'
ORDER BY method, time
LIMIT 20;

-- Count p99 metrics by method
SELECT 
    method,
    COUNT(*) as count,
    MIN(value) as min_p99,
    MAX(value) as max_p99,
    AVG(value) as avg_p99
FROM benchmark_metrics
WHERE run_id = :'run_id'
  AND metric_name = 'latency_p99'
GROUP BY method;

-- Show all unique metric names for this run
SELECT DISTINCT metric_name
FROM benchmark_metrics
WHERE run_id = :'run_id'
ORDER BY metric_name;

-- Check if there are any NULL values
SELECT 
    method,
    metric_name,
    COUNT(*) as total_rows,
    COUNT(value) as non_null_values,
    COUNT(*) - COUNT(value) as null_values
FROM benchmark_metrics
WHERE run_id = :'run_id'
  AND metric_name LIKE 'latency%'
GROUP BY method, metric_name
ORDER BY method, metric_name;