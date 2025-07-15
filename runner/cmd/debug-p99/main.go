package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

func main() {
	var (
		runID         = flag.String("run-id", "", "Run ID to debug p99 metrics for")
		storageConfig = flag.String("storage-config", "", "Path to storage configuration file")
		verbose       = flag.Bool("verbose", false, "Enable verbose output")
	)
	flag.Parse()

	// Setup logging
	log := logrus.New()
	if *verbose {
		log.SetLevel(logrus.DebugLevel)
	} else {
		log.SetLevel(logrus.InfoLevel)
	}

	if *runID == "" {
		log.Fatal("run-id is required")
	}

	// Load storage configuration
	cfg, err := config.LoadStorageConfig(*storageConfig, log)
	if err != nil {
		log.WithError(err).Fatal("Failed to load storage configuration")
	}

	// Debug the p99 metrics for the run
	if err := debugP99ForRun(*runID, cfg, log); err != nil {
		log.WithError(err).Fatal("Failed to debug p99 metrics")
	}
}

func debugP99ForRun(runID string, cfg *config.StorageConfig, log logrus.FieldLogger) error {
	// Connect to database
	db, err := sql.Open("postgres", cfg.PostgreSQL.ConnectionString())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	log.WithField("run_id", runID).Info("Debugging p99 metrics for run")

	// 1. Get run information
	log.Info("=== Run Information ===")
	run, err := getRunInfo(db, runID, cfg.PostgreSQL.RunsTable)
	if err != nil {
		return fmt.Errorf("failed to get run info: %w", err)
	}

	fmt.Printf("Run ID: %s\n", run.ID)
	fmt.Printf("Test Name: %s\n", run.TestName)
	fmt.Printf("Timestamp: %s\n", run.Timestamp.Format(time.RFC3339))
	fmt.Printf("Git Commit: %s\n", run.GitCommit)
	fmt.Printf("Git Branch: %s\n", run.GitBranch)
	fmt.Printf("Total Requests: %d\n", run.TotalRequests)
	fmt.Printf("Success Rate: %.2f%%\n", run.SuccessRate)
	fmt.Printf("Avg Latency: %.2f ms\n", run.AvgLatency)
	fmt.Printf("P95 Latency: %.2f ms\n", run.P95Latency)
	fmt.Printf("Result Path: %s\n", run.ResultPath)
	fmt.Println()

	// 2. Query p99 metrics from benchmark_metrics table
	log.Info("=== P99 Metrics from Database ===")
	p99Metrics, err := queryP99Metrics(db, runID, cfg.PostgreSQL.MetricsTable)
	if err != nil {
		return fmt.Errorf("failed to query p99 metrics: %w", err)
	}

	if len(p99Metrics) == 0 {
		fmt.Println("No p99 metrics found in database for this run")
	} else {
		fmt.Printf("Found %d p99 metric entries\n", len(p99Metrics))
		for _, metric := range p99Metrics {
			fmt.Printf("  Client: %s, Method: %s, Value: %.2f ms, Time: %s\n",
				metric.Client, metric.Method, metric.Value, metric.Time.Format(time.RFC3339))
		}
	}
	fmt.Println()

	// 3. Check for K6 raw results file
	log.Info("=== K6 Raw Results ===")
	k6File, k6Data, err := findK6ResultsFile(run.ResultPath, cfg.HistoricPath)
	if err != nil {
		log.WithError(err).Warn("Failed to find or read K6 results file")
		fmt.Printf("K6 results file not found or error reading: %v\n", err)
	} else {
		fmt.Printf("K6 results file: %s\n", k6File)

		// Parse and display relevant K6 metrics
		if k6Data != nil {
			displayK6Metrics(k6Data)
		}
	}
	fmt.Println()

	// 4. Check for any latency_p99 metrics
	log.Info("=== All Latency P99 Metrics ===")
	allP99, err := queryAllP99Metrics(db, runID, cfg.PostgreSQL.MetricsTable)
	if err != nil {
		return fmt.Errorf("failed to query all p99 metrics: %w", err)
	}

	if len(allP99) == 0 {
		fmt.Println("No latency_p99 metrics found for this run")
	} else {
		fmt.Printf("Found %d latency_p99 entries:\n", len(allP99))
		for _, metric := range allP99 {
			fmt.Printf("  Time: %s, Client: %s, Method: %s, Metric: %s, Value: %.2f\n",
				metric.Time.Format(time.RFC3339), metric.Client, metric.Method,
				metric.MetricName, metric.Value)
		}
	}

	// 5. Summary and recommendations
	log.Info("=== Summary ===")
	fmt.Println("\nSummary:")
	if len(p99Metrics) == 0 && len(allP99) == 0 {
		fmt.Println("- No p99 metrics found in the database")
		fmt.Println("- This could indicate:")
		fmt.Println("  1. The benchmark didn't collect p99 metrics")
		fmt.Println("  2. The metrics weren't properly stored in the database")
		fmt.Println("  3. The run failed before metrics collection")
		fmt.Println("\nRecommendations:")
		fmt.Println("- Check the K6 script configuration for p99 metric collection")
		fmt.Println("- Verify the metrics collection and storage pipeline")
		fmt.Println("- Check logs for any errors during the benchmark run")
	} else {
		fmt.Printf("- Found %d p99 metric entries\n", len(p99Metrics)+len(allP99))
		fmt.Println("- P99 metrics are being collected and stored")
	}

	return nil
}

