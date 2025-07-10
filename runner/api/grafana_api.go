package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/storage"
)

// GrafanaAPI provides Grafana SimpleJSON datasource compatibility
type GrafanaAPI interface {
	Start(ctx context.Context) error
	Stop() error

	// SimpleJSON datasource endpoints
	HandleGrafanaTestConnection(w http.ResponseWriter, r *http.Request)
	HandleGrafanaSearch(w http.ResponseWriter, r *http.Request)
	HandleGrafanaQuery(w http.ResponseWriter, r *http.Request)
	HandleGrafanaAnnotations(w http.ResponseWriter, r *http.Request)
	HandleGrafanaMetrics(w http.ResponseWriter, r *http.Request)

	// Helper methods for data formatting
	FormatGrafanaTimeSeries(data []TimeSeriesDataPoint, target string) GrafanaTimeSeries
	FormatGrafanaTable(data []TableRow, columns []TableColumn) GrafanaTable
}

// grafanaAPI implements GrafanaAPI interface
type grafanaAPI struct {
	storage storage.HistoricStorage
	db      *sql.DB
	log     logrus.FieldLogger
}

// NewGrafanaAPI creates a new Grafana API instance
func NewGrafanaAPI(
	historicStorage storage.HistoricStorage,
	db *sql.DB,
	log logrus.FieldLogger,
) GrafanaAPI {
	return &grafanaAPI{
		storage: historicStorage,
		db:      db,
		log:     log.WithField("component", "grafana-api"),
	}
}

// Start initializes the Grafana API
func (g *grafanaAPI) Start(ctx context.Context) error {
	g.log.Info("Starting Grafana API")
	return nil
}

// Stop shuts down the Grafana API
func (g *grafanaAPI) Stop() error {
	g.log.Info("Stopping Grafana API")
	return nil
}

// SimpleJSON Datasource Endpoints

// HandleGrafanaTestConnection handles the test connection endpoint
func (g *grafanaAPI) HandleGrafanaTestConnection(w http.ResponseWriter, r *http.Request) {
	g.log.Debug("Handling Grafana test connection")

	// Test database connection
	if err := g.db.Ping(); err != nil {
		g.log.WithError(err).Error("Database connection failed")
		g.writeGrafanaErrorResponse(w, http.StatusServiceUnavailable, "Database connection failed")
		return
	}

	response := map[string]string{
		"status":  "success",
		"message": "Data source is working",
	}

	g.writeGrafanaResponse(w, http.StatusOK, response)
}

// HandleGrafanaSearch handles search requests for metrics and dimensions
func (g *grafanaAPI) HandleGrafanaSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g.log.Debug("Handling Grafana search request")

	var req GrafanaSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.log.WithError(err).Error("Failed to decode search request")
		g.writeGrafanaErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	g.log.WithField("target", req.Target).Debug("Search target")

	// Get available metrics based on search target
	metrics, err := g.getAvailableMetrics(ctx, req.Target)
	if err != nil {
		g.log.WithError(err).Error("Failed to get available metrics")
		g.writeGrafanaErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve metrics")
		return
	}

	g.writeGrafanaResponse(w, http.StatusOK, metrics)
}

// HandleGrafanaQuery handles query requests for time series data
func (g *grafanaAPI) HandleGrafanaQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g.log.Debug("Handling Grafana query request")

	var req GrafanaQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.log.WithError(err).Error("Failed to decode query request")
		g.writeGrafanaErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	g.log.WithFields(logrus.Fields{
		"from":    req.Range.From,
		"to":      req.Range.To,
		"targets": len(req.Targets),
	}).Debug("Query parameters")

	// Parse time range
	fromTime, err := g.parseGrafanaTime(req.Range.From)
	if err != nil {
		g.log.WithError(err).Error("Failed to parse from time")
		g.writeGrafanaErrorResponse(w, http.StatusBadRequest, "Invalid from time format")
		return
	}

	toTime, err := g.parseGrafanaTime(req.Range.To)
	if err != nil {
		g.log.WithError(err).Error("Failed to parse to time")
		g.writeGrafanaErrorResponse(w, http.StatusBadRequest, "Invalid to time format")
		return
	}

	var response []interface{}

	// Process each target
	for _, target := range req.Targets {
		if target.Target == "" {
			continue
		}

		g.log.WithField("target", target.Target).Debug("Processing target")

		// Parse target to extract metric information
		metricInfo := g.parseMetricTarget(target.Target)
		if metricInfo == nil {
			g.log.WithField("target", target.Target).Warn("Failed to parse metric target")
			continue
		}

		// Determine response type
		switch target.Type {
		case "table":
			tableData, err := g.queryTableData(ctx, metricInfo, fromTime, toTime)
			if err != nil {
				g.log.WithError(err).WithField("target", target.Target).Error("Failed to query table data")
				continue
			}
			response = append(response, tableData)

		default: // time series
			timeSeriesData, err := g.queryTimeSeriesData(ctx, metricInfo, fromTime, toTime)
			if err != nil {
				g.log.WithError(err).WithField("target", target.Target).Error("Failed to query time series data")
				continue
			}
			response = append(response, timeSeriesData)
		}
	}

	g.writeGrafanaResponse(w, http.StatusOK, response)
}

