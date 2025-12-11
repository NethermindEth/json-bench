package api

import (
	"database/sql"

	"github.com/jsonrpc-bench/runner/types"
)

// getClientMetricsForRun retrieves client metrics from the benchmark_metrics table
func getClientMetricsForRun(db *sql.DB, runID string) (map[string]*types.ClientMetrics, error) {
	// Query to aggregate client metrics
	query := `
		SELECT 
			client,
			COUNT(DISTINCT CASE WHEN method != 'all' THEN method END) as method_count,
			MAX(CASE WHEN metric_name = 'total_requests' AND method = 'all' THEN value ELSE 0 END) as total_requests,
			MAX(CASE WHEN metric_name = 'error_rate' AND method = 'all' THEN value ELSE 0 END) as error_rate,
			MAX(CASE WHEN metric_name = 'success_rate' AND method = 'all' THEN value ELSE 0 END) as success_rate,
			MAX(CASE WHEN metric_name = 'throughput' AND method = 'all' THEN value ELSE 0 END) as throughput,
			MAX(CASE WHEN metric_name = 'latency_avg' AND method = 'all' THEN value ELSE 0 END) as avg_latency,
			MAX(CASE WHEN metric_name = 'latency_min' AND method = 'all' THEN value ELSE 0 END) as min_latency,
			MAX(CASE WHEN metric_name = 'latency_max' AND method = 'all' THEN value ELSE 0 END) as max_latency,
			MAX(CASE WHEN metric_name = 'latency_p50' AND method = 'all' THEN value ELSE 0 END) as p50_latency,
			MAX(CASE WHEN metric_name = 'latency_p90' AND method = 'all' THEN value ELSE 0 END) as p90_latency,
			MAX(CASE WHEN metric_name = 'latency_p95' AND method = 'all' THEN value ELSE 0 END) as p95_latency,
			MAX(CASE WHEN metric_name = 'latency_p99' AND method = 'all' THEN value ELSE 0 END) as p99_latency
		FROM benchmark_metrics
		WHERE run_id = $1
		GROUP BY client
		ORDER BY client`

	rows, err := db.Query(query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clientMetrics := make(map[string]*types.ClientMetrics)

	for rows.Next() {
		var client string
		var methodCount int
		var totalRequests, errorRate, successRate, throughput, avgLatency, minLatency, maxLatency float64
		var p50Latency, p90Latency, p95Latency, p99Latency float64

		err := rows.Scan(
			&client, &methodCount, &totalRequests, &errorRate, &successRate, &throughput,
			&avgLatency, &minLatency, &maxLatency,
			&p50Latency, &p90Latency, &p95Latency, &p99Latency,
		)
		if err != nil {
			continue
		}

		// Convert to ClientMetrics structure
		cm := &types.ClientMetrics{
			TotalRequests: int64(totalRequests),
			TotalErrors:   int64(totalRequests * errorRate / 100.0),
			ErrorRate:     errorRate,
			SuccessRate:   successRate,
			Latency: types.MetricSummary{
				Avg:         avgLatency,
				Min:         minLatency,
				Max:         maxLatency,
				P50:         p50Latency,
				P90:         p90Latency,
				P95:         p95Latency,
				P99:         p99Latency,
				Throughput:  throughput,
				SuccessRate: successRate,
			},
			Methods: make(map[string]types.MetricSummary),
		}

		// Now get method-specific metrics for this client
		methodQuery := `
			SELECT 
				method,
				MAX(CASE WHEN metric_name = 'latency_avg' THEN value ELSE 0 END) as avg,
				MAX(CASE WHEN metric_name = 'latency_min' THEN value ELSE 0 END) as min,
				MAX(CASE WHEN metric_name = 'latency_max' THEN value ELSE 0 END) as max,
				MAX(CASE WHEN metric_name = 'latency_p50' THEN value ELSE 0 END) as p50,
				MAX(CASE WHEN metric_name = 'latency_p90' THEN value ELSE 0 END) as p90,
				MAX(CASE WHEN metric_name = 'latency_p95' THEN value ELSE 0 END) as p95,
				MAX(CASE WHEN metric_name = 'latency_p99' THEN value ELSE 0 END) as p99,
				MAX(CASE WHEN metric_name = 'error_rate' THEN value ELSE 0 END) as error_rate,
				MAX(CASE WHEN metric_name = 'throughput' THEN value ELSE 0 END) as throughput
			FROM benchmark_metrics
			WHERE run_id = $1 AND client = $2 AND method != 'all'
			GROUP BY method`

		methodRows, err := db.Query(methodQuery, runID, client)
		if err == nil {
			defer methodRows.Close()
			for methodRows.Next() {
				var method string
				var mm types.MetricSummary
				err := methodRows.Scan(
					&method,
					&mm.Avg, &mm.Min, &mm.Max,
					&mm.P50, &mm.P90, &mm.P95, &mm.P99,
					&mm.ErrorRate, &mm.Throughput,
				)
				if err == nil {
					mm.Count = int64(mm.Throughput) // Approximate count from throughput
					mm.SuccessRate = 100 - mm.ErrorRate
					cm.Methods[method] = mm
				}
			}
		}

		clientMetrics[client] = cm
	}

	return clientMetrics, nil
}