func getRunInfo(db *sql.DB, runID, tableName string) (*types.HistoricRun, error) {
	if tableName == "" {
		tableName = "benchmark_runs"
	}

	query := fmt.Sprintf(`
		SELECT id, timestamp, git_commit, git_branch, test_name, 
		       COALESCE(description, ''), COALESCE(config_hash, ''), 
		       COALESCE(result_path, ''), COALESCE(duration::text, '0'),
		       COALESCE(total_requests, 0), COALESCE(success_rate, 0),
		       COALESCE(avg_latency, 0), COALESCE(p95_latency, 0),
		       COALESCE(clients::text, '[]'), COALESCE(methods::text, '[]'),
		       COALESCE(tags::text, '[]'), COALESCE(is_baseline, false),
		       COALESCE(baseline_name, '')
		FROM %s WHERE id = $1`, tableName)

	var run types.HistoricRun
	var clientsJSON, methodsJSON, tagsJSON string

	err := db.QueryRow(query, runID).Scan(
		&run.ID, &run.Timestamp, &run.GitCommit, &run.GitBranch,
		&run.TestName, &run.Description, &run.ConfigHash, &run.ResultPath,
		&run.Duration, &run.TotalRequests, &run.SuccessRate,
		&run.AvgLatency, &run.P95Latency, &clientsJSON, &methodsJSON,
		&tagsJSON, &run.IsBaseline, &run.BaselineName,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("run not found: %s", runID)
		}
		return nil, err
	}

	// Parse JSON fields
	json.Unmarshal([]byte(clientsJSON), &run.Clients)
	json.Unmarshal([]byte(methodsJSON), &run.Methods)
	json.Unmarshal([]byte(tagsJSON), &run.Tags)

	return &run, nil
}