// HandleGrafanaAnnotations handles annotation requests
func (g *grafanaAPI) HandleGrafanaAnnotations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g.log.Debug("Handling Grafana annotations request")

	var req GrafanaAnnotationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.log.WithError(err).Error("Failed to decode annotation request")
		g.writeGrafanaErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Parse time range
	fromTime, err := g.parseGrafanaTime(req.Range.From)
	if err != nil {
		g.log.WithError(err).Error("Failed to parse from time")
		g.writeGrafanaErrorResponse(w, http.StatusBadRequest, "Invalid from time format")
		return
	}

	toTime, err := g.parseGrafanaTime(req.Range.To)
	if err != nil {
		g.log.WithError(err).Error("Failed to parse to time")
		g.writeGrafanaErrorResponse(w, http.StatusBadRequest, "Invalid to time format")
		return
	}

	annotations, err := g.getAnnotations(ctx, req.Annotation, fromTime, toTime)
	if err != nil {
		g.log.WithError(err).Error("Failed to get annotations")
		g.writeGrafanaErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve annotations")
		return
	}

	g.writeGrafanaResponse(w, http.StatusOK, annotations)
}

// HandleGrafanaMetrics handles metrics metadata requests
func (g *grafanaAPI) HandleGrafanaMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g.log.Debug("Handling Grafana metrics request")

	// Get all available metrics with metadata
	metrics, err := g.getMetricsMetadata(ctx)
	if err != nil {
		g.log.WithError(err).Error("Failed to get metrics metadata")
		g.writeGrafanaErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve metrics metadata")
		return
	}

	g.writeGrafanaResponse(w, http.StatusOK, metrics)
}

// Data Query Methods

// getAvailableMetrics retrieves available metrics based on search criteria
func (g *grafanaAPI) getAvailableMetrics(ctx context.Context, searchTarget string) ([]string, error) {
	var metrics []string

	// Get distinct test names
	testQuery := `SELECT DISTINCT test_name FROM historic_runs ORDER BY test_name`
	rows, err := g.db.QueryContext(ctx, testQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query test names: %w", err)
	}
	defer rows.Close()

	var testNames []string
	for rows.Next() {
		var testName string
		if err := rows.Scan(&testName); err != nil {
			continue
		}
		testNames = append(testNames, testName)
	}

	// Get distinct clients
	clientQuery := `
		SELECT DISTINCT client_name 
		FROM (
			SELECT jsonb_object_keys(performance_scores) as client_name 
			FROM historic_runs 
			WHERE performance_scores IS NOT NULL
		) AS clients 
		ORDER BY client_name`

	rows, err = g.db.QueryContext(ctx, clientQuery)
	if err != nil {
		g.log.WithError(err).Warn("Failed to query client names")
	} else {
		defer rows.Close()
		var clientNames []string
		for rows.Next() {
			var clientName string
			if err := rows.Scan(&clientName); err != nil {
				continue
			}
			clientNames = append(clientNames, clientName)
		}

		// Generate metric combinations
		metricTypes := []string{"avg_latency", "p95_latency", "p99_latency", "error_rate", "throughput"}

		// Get distinct methods from benchmark_metrics table
		var methodNames []string
		methodQuery := `
			SELECT DISTINCT method 
			FROM benchmark_metrics 
			WHERE method IS NOT NULL AND method != ''
			ORDER BY method`

		methodRows, err := g.db.QueryContext(ctx, methodQuery)
		if err != nil {
			g.log.WithError(err).Warn("Failed to query method names")
		} else {
			defer methodRows.Close()
			for methodRows.Next() {
				var methodName string
				if err := methodRows.Scan(&methodName); err != nil {
					continue
				}
				methodNames = append(methodNames, methodName)
			}
		}

		for _, testName := range testNames {
			for _, metricType := range metricTypes {
				// Overall metrics
				metric := fmt.Sprintf("%s.overall.%s", testName, metricType)
				if searchTarget == "" || g.matchesSearch(metric, searchTarget) {
					metrics = append(metrics, metric)
				}

				// Client-specific metrics
				for _, clientName := range clientNames {
					clientMetric := fmt.Sprintf("%s.%s.%s", testName, clientName, metricType)
					if searchTarget == "" || g.matchesSearch(clientMetric, searchTarget) {
						metrics = append(metrics, clientMetric)
					}

					// Method-specific metrics
					for _, methodName := range methodNames {
						methodMetric := fmt.Sprintf("%s.%s.%s.%s", testName, clientName, methodName, metricType)
						if searchTarget == "" || g.matchesSearch(methodMetric, searchTarget) {
							metrics = append(metrics, methodMetric)
						}
					}
				}
			}
		}
	}

	// Add aggregation functions
	aggregations := []string{"count", "rate", "delta"}
	var aggregatedMetrics []string
	for _, metric := range metrics {
		for _, agg := range aggregations {
			aggMetric := fmt.Sprintf("%s(%s)", agg, metric)
			if searchTarget == "" || g.matchesSearch(aggMetric, searchTarget) {
				aggregatedMetrics = append(aggregatedMetrics, aggMetric)
			}
		}
	}
	metrics = append(metrics, aggregatedMetrics...)

	// Sort and limit results
	sort.Strings(metrics)
	if len(metrics) > 1000 {
		metrics = metrics[:1000]
	}

	return metrics, nil
}