func queryP99Metrics(db *sql.DB, runID, tableName string) ([]types.TimeSeriesMetric, error) {
	if tableName == "" {
		tableName = "benchmark_metrics"
	}

	query := fmt.Sprintf(`
		SELECT time, run_id, client, method, metric_name, value, COALESCE(tags::text, '{}')
		FROM %s 
		WHERE run_id = $1 AND metric_name = 'latency_p99'
		ORDER BY time`, tableName)

	rows, err := db.Query(query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []types.TimeSeriesMetric
	for rows.Next() {
		var metric types.TimeSeriesMetric
		var tagsJSON string

		err := rows.Scan(
			&metric.Time, &metric.RunID, &metric.Client,
			&metric.Method, &metric.MetricName, &metric.Value, &tagsJSON,
		)
		if err != nil {
			return nil, err
		}

		if tagsJSON != "" && tagsJSON != "{}" {
			json.Unmarshal([]byte(tagsJSON), &metric.Tags)
		}

		metrics = append(metrics, metric)
	}

	return metrics, rows.Err()
}

func queryAllP99Metrics(db *sql.DB, runID, tableName string) ([]types.TimeSeriesMetric, error) {
	if tableName == "" {
		tableName = "benchmark_metrics"
	}

	// Query for any metric that might contain p99 data
	query := fmt.Sprintf(`
		SELECT time, run_id, client, method, metric_name, value, COALESCE(tags::text, '{}')
		FROM %s 
		WHERE run_id = $1 AND (
			metric_name LIKE '%%p99%%' OR 
			metric_name LIKE '%%P99%%' OR
			metric_name = 'http_req_duration' OR
			metric_name LIKE '%%latency%%'
		)
		ORDER BY time, metric_name`, tableName)

	rows, err := db.Query(query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []types.TimeSeriesMetric
	for rows.Next() {
		var metric types.TimeSeriesMetric
		var tagsJSON string

		err := rows.Scan(
			&metric.Time, &metric.RunID, &metric.Client,
			&metric.Method, &metric.MetricName, &metric.Value, &tagsJSON,
		)
		if err != nil {
			return nil, err
		}

		if tagsJSON != "" && tagsJSON != "{}" {
			json.Unmarshal([]byte(tagsJSON), &metric.Tags)
		}

		metrics = append(metrics, metric)
	}

	return metrics, rows.Err()
}

func findK6ResultsFile(resultPath, historicPath string) (string, map[string]interface{}, error) {
	// Try different possible locations for the K6 results file
	possiblePaths := []string{
		resultPath,
		filepath.Join(historicPath, resultPath),
		filepath.Join(historicPath, filepath.Base(resultPath)),
		strings.Replace(resultPath, ".json", "_summary.json", 1),
		strings.Replace(resultPath, ".json", "_raw.json", 1),
	}

	// Also check for K6 specific output files
	if dir := filepath.Dir(resultPath); dir != "" && dir != "." {
		possiblePaths = append(possiblePaths,
			filepath.Join(dir, "k6_results.json"),
			filepath.Join(dir, "summary.json"),
			filepath.Join(dir, "raw_metrics.json"),
		)
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			// Found the file, try to read it
			data, err := os.ReadFile(path)
			if err != nil {
				return path, nil, fmt.Errorf("found file but failed to read: %w", err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				return path, nil, fmt.Errorf("found file but failed to parse JSON: %w", err)
			}

			return path, result, nil
		}
	}

	return "", nil, fmt.Errorf("K6 results file not found in any expected location")
}

func displayK6Metrics(data map[string]interface{}) {
	fmt.Println("K6 Metrics Summary:")

	// Look for metrics section
	if metrics, ok := data["metrics"].(map[string]interface{}); ok {
		// Check for http_req_duration metrics
		if httpDuration, ok := metrics["http_req_duration"].(map[string]interface{}); ok {
			fmt.Println("  HTTP Request Duration:")
			if values, ok := httpDuration["values"].(map[string]interface{}); ok {
				for key, value := range values {
					if strings.Contains(strings.ToLower(key), "p99") {
						fmt.Printf("    %s: %.2f ms\n", key, toFloat64(value))
					}
				}
			}
		}

		// Check for other relevant metrics
		for metricName, metricData := range metrics {
			if md, ok := metricData.(map[string]interface{}); ok {
				if values, ok := md["values"].(map[string]interface{}); ok {
					for key, value := range values {
						if strings.Contains(strings.ToLower(key), "p99") {
							fmt.Printf("  %s - %s: %.2f\n", metricName, key, toFloat64(value))
						}
					}
				}
			}
		}
	}

	// Look for summary section
	if summary, ok := data["summary"].(map[string]interface{}); ok {
		fmt.Println("  Summary Statistics:")
		if metrics, ok := summary["metrics"].(map[string]interface{}); ok {
			for metricName, metricData := range metrics {
				if strings.Contains(strings.ToLower(metricName), "duration") {
					fmt.Printf("    %s:\n", metricName)
					if md, ok := metricData.(map[string]interface{}); ok {
						for key, value := range md {
							if strings.Contains(strings.ToLower(key), "p99") {
								fmt.Printf("      %s: %.2f\n", key, toFloat64(value))
							}
						}
					}
				}
			}
		}
	}
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		// Try to parse string as float
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0.0
	}
}