// queryTimeSeriesData queries time series data for a metric
func (g *grafanaAPI) queryTimeSeriesData(ctx context.Context, metricInfo *MetricInfo, fromTime, toTime time.Time) (*GrafanaTimeSeries, error) {
	var query string
	var args []interface{}

	// Check if method is specified - use benchmark_metrics table for method-specific queries
	if metricInfo.Method != "" {
		// Query from benchmark_metrics table for method-specific data
		var metricName string
		switch metricInfo.MetricType {
		case "avg_latency":
			metricName = "latency_avg"
		case "p95_latency":
			metricName = "latency_p95"
		case "p99_latency":
			metricName = "latency_p99"
		case "error_rate":
			metricName = "error_rate"
		case "throughput":
			metricName = "throughput"
		default:
			return nil, fmt.Errorf("unsupported metric type: %s", metricInfo.MetricType)
		}

		query = `
			SELECT time as timestamp, value
			FROM benchmark_metrics
			WHERE metric_name = $1 AND client = $2 AND method = $3 
			  AND time >= $4 AND time <= $5`
		args = []interface{}{metricName, metricInfo.Client, metricInfo.Method, fromTime, toTime}

		// Add test name filter if we have a way to link it (through run_id)
		if metricInfo.TestName != "" {
			query = `
				SELECT bm.time as timestamp, bm.value
				FROM benchmark_metrics bm
				JOIN benchmark_runs br ON bm.run_id = br.id
				WHERE bm.metric_name = $1 AND bm.client = $2 AND bm.method = $3 
				  AND bm.time >= $4 AND bm.time <= $5
				  AND br.test_name = $6`
			args = append(args, metricInfo.TestName)
		}

		query += ` ORDER BY timestamp`
	} else {
		// Use historic_runs table for overall metrics (existing logic)
		switch metricInfo.MetricType {
		case "avg_latency":
			query = `
				SELECT timestamp, avg_latency_ms as value
				FROM historic_runs
				WHERE test_name = $1 AND timestamp >= $2 AND timestamp <= $3`
			args = []interface{}{metricInfo.TestName, fromTime, toTime}

		case "p95_latency":
			query = `
				SELECT timestamp, p95_latency_ms as value
				FROM historic_runs
				WHERE test_name = $1 AND timestamp >= $2 AND timestamp <= $3`
			args = []interface{}{metricInfo.TestName, fromTime, toTime}

		case "p99_latency":
			query = `
				SELECT timestamp, p99_latency_ms as value
				FROM historic_runs
				WHERE test_name = $1 AND timestamp >= $2 AND timestamp <= $3`
			args = []interface{}{metricInfo.TestName, fromTime, toTime}

		case "error_rate":
			query = `
				SELECT timestamp, overall_error_rate as value
				FROM historic_runs
				WHERE test_name = $1 AND timestamp >= $2 AND timestamp <= $3`
			args = []interface{}{metricInfo.TestName, fromTime, toTime}

		case "throughput":
			query = `
				SELECT timestamp, 
					   CASE 
						   WHEN duration != '' THEN total_requests / EXTRACT(EPOCH FROM duration::interval)
						   ELSE 0
					   END as value
				FROM historic_runs
				WHERE test_name = $1 AND timestamp >= $2 AND timestamp <= $3`
			args = []interface{}{metricInfo.TestName, fromTime, toTime}

		default:
			return nil, fmt.Errorf("unsupported metric type: %s", metricInfo.MetricType)
		}

		// Add client filtering if specified (only for historic_runs)
		if metricInfo.Client != "" && metricInfo.Client != "overall" {
			query += ` AND performance_scores ? $4`
			args = append(args, metricInfo.Client)
		}

		query += ` ORDER BY timestamp`
	}

	rows, err := g.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	var dataPoints [][]interface{}
	for rows.Next() {
		var timestamp time.Time
		var value float64

		if err := rows.Scan(&timestamp, &value); err != nil {
			g.log.WithError(err).Warn("Failed to scan data point")
			continue
		}

		// Apply aggregation if specified
		if metricInfo.Aggregation != "" {
			value = g.applyAggregation(metricInfo.Aggregation, value, dataPoints)
		}

		// Grafana expects [value, timestamp_ms] format
		dataPoints = append(dataPoints, []interface{}{value, timestamp.UnixNano() / 1000000})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	// Apply post-processing for aggregations
	if metricInfo.Aggregation != "" {
		dataPoints = g.postProcessAggregation(metricInfo.Aggregation, dataPoints)
	}

	return &GrafanaTimeSeries{
		Target:     metricInfo.OriginalTarget,
		DataPoints: dataPoints,
		Meta: map[string]interface{}{
			"test_name":   metricInfo.TestName,
			"client":      metricInfo.Client,
			"method":      metricInfo.Method,
			"metric_type": metricInfo.MetricType,
			"aggregation": metricInfo.Aggregation,
		},
	}, nil
}

// queryTableData queries table data for metrics
func (g *grafanaAPI) queryTableData(ctx context.Context, metricInfo *MetricInfo, fromTime, toTime time.Time) (*GrafanaTable, error) {
	query := `
		SELECT 
			timestamp,
			test_name,
			git_commit,
			git_branch,
			total_requests,
			total_errors,
			overall_error_rate,
			avg_latency_ms,
			p95_latency_ms,
			p99_latency_ms,
			best_client
		FROM historic_runs
		WHERE test_name = $1 AND timestamp >= $2 AND timestamp <= $3
		ORDER BY timestamp DESC
		LIMIT 1000`

	rows, err := g.db.QueryContext(ctx, query, metricInfo.TestName, fromTime, toTime)
	if err != nil {
		return nil, fmt.Errorf("failed to execute table query: %w", err)
	}
	defer rows.Close()

	columns := []TableColumn{
		{Text: "Time", Type: "time"},
		{Text: "Test", Type: "string"},
		{Text: "Commit", Type: "string"},
		{Text: "Branch", Type: "string"},
		{Text: "Requests", Type: "number"},
		{Text: "Errors", Type: "number"},
		{Text: "Error Rate", Type: "number"},
		{Text: "Avg Latency", Type: "number"},
		{Text: "P95 Latency", Type: "number"},
		{Text: "P99 Latency", Type: "number"},
		{Text: "Best Client", Type: "string"},
	}

	var tableRows [][]interface{}
	for rows.Next() {
		var timestamp time.Time
		var testName, gitCommit, gitBranch, bestClient string
		var totalRequests, totalErrors int64
		var errorRate, avgLatency, p95Latency, p99Latency float64

		err := rows.Scan(
			&timestamp, &testName, &gitCommit, &gitBranch,
			&totalRequests, &totalErrors, &errorRate,
			&avgLatency, &p95Latency, &p99Latency, &bestClient,
		)
		if err != nil {
			g.log.WithError(err).Warn("Failed to scan table row")
			continue
		}

		row := []interface{}{
			timestamp.UnixNano() / 1000000, // Convert to milliseconds
			testName,
			gitCommit,
			gitBranch,
			totalRequests,
			totalErrors,
			errorRate,
			avgLatency,
			p95Latency,
			p99Latency,
			bestClient,
		}
		tableRows = append(tableRows, row)
	}

	return &GrafanaTable{
		Columns: columns,
		Rows:    tableRows,
		Type:    "table",
		Meta: map[string]interface{}{
			"test_name": metricInfo.TestName,
			"row_count": len(tableRows),
		},
	}, nil
}

// getAnnotations retrieves annotations for the specified time range
func (g *grafanaAPI) getAnnotations(ctx context.Context, annotation GrafanaAnnotationQuery, fromTime, toTime time.Time) ([]GrafanaAnnotation, error) {
	var annotations []GrafanaAnnotation

	// Get regression annotations
	if annotation.Name == "regressions" || annotation.Name == "" {
		regressionQuery := `
			SELECT r.id, r.run_id, r.client, r.metric, r.severity, r.detected_at, hr.test_name
			FROM regressions r
			JOIN historic_runs hr ON r.run_id = hr.id
			WHERE r.detected_at >= $1 AND r.detected_at <= $2
			ORDER BY r.detected_at DESC
			LIMIT 100`

		rows, err := g.db.QueryContext(ctx, regressionQuery, fromTime, toTime)
		if err != nil {
			g.log.WithError(err).Warn("Failed to query regression annotations")
		} else {
			defer rows.Close()
			for rows.Next() {
				var id, runID, client, metric, severity, testName string
				var detectedAt time.Time

				if err := rows.Scan(&id, &runID, &client, &metric, &severity, &detectedAt, &testName); err != nil {
					continue
				}

				annotation := GrafanaAnnotation{
					Annotation: map[string]interface{}{
						"name":       "regressions",
						"enabled":    true,
						"datasource": "jsonrpc-bench",
						"iconColor":  g.getSeverityColor(severity),
						"showLine":   true,
					},
					Title: fmt.Sprintf("Regression: %s", metric),
					Text:  fmt.Sprintf("Client: %s, Severity: %s", client, severity),
					Time:  detectedAt.UnixNano() / 1000000,
					Tags:  []string{"regression", severity, client, testName},
					Data: map[string]interface{}{
						"regression_id": id,
						"run_id":        runID,
						"client":        client,
						"metric":        metric,
						"severity":      severity,
					},
				}
				annotations = append(annotations, annotation)
			}
		}
	}

	// Get baseline annotations
	if annotation.Name == "baselines" || annotation.Name == "" {
		baselineQuery := `
			SELECT b.name, b.run_id, b.test_name, b.created_at, hr.git_commit
			FROM baselines b
			JOIN historic_runs hr ON b.run_id = hr.id
			WHERE b.created_at >= $1 AND b.created_at <= $2
			ORDER BY b.created_at DESC
			LIMIT 50`

		rows, err := g.db.QueryContext(ctx, baselineQuery, fromTime, toTime)
		if err != nil {
			g.log.WithError(err).Warn("Failed to query baseline annotations")
		} else {
			defer rows.Close()
			for rows.Next() {
				var name, runID, testName, gitCommit string
				var createdAt time.Time

				if err := rows.Scan(&name, &runID, &testName, &createdAt, &gitCommit); err != nil {
					continue
				}

				annotation := GrafanaAnnotation{
					Annotation: map[string]interface{}{
						"name":       "baselines",
						"enabled":    true,
						"datasource": "jsonrpc-bench",
						"iconColor":  "green",
						"showLine":   true,
					},
					Title: fmt.Sprintf("Baseline: %s", name),
					Text:  fmt.Sprintf("Test: %s, Commit: %s", testName, gitCommit),
					Time:  createdAt.UnixNano() / 1000000,
					Tags:  []string{"baseline", testName},
					Data: map[string]interface{}{
						"baseline_name": name,
						"run_id":        runID,
						"test_name":     testName,
						"git_commit":    gitCommit,
					},
				}
				annotations = append(annotations, annotation)
			}
		}
	}

	// Get deployment annotations (significant runs)
	if annotation.Name == "deployments" || annotation.Name == "" {
		deploymentQuery := `
			SELECT id, test_name, git_commit, git_branch, timestamp, best_client
			FROM historic_runs
			WHERE timestamp >= $1 AND timestamp <= $2
			  AND git_commit IS NOT NULL 
			  AND git_commit != ''
			ORDER BY timestamp DESC
			LIMIT 50`

		rows, err := g.db.QueryContext(ctx, deploymentQuery, fromTime, toTime)
		if err != nil {
			g.log.WithError(err).Warn("Failed to query deployment annotations")
		} else {
			defer rows.Close()
			for rows.Next() {
				var id, testName, gitCommit, gitBranch, bestClient string
				var timestamp time.Time

				if err := rows.Scan(&id, &testName, &gitCommit, &gitBranch, &timestamp, &bestClient); err != nil {
					continue
				}

				annotation := GrafanaAnnotation{
					Annotation: map[string]interface{}{
						"name":       "deployments",
						"enabled":    true,
						"datasource": "jsonrpc-bench",
						"iconColor":  "blue",
						"showLine":   false,
					},
					Title: fmt.Sprintf("Run: %s", id[:8]),
					Text:  fmt.Sprintf("Branch: %s, Commit: %s, Best: %s", gitBranch, gitCommit[:8], bestClient),
					Time:  timestamp.UnixNano() / 1000000,
					Tags:  []string{"deployment", testName, gitBranch},
					Data: map[string]interface{}{
						"run_id":      id,
						"test_name":   testName,
						"git_commit":  gitCommit,
						"git_branch":  gitBranch,
						"best_client": bestClient,
					},
				}
				annotations = append(annotations, annotation)
			}
		}
	}

	return annotations, nil
}

// getMetricsMetadata retrieves metadata about available metrics
func (g *grafanaAPI) getMetricsMetadata(ctx context.Context) ([]MetricMetadata, error) {
	var metadata []MetricMetadata

	// Base metrics
	baseMetrics := []MetricMetadata{
		{
			Name:   "avg_latency",
			Type:   "gauge",
			Help:   "Average response latency in milliseconds",
			Unit:   "ms",
			Labels: []string{"test_name", "client"},
		},
		{
			Name:   "p95_latency",
			Type:   "gauge",
			Help:   "95th percentile response latency in milliseconds",
			Unit:   "ms",
			Labels: []string{"test_name", "client"},
		},
		{
			Name:   "p99_latency",
			Type:   "gauge",
			Help:   "99th percentile response latency in milliseconds",
			Unit:   "ms",
			Labels: []string{"test_name", "client"},
		},
		{
			Name:   "error_rate",
			Type:   "gauge",
			Help:   "Error rate as a percentage",
			Unit:   "percent",
			Labels: []string{"test_name", "client"},
		},
		{
			Name:   "throughput",
			Type:   "gauge",
			Help:   "Requests per second throughput",
			Unit:   "rps",
			Labels: []string{"test_name", "client"},
		},
	}

	metadata = append(metadata, baseMetrics...)

	// Add aggregated metrics
	aggregations := []string{"rate", "delta", "count"}
	for _, base := range baseMetrics {
		for _, agg := range aggregations {
			aggMetric := MetricMetadata{
				Name:   fmt.Sprintf("%s_%s", base.Name, agg),
				Type:   "counter",
				Help:   fmt.Sprintf("%s %s", strings.Title(agg), base.Help),
				Unit:   base.Unit,
				Labels: base.Labels,
			}
			metadata = append(metadata, aggMetric)
		}
	}

	return metadata, nil
}

// Helper Methods

// parseMetricTarget parses a Grafana metric target string
func (g *grafanaAPI) parseMetricTarget(target string) *MetricInfo {
	// Handle aggregation functions
	var aggregation string
	originalTarget := target
	if strings.Contains(target, "(") && strings.Contains(target, ")") {
		parts := strings.Split(target, "(")
		if len(parts) == 2 {
			aggregation = parts[0]
			target = strings.TrimSuffix(parts[1], ")")
		}
	}

	// Parse format: test_name.client.metric_type or test_name.client.method.metric_type
	parts := strings.Split(target, ".")
	if len(parts) < 3 {
		return nil
	}

	// Check if we have method-specific format
	if len(parts) == 4 {
		// Format: test_name.client.method.metric_type
		return &MetricInfo{
			OriginalTarget: originalTarget,
			TestName:       parts[0],
			Client:         parts[1],
			Method:         parts[2],
			MetricType:     parts[3],
			Aggregation:    aggregation,
		}
	}

	// Default format: test_name.client.metric_type
	return &MetricInfo{
		OriginalTarget: originalTarget,
		TestName:       parts[0],
		Client:         parts[1],
		Method:         "",
		MetricType:     parts[2],
		Aggregation:    aggregation,
	}
}

// parseGrafanaTime parses Grafana time format
func (g *grafanaAPI) parseGrafanaTime(timeStr string) (time.Time, error) {
	// Try different time formats
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, timeStr); err == nil {
			return t, nil
		}
	}

	// Try parsing as Unix timestamp
	if timestamp, err := strconv.ParseInt(timeStr, 10, 64); err == nil {
		// Handle both seconds and milliseconds
		if timestamp > 1e12 {
			return time.Unix(0, timestamp*1e6), nil
		}
		return time.Unix(timestamp, 0), nil
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", timeStr)
}

// matchesSearch checks if a metric matches the search criteria
func (g *grafanaAPI) matchesSearch(metric, search string) bool {
	if search == "" {
		return true
	}

	lowerMetric := strings.ToLower(metric)
	lowerSearch := strings.ToLower(search)

	// Support wildcard search
	if strings.Contains(search, "*") {
		// Simple wildcard matching
		pattern := strings.ReplaceAll(lowerSearch, "*", ".*")
		// This is a simplified regex match - in production you'd want proper regex
		return strings.Contains(lowerMetric, strings.ReplaceAll(pattern, ".*", ""))
	}

	return strings.Contains(lowerMetric, lowerSearch)
}

// applyAggregation applies the specified aggregation function
func (g *grafanaAPI) applyAggregation(aggregation string, value float64, existingPoints [][]interface{}) float64 {
	switch aggregation {
	case "rate":
		if len(existingPoints) > 0 {
			if lastPoint := existingPoints[len(existingPoints)-1]; len(lastPoint) >= 2 {
				if lastValue, ok := lastPoint[0].(float64); ok {
					return value - lastValue // Simple rate calculation
				}
			}
		}
		return 0

	case "delta":
		if len(existingPoints) > 0 {
			if lastPoint := existingPoints[len(existingPoints)-1]; len(lastPoint) >= 2 {
				if lastValue, ok := lastPoint[0].(float64); ok {
					return value - lastValue
				}
			}
		}
		return 0

	case "count":
		return 1 // Count of data points

	default:
		return value
	}
}

// postProcessAggregation applies post-processing for aggregations
func (g *grafanaAPI) postProcessAggregation(aggregation string, dataPoints [][]interface{}) [][]interface{} {
	switch aggregation {
	case "rate":
		// Smooth rate calculations
		if len(dataPoints) > 1 {
			for i := 1; i < len(dataPoints); i++ {
				if len(dataPoints[i]) >= 2 && len(dataPoints[i-1]) >= 2 {
					currentTime := dataPoints[i][1].(int64)
					prevTime := dataPoints[i-1][1].(int64)
					timeDiff := float64(currentTime-prevTime) / 1000.0 // Convert to seconds

					if timeDiff > 0 {
						if rate, ok := dataPoints[i][0].(float64); ok {
							dataPoints[i][0] = rate / timeDiff
						}
					}
				}
			}
		}

	case "count":
		// Convert count to cumulative
		cumulative := 0.0
		for i := range dataPoints {
			cumulative++
			if len(dataPoints[i]) >= 1 {
				dataPoints[i][0] = cumulative
			}
		}
	}

	return dataPoints
}

// getSeverityColor returns a color for regression severity
func (g *grafanaAPI) getSeverityColor(severity string) string {
	switch severity {
	case "critical":
		return "red"
	case "major":
		return "orange"
	case "minor":
		return "yellow"
	default:
		return "blue"
	}
}

// FormatGrafanaTimeSeries formats data as Grafana time series
func (g *grafanaAPI) FormatGrafanaTimeSeries(data []TimeSeriesDataPoint, target string) GrafanaTimeSeries {
	dataPoints := make([][]interface{}, len(data))
	for i, point := range data {
		dataPoints[i] = []interface{}{point.Value, point.Timestamp.UnixNano() / 1000000}
	}

	return GrafanaTimeSeries{
		Target:     target,
		DataPoints: dataPoints,
	}
}

// FormatGrafanaTable formats data as Grafana table
func (g *grafanaAPI) FormatGrafanaTable(data []TableRow, columns []TableColumn) GrafanaTable {
	rows := make([][]interface{}, len(data))
	for i, row := range data {
		rows[i] = row.Values
	}

	return GrafanaTable{
		Columns: columns,
		Rows:    rows,
		Type:    "table",
	}
}

// writeGrafanaResponse writes a Grafana-compatible JSON response
func (g *grafanaAPI) writeGrafanaResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		g.log.WithError(err).Error("Failed to encode Grafana response")
	}
}

// writeGrafanaErrorResponse writes a Grafana-compatible error response
func (g *grafanaAPI) writeGrafanaErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	errorResponse := map[string]interface{}{
		"error":   message,
		"message": message,
		"status":  statusCode,
	}

	g.writeGrafanaResponse(w, statusCode, errorResponse)
}

// Data Types for Grafana API

// MetricInfo contains parsed metric information
type MetricInfo struct {
	OriginalTarget string
	TestName       string
	Client         string
	Method         string
	MetricType     string
	Aggregation    string
}

// TimeSeriesDataPoint represents a single time series data point
type TimeSeriesDataPoint struct {
	Timestamp time.Time
	Value     float64
}

// TableRow represents a single table row
type TableRow struct {
	Values []interface{}
}

// TableColumn represents a table column definition
type TableColumn struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

// MetricMetadata provides metadata about a metric
type MetricMetadata struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Help   string   `json:"help"`
	Unit   string   `json:"unit"`
	Labels []string `json:"labels"`
}

// Grafana API request/response types

// GrafanaSearchRequest represents a Grafana search request
type GrafanaSearchRequest struct {
	Target string `json:"target"`
}

// GrafanaQueryRequest represents a Grafana query request
type GrafanaQueryRequest struct {
	Range         GrafanaRange    `json:"range"`
	Targets       []GrafanaTarget `json:"targets"`
	MaxDataPoints int             `json:"maxDataPoints,omitempty"`
	Interval      string          `json:"interval,omitempty"`
}

// GrafanaRange represents the time range for a query
type GrafanaRange struct {
	From string            `json:"from"`
	To   string            `json:"to"`
	Raw  map[string]string `json:"raw,omitempty"`
}

// GrafanaTarget represents a query target
type GrafanaTarget struct {
	Target string                 `json:"target"`
	RefID  string                 `json:"refId"`
	Type   string                 `json:"type,omitempty"`
	Format string                 `json:"format,omitempty"`
	Data   map[string]interface{} `json:"data,omitempty"`
	Hide   bool                   `json:"hide,omitempty"`
}

// GrafanaTimeSeries represents a time series response
type GrafanaTimeSeries struct {
	Target     string                 `json:"target"`
	DataPoints [][]interface{}        `json:"datapoints"`
	Meta       map[string]interface{} `json:"meta,omitempty"`
}

// GrafanaTable represents a table response
type GrafanaTable struct {
	Columns []TableColumn          `json:"columns"`
	Rows    [][]interface{}        `json:"rows"`
	Type    string                 `json:"type"`
	Meta    map[string]interface{} `json:"meta,omitempty"`
}

// GrafanaAnnotationRequest represents an annotation request
type GrafanaAnnotationRequest struct {
	Range      GrafanaRange           `json:"range"`
	Annotation GrafanaAnnotationQuery `json:"annotation"`
}

// GrafanaAnnotationQuery represents an annotation query
type GrafanaAnnotationQuery struct {
	Name       string `json:"name"`
	Datasource string `json:"datasource"`
	Enable     bool   `json:"enable"`
	IconColor  string `json:"iconColor"`
	Query      string `json:"query,omitempty"`
	TagKeys    string `json:"tagKeys,omitempty"`
	TextField  string `json:"textField,omitempty"`
	TitleField string `json:"titleField,omitempty"`
}

// GrafanaAnnotation represents an annotation response
type GrafanaAnnotation struct {
	Annotation map[string]interface{} `json:"annotation"`
	Title      string                 `json:"title"`
	Time       int64                  `json:"time"`
	TimeEnd    int64                  `json:"timeEnd,omitempty"`
	Text       string                 `json:"text"`
	Tags       []string               `json:"tags,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
}
