package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// APIHandlers provides comprehensive REST API handlers for the historic tracking system
type APIHandlers interface {
	Start(ctx context.Context) error
	Stop() error

	// Historic runs handlers
	HandleListRuns(w http.ResponseWriter, r *http.Request)
	HandleGetRun(w http.ResponseWriter, r *http.Request)
	HandleGetRunMethods(w http.ResponseWriter, r *http.Request)
	HandleGetReport(w http.ResponseWriter, r *http.Request)
	HandleDeleteRun(w http.ResponseWriter, r *http.Request)
	HandleCompareRuns(w http.ResponseWriter, r *http.Request)

	// Baseline management handlers
	HandleListBaselines(w http.ResponseWriter, r *http.Request)
	HandleCreateBaseline(w http.ResponseWriter, r *http.Request)
	HandleGetBaseline(w http.ResponseWriter, r *http.Request)
	HandleDeleteBaseline(w http.ResponseWriter, r *http.Request)
	HandleSetBaseline(w http.ResponseWriter, r *http.Request)

	// Trend analysis handlers
	HandleGetTrends(w http.ResponseWriter, r *http.Request)
	HandleMethodTrends(w http.ResponseWriter, r *http.Request)
	HandleClientTrends(w http.ResponseWriter, r *http.Request)

	// Regression detection handlers
	HandleGetRegressions(w http.ResponseWriter, r *http.Request)
	HandleDetectRegressions(w http.ResponseWriter, r *http.Request)
	HandleAcknowledgeRegression(w http.ResponseWriter, r *http.Request)

	// Analysis handlers
	HandleAnalyzeRun(w http.ResponseWriter, r *http.Request)
	HandleGetMetricTrends(w http.ResponseWriter, r *http.Request)

	// Health and status handlers
	HandleHealth(w http.ResponseWriter, r *http.Request)
	HandleStatus(w http.ResponseWriter, r *http.Request)
}

// apiHandlers implements APIHandlers interface
type apiHandlers struct {
	storage            storage.HistoricStorage
	baselineManager    analysis.BaselineManager
	trendAnalyzer      analysis.TrendAnalyzer
	regressionDetector analysis.RegressionDetector
	db                 *sql.DB
	log                logrus.FieldLogger
}

// NewAPIHandlers creates a new API handlers instance
func NewAPIHandlers(
	historicStorage storage.HistoricStorage,
	baselineManager analysis.BaselineManager,
	trendAnalyzer analysis.TrendAnalyzer,
	regressionDetector analysis.RegressionDetector,
	db *sql.DB,
	log logrus.FieldLogger,
) APIHandlers {
	return &apiHandlers{
		storage:            historicStorage,
		baselineManager:    baselineManager,
		trendAnalyzer:      trendAnalyzer,
		regressionDetector: regressionDetector,
		db:                 db,
		log:                log.WithField("component", "api-handlers"),
	}
}

// Start initializes the API handlers
func (h *apiHandlers) Start(ctx context.Context) error {
	h.log.Info("Starting API handlers")
	return nil
}

// Stop shuts down the API handlers
func (h *apiHandlers) Stop() error {
	h.log.Info("Stopping API handlers")
	return nil
}

// Historic Runs Handlers

// HandleListRuns lists historic benchmark runs with filtering and pagination
func (h *apiHandlers) HandleListRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	h.log.Debug("Handling list runs request")

	// Parse query parameters
	testName := r.URL.Query().Get("test")
	branch := r.URL.Query().Get("branch")
	client := r.URL.Query().Get("client")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	// Set defaults
	limit := 50
	offset := 0

	// Parse limit
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	// Parse offset
	if offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Build query with filtering
	query := `
		SELECT id, test_name, description, git_commit, git_branch,
			   timestamp, timestamp as start_time, timestamp as end_time, duration,
			   0 as clients_count, 0 as endpoints_count, 0 as target_rps,
			   total_requests, 0 as total_errors, (100.0 - success_rate) as overall_error_rate,
			   avg_latency as avg_latency_ms, p95_latency as p95_latency_ms, p95_latency as p99_latency_ms, avg_latency as max_latency_ms,
			   '' as best_client, '' as notes, timestamp as created_at
		FROM benchmark_runs
		WHERE 1=1`

	args := []interface{}{}
	argIndex := 1

	// Add filters
	if testName != "" {
		query += fmt.Sprintf(" AND test_name = $%d", argIndex)
		args = append(args, testName)
		argIndex++
	}

	if branch != "" {
		query += fmt.Sprintf(" AND git_branch = $%d", argIndex)
		args = append(args, branch)
		argIndex++
	}

	if fromStr != "" {
		if fromTime, err := time.Parse("2006-01-02", fromStr); err == nil {
			query += fmt.Sprintf(" AND timestamp >= $%d", argIndex)
			args = append(args, fromTime)
			argIndex++
		}
	}

	if toStr != "" {
		if toTime, err := time.Parse("2006-01-02", toStr); err == nil {
			// Add 24 hours to include the entire day
			toTime = toTime.Add(24 * time.Hour)
			query += fmt.Sprintf(" AND timestamp < $%d", argIndex)
			args = append(args, toTime)
			argIndex++
		}
	}

	// Add performance score filter if client specified
	if client != "" {
		query += fmt.Sprintf(" AND performance_scores ? $%d", argIndex)
		args = append(args, client)
		argIndex++
	}

	// Add ordering and pagination
	query += " ORDER BY timestamp DESC"
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
	args = append(args, limit, offset)

	// Execute query
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		h.log.WithError(err).Error("Failed to query historic runs")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve runs")
		return
	}
	defer rows.Close()

	var runs []RunSummary
	for rows.Next() {
		var run RunSummary
		err := rows.Scan(
			&run.ID, &run.TestName, &run.Description, &run.GitCommit, &run.GitBranch,
			&run.Timestamp, &run.StartTime, &run.EndTime, &run.Duration,
			&run.ClientsCount, &run.EndpointsCount, &run.TargetRPS,
			&run.TotalRequests, &run.TotalErrors, &run.OverallErrorRate,
			&run.AvgLatencyMs, &run.P95LatencyMs, &run.P99LatencyMs, &run.MaxLatencyMs,
			&run.BestClient, &run.Notes, &run.CreatedAt,
		)
		if err != nil {
			h.log.WithError(err).Error("Failed to scan run")
			continue
		}
		runs = append(runs, run)
	}

	// Get total count for pagination
	countQuery := `SELECT COUNT(*) FROM benchmark_runs WHERE 1=1`
	countArgs := args[:len(args)-2] // Remove LIMIT and OFFSET args

	if testName != "" {
		countQuery += " AND test_name = $1"
	}
	if branch != "" {
		countQuery += " AND git_branch = $" + strconv.Itoa(len(countArgs))
	}
	// Add other filters as needed for count...

	var totalCount int
	err = h.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount)
	if err != nil {
		h.log.WithError(err).Warn("Failed to get total count")
		totalCount = len(runs)
	}

	response := map[string]interface{}{
		"runs":        runs,
		"count":       len(runs),
		"total_count": totalCount,
		"limit":       limit,
		"offset":      offset,
		"filters": map[string]interface{}{
			"test":   testName,
			"branch": branch,
			"client": client,
			"from":   fromStr,
			"to":     toStr,
		},
	}

	h.writeJSONResponse(w, http.StatusOK, response)
}

// HandleGetRun retrieves a specific historic run with full details
func (h *apiHandlers) HandleGetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	h.log.WithField("run_id", runID).Debug("Handling get run request")

	if runID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	run, err := h.storage.GetHistoricRun(ctx, runID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeErrorResponse(w, http.StatusNotFound, "Run not found")
		} else {
			h.log.WithError(err).Error("Failed to get historic run")
			h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve run")
		}
		return
	}

	// Check if client filtering is requested
	clientFilter := r.URL.Query().Get("client")
	if clientFilter != "" {
		// Parse full results and filter by client
		var fullResult types.BenchmarkResult
		if err := json.Unmarshal(run.FullResults, &fullResult); err == nil {
			filteredMetrics := make(map[string]*types.ClientMetrics)
			if clientMetrics, exists := fullResult.ClientMetrics[clientFilter]; exists {
				filteredMetrics[clientFilter] = clientMetrics
				fullResult.ClientMetrics = filteredMetrics
				if filteredJSON, err := json.Marshal(fullResult); err == nil {
					run.FullResults = filteredJSON
				}
			}
		}
	}

	h.writeJSONResponse(w, http.StatusOK, run)
}

// HandleGetRunMethods returns method-specific metrics for a run
func (h *apiHandlers) HandleGetRunMethods(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	runID := vars["runId"]

	h.log.WithField("run_id", runID).Debug("Getting method metrics for run")

	// Query method metrics directly from database
	// Note: Using MAX aggregation to handle cases where there might be multiple metric entries
	query := `
		SELECT 
			method,
			MAX(CASE WHEN metric_name = 'latency_avg' THEN value END) as avg_latency,
			MAX(CASE WHEN metric_name = 'latency_p50' THEN value END) as p50_latency,
			MAX(CASE WHEN metric_name = 'latency_p95' THEN value END) as p95_latency,
			MAX(CASE WHEN metric_name = 'latency_p99' THEN value END) as p99_latency,
			MAX(CASE WHEN metric_name = 'latency_min' THEN value END) as min_latency,
			MAX(CASE WHEN metric_name = 'latency_max' THEN value END) as max_latency,
			MAX(CASE WHEN metric_name = 'success_rate' THEN value END) as success_rate,
			MAX(CASE WHEN metric_name = 'throughput' THEN value END) as throughput
		FROM benchmark_metrics
		WHERE run_id = $1 AND method != 'all'
		GROUP BY method
		ORDER BY method`

	rows, err := h.db.QueryContext(r.Context(), query, runID)
	if err != nil {
		h.log.WithError(err).Error("Failed to query method metrics")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve method metrics")
		return
	}
	defer rows.Close()

	methods := make(map[string]interface{})
	methodCount := 0
	for rows.Next() {
		var method string
		var avgLatency, p50Latency, p95Latency, p99Latency, minLatency, maxLatency, successRate, throughput sql.NullFloat64

		err := rows.Scan(&method, &avgLatency, &p50Latency, &p95Latency, &p99Latency,
			&minLatency, &maxLatency, &successRate, &throughput)
		if err != nil {
			h.log.WithError(err).Error("Failed to scan method metrics")
			continue
		}

		// Debug logging for NULL values
		h.log.WithFields(logrus.Fields{
			"method":    method,
			"p99_valid": p99Latency.Valid,
			"p99_value": p99Latency.Float64,
			"avg_valid": avgLatency.Valid,
			"avg_value": avgLatency.Float64,
		}).Debug("Scanned method metrics")

		// Helper function to get value or nil from sql.NullFloat64
		getValue := func(nf sql.NullFloat64) interface{} {
			if nf.Valid {
				return nf.Float64
			}
			return nil
		}

		methods[method] = map[string]interface{}{
			"avg_latency":  getValue(avgLatency),
			"p50_latency":  getValue(p50Latency),
			"p95_latency":  getValue(p95Latency),
			"p99_latency":  getValue(p99Latency),
			"min_latency":  getValue(minLatency),
			"max_latency":  getValue(maxLatency),
			"success_rate": getValue(successRate),
			"throughput":   getValue(throughput),
		}
		methodCount++
	}

	// Debug logging for total methods found
	h.log.WithFields(logrus.Fields{
		"run_id":       runID,
		"method_count": methodCount,
	}).Debug("Completed processing method metrics")

	response := map[string]interface{}{
		"run_id":  runID,
		"methods": methods,
	}

	h.writeJSONResponse(w, http.StatusOK, response)
}

// HandleGetReport retrieves a formatted report for a historic run
func (h *apiHandlers) HandleGetReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	h.log.WithField("run_id", runID).Debug("Handling get report request")

	if runID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	// Get the run
	run, err := h.storage.GetHistoricRun(ctx, runID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeErrorResponse(w, http.StatusNotFound, "Run not found")
		} else {
			h.log.WithError(err).Error("Failed to get historic run")
			h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve run")
		}
		return
	}

	// Parse full results
	var fullResult types.BenchmarkResult
	if err := json.Unmarshal(run.FullResults, &fullResult); err != nil {
		h.log.WithError(err).Error("Failed to parse full results")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to parse run results")
		return
	}

	// Generate comprehensive report
	report := h.generateRunReport(run, &fullResult)

	format := r.URL.Query().Get("format")
	switch format {
	case "html":
		w.Header().Set("Content-Type", "text/html")
		// In a full implementation, you would generate HTML here
		fmt.Fprintf(w, "<h1>Report for Run %s</h1><pre>%+v</pre>", runID, report)
	default:
		h.writeJSONResponse(w, http.StatusOK, report)
	}
}

// HandleDeleteRun deletes a historic run
func (h *apiHandlers) HandleDeleteRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	h.log.WithField("run_id", runID).Info("Handling delete run request")

	if runID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	err := h.storage.DeleteHistoricRun(ctx, runID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeErrorResponse(w, http.StatusNotFound, "Run not found")
		} else {
			h.log.WithError(err).Error("Failed to delete historic run")
			h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete run")
		}
		return
	}

	h.writeJSONResponse(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Run deleted successfully",
		"run_id":  runID,
	})
}

// HandleCompareRuns compares two historic runs
func (h *apiHandlers) HandleCompareRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID1 := vars["runId1"]
	runID2 := vars["runId2"]

	h.log.WithFields(logrus.Fields{
		"run_id_1": runID1,
		"run_id_2": runID2,
	}).Debug("Handling compare runs request")

	if runID1 == "" || runID2 == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Both run IDs are required")
		return
	}

	comparison, err := h.storage.CompareRuns(ctx, runID1, runID2)
	if err != nil {
		h.log.WithError(err).Error("Failed to compare runs")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to compare runs")
		return
	}

	// Add detailed analysis if requested
	if r.URL.Query().Get("detailed") == "true" {
		h.enhanceComparison(ctx, comparison, runID1, runID2)
	}

	h.writeJSONResponse(w, http.StatusOK, comparison)
}

// Baseline Management Handlers

// HandleListBaselines lists all baselines with optional filtering
func (h *apiHandlers) HandleListBaselines(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	testName := r.URL.Query().Get("test")

	h.log.WithField("test_name", testName).Debug("Handling list baselines request")

	baselines, err := h.baselineManager.ListBaselines(ctx, testName)
	if err != nil {
		h.log.WithError(err).Error("Failed to list baselines")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve baselines")
		return
	}

	response := map[string]interface{}{
		"baselines": baselines,
		"count":     len(baselines),
		"test":      testName,
	}

	h.writeJSONResponse(w, http.StatusOK, response)
}

// HandleCreateBaseline creates a new baseline
func (h *apiHandlers) HandleCreateBaseline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	h.log.Debug("Handling create baseline request")

	var req CreateBaselineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.RunID == "" || req.Name == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Run ID and name are required")
		return
	}

	baseline, err := h.baselineManager.SetBaseline(ctx, req.RunID, req.Name, req.Description)
	if err != nil {
		h.log.WithError(err).Error("Failed to create baseline")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to create baseline")
		return
	}

	h.writeJSONResponse(w, http.StatusCreated, baseline)
}

// HandleGetBaseline retrieves a specific baseline
func (h *apiHandlers) HandleGetBaseline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	baselineName := vars["baselineName"]

	h.log.WithField("baseline_name", baselineName).Debug("Handling get baseline request")

	if baselineName == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Baseline name is required")
		return
	}

	baseline, err := h.baselineManager.GetBaseline(ctx, baselineName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeErrorResponse(w, http.StatusNotFound, "Baseline not found")
		} else {
			h.log.WithError(err).Error("Failed to get baseline")
			h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve baseline")
		}
		return
	}

	h.writeJSONResponse(w, http.StatusOK, baseline)
}

// HandleDeleteBaseline deletes a baseline
func (h *apiHandlers) HandleDeleteBaseline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	baselineName := vars["baselineName"]

	h.log.WithField("baseline_name", baselineName).Info("Handling delete baseline request")

	if baselineName == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Baseline name is required")
		return
	}

	err := h.baselineManager.DeleteBaseline(ctx, baselineName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeErrorResponse(w, http.StatusNotFound, "Baseline not found")
		} else {
			h.log.WithError(err).Error("Failed to delete baseline")
			h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete baseline")
		}
		return
	}

	h.writeJSONResponse(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Baseline deleted successfully",
		"name":    baselineName,
	})
}

// HandleSetBaseline sets a new baseline from a run
func (h *apiHandlers) HandleSetBaseline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	h.log.WithField("run_id", runID).Info("Handling set baseline request")

	if runID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	var req SetBaselineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Baseline name is required")
		return
	}

	baseline, err := h.baselineManager.SetBaseline(ctx, runID, req.Name, req.Description)
	if err != nil {
		h.log.WithError(err).Error("Failed to set baseline")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to set baseline")
		return
	}

	h.writeJSONResponse(w, http.StatusCreated, baseline)
}

// Trend Analysis Handlers

// HandleGetTrends retrieves comprehensive trend analysis
func (h *apiHandlers) HandleGetTrends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	testName := r.URL.Query().Get("test")
	daysStr := r.URL.Query().Get("days")

	h.log.WithFields(logrus.Fields{
		"test_name": testName,
		"days":      daysStr,
	}).Debug("Handling get trends request")

	if testName == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Test name is required")
		return
	}

	days := 30 // Default
	if daysStr != "" {
		if parsed, err := strconv.Atoi(daysStr); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}

	trends, err := h.trendAnalyzer.CalculateTrends(ctx, testName, days)
	if err != nil {
		h.log.WithError(err).Error("Failed to calculate trends")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to calculate trends")
		return
	}

	h.writeJSONResponse(w, http.StatusOK, trends)
}

// HandleMethodTrends retrieves method-specific trends
func (h *apiHandlers) HandleMethodTrends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	testName := vars["testName"]
	method := vars["method"]
	daysStr := r.URL.Query().Get("days")

	h.log.WithFields(logrus.Fields{
		"test_name": testName,
		"method":    method,
		"days":      daysStr,
	}).Debug("Handling method trends request")

	if testName == "" || method == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Test name and method are required")
		return
	}

	days := 30 // Default
	if daysStr != "" {
		if parsed, err := strconv.Atoi(daysStr); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}

	trends, err := h.trendAnalyzer.GetMethodTrends(ctx, testName, method, days)
	if err != nil {
		h.log.WithError(err).Error("Failed to get method trends")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve method trends")
		return
	}

	h.writeJSONResponse(w, http.StatusOK, trends)
}

// HandleClientTrends retrieves client-specific trends
func (h *apiHandlers) HandleClientTrends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	testName := vars["testName"]
	client := vars["client"]
	daysStr := r.URL.Query().Get("days")

	h.log.WithFields(logrus.Fields{
		"test_name": testName,
		"client":    client,
		"days":      daysStr,
	}).Debug("Handling client trends request")

	if testName == "" || client == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Test name and client are required")
		return
	}

	days := 30 // Default
	if daysStr != "" {
		if parsed, err := strconv.Atoi(daysStr); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}

	trends, err := h.trendAnalyzer.GetClientTrends(ctx, testName, client, days)
	if err != nil {
		h.log.WithError(err).Error("Failed to get client trends")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve client trends")
		return
	}

	h.writeJSONResponse(w, http.StatusOK, trends)
}

// Regression Detection Handlers

// HandleGetRegressions retrieves regressions for a run
func (h *apiHandlers) HandleGetRegressions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	h.log.WithField("run_id", runID).Debug("Handling get regressions request")

	if runID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	regressions, err := h.regressionDetector.GetRegressions(ctx, runID)
	if err != nil {
		h.log.WithError(err).Error("Failed to get regressions")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve regressions")
		return
	}

	// Filter by severity if requested
	severityFilter := r.URL.Query().Get("severity")
	if severityFilter != "" {
		var filtered []*types.Regression
		for _, regression := range regressions {
			if regression.Severity == severityFilter {
				filtered = append(filtered, regression)
			}
		}
		regressions = filtered
	}

	response := map[string]interface{}{
		"regressions": regressions,
		"count":       len(regressions),
		"run_id":      runID,
	}

	h.writeJSONResponse(w, http.StatusOK, response)
}

// HandleDetectRegressions detects regressions for a run
func (h *apiHandlers) HandleDetectRegressions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	h.log.WithField("run_id", runID).Info("Handling detect regressions request")

	if runID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	var req RegressionDetectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Set defaults
	if req.ComparisonMode == "" {
		req.ComparisonMode = "sequential"
	}
	if req.LookbackCount == 0 {
		req.LookbackCount = 1
	}
	if req.WindowSize == 0 {
		req.WindowSize = 5
	}

	options := analysis.DetectionOptions{
		ComparisonMode:     req.ComparisonMode,
		BaselineName:       req.BaselineName,
		LookbackCount:      req.LookbackCount,
		WindowSize:         req.WindowSize,
		EnableStatistical:  req.EnableStatistical,
		CustomThresholds:   req.CustomThresholds,
		IncludeClients:     req.IncludeClients,
		ExcludeClients:     req.ExcludeClients,
		IncludeMethods:     req.IncludeMethods,
		ExcludeMethods:     req.ExcludeMethods,
		MinConfidence:      req.MinConfidence,
		IgnoreImprovements: req.IgnoreImprovements,
	}

	report, err := h.regressionDetector.DetectRegressions(ctx, runID, options)
	if err != nil {
		h.log.WithError(err).Error("Failed to detect regressions")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to detect regressions")
		return
	}

	h.writeJSONResponse(w, http.StatusOK, report)
}

// HandleAcknowledgeRegression acknowledges a regression
func (h *apiHandlers) HandleAcknowledgeRegression(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	regressionID := vars["regressionId"]

	h.log.WithField("regression_id", regressionID).Info("Handling acknowledge regression request")

	if regressionID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Regression ID is required")
		return
	}

	var req AcknowledgeRegressionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AcknowledgedBy == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Acknowledged by is required")
		return
	}

	err := h.regressionDetector.AcknowledgeRegression(ctx, regressionID, req.AcknowledgedBy)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeErrorResponse(w, http.StatusNotFound, "Regression not found")
		} else {
			h.log.WithError(err).Error("Failed to acknowledge regression")
			h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to acknowledge regression")
		}
		return
	}

	h.writeJSONResponse(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Regression acknowledged successfully",
		"id":      regressionID,
	})
}

// Analysis Handlers

// HandleAnalyzeRun provides comprehensive analysis for a run
func (h *apiHandlers) HandleAnalyzeRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	h.log.WithField("run_id", runID).Debug("Handling analyze run request")

	if runID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	analysis, err := h.regressionDetector.AnalyzeRun(ctx, runID)
	if err != nil {
		h.log.WithError(err).Error("Failed to analyze run")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to analyze run")
		return
	}

	h.writeJSONResponse(w, http.StatusOK, analysis)
}

// HandleGetMetricTrends provides trend analysis for a specific metric
func (h *apiHandlers) HandleGetMetricTrends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	testName := vars["testName"]
	metric := vars["metric"]

	h.log.WithFields(logrus.Fields{
		"test_name": testName,
		"metric":    metric,
	}).Debug("Handling get metric trends request")

	if testName == "" || metric == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Test name and metric are required")
		return
	}

	// Parse query parameters
	daysStr := r.URL.Query().Get("days")
	client := r.URL.Query().Get("client")
	windowSizeStr := r.URL.Query().Get("window_size")

	days := 30 // Default
	if daysStr != "" {
		if parsed, err := strconv.Atoi(daysStr); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}

	if client == "" {
		client = "overall"
	}

	// Create trend filter
	since := time.Now().AddDate(0, 0, -days)
	filter := types.TrendFilter{
		Client: client,
		Since:  since,
	}

	// Get basic trend
	trend, err := h.storage.GetHistoricTrends(ctx, filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to get metric trends")
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve metric trends")
		return
	}

	response := map[string]interface{}{
		"trend":     trend,
		"test_name": testName,
		"metric":    metric,
		"client":    client,
		"days":      days,
	}

	// Add moving average if requested
	if windowSizeStr != "" {
		if windowSize, err := strconv.Atoi(windowSizeStr); err == nil && windowSize > 1 {
			movingAvg, err := h.trendAnalyzer.CalculateMovingAverage(ctx, testName, metric, windowSize, days)
			if err == nil {
				response["moving_average"] = movingAvg
			}
		}
	}

	// Add forecasting if requested
	if r.URL.Query().Get("forecast") == "true" {
		forecastDays := 7 // Default
		if forecastDaysStr := r.URL.Query().Get("forecast_days"); forecastDaysStr != "" {
			if parsed, err := strconv.Atoi(forecastDaysStr); err == nil && parsed > 0 && parsed <= 30 {
				forecastDays = parsed
			}
		}

		forecast, err := h.trendAnalyzer.ForecastTrend(ctx, testName, metric, days, forecastDays)
		if err == nil {
			response["forecast"] = forecast
		}
	}

	h.writeJSONResponse(w, http.StatusOK, response)
}

// Health and Status Handlers

// HandleHealth provides a health check endpoint
func (h *apiHandlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	h.log.Debug("Handling health check request")

	status := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"version":   "1.0.0",
		"services": map[string]string{
			"database": "connected",
			"storage":  "ready",
		},
	}

	// Check database connection
	if err := h.db.Ping(); err != nil {
		status["status"] = "unhealthy"
		status["services"].(map[string]string)["database"] = "disconnected"
		h.writeJSONResponse(w, http.StatusServiceUnavailable, status)
		return
	}

	// Check storage health
	ctx := r.Context()
	healthFilter := types.RunFilter{Limit: 1}
	if _, err := h.storage.ListHistoricRuns(ctx, healthFilter); err != nil {
		status["services"].(map[string]string)["storage"] = "error"
		h.log.WithError(err).Warn("Storage health check failed")
	}

	h.writeJSONResponse(w, http.StatusOK, status)
}

// HandleStatus provides detailed system status
func (h *apiHandlers) HandleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	h.log.Debug("Handling status request")

	// Get system statistics
	var totalRuns int64
	err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM historic_runs").Scan(&totalRuns)
	if err != nil {
		h.log.WithError(err).Warn("Failed to get total runs count")
	}

	var totalBaselines int64
	err = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM baselines WHERE is_active = true").Scan(&totalBaselines)
	if err != nil {
		h.log.WithError(err).Warn("Failed to get total baselines count")
	}

	var totalRegressions int64
	err = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM regressions WHERE acknowledged_at IS NULL").Scan(&totalRegressions)
	if err != nil {
		h.log.WithError(err).Warn("Failed to get total regressions count")
	}

	// Get recent activity
	var lastRunTime *time.Time
	err = h.db.QueryRowContext(ctx, "SELECT MAX(created_at) FROM historic_runs").Scan(&lastRunTime)
	if err != nil {
		h.log.WithError(err).Warn("Failed to get last run time")
	}

	status := map[string]interface{}{
		"status":    "operational",
		"timestamp": time.Now(),
		"statistics": map[string]interface{}{
			"total_runs":                 totalRuns,
			"total_baselines":            totalBaselines,
			"unacknowledged_regressions": totalRegressions,
			"last_run_time":              lastRunTime,
		},
		"components": map[string]string{
			"api_handlers":        "healthy",
			"storage":             "healthy",
			"baseline_manager":    "healthy",
			"trend_analyzer":      "healthy",
			"regression_detector": "healthy",
		},
	}

	h.writeJSONResponse(w, http.StatusOK, status)
}

// Helper Methods

// writeJSONResponse writes a JSON response with the given status code
func (h *apiHandlers) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.log.WithError(err).Error("Failed to encode JSON response")
	}
}

// writeErrorResponse writes an error response with the given status code and message
func (h *apiHandlers) writeErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	errorResponse := map[string]interface{}{
		"error":     true,
		"message":   message,
		"status":    statusCode,
		"timestamp": time.Now(),
	}

	h.writeJSONResponse(w, statusCode, errorResponse)
}

// MethodMetric represents aggregated metrics for a specific method
type MethodMetric struct {
	Name          string  `json:"name"`
	TotalRequests int64   `json:"total_requests"`
	SuccessRate   float64 `json:"success_rate"`
	AvgLatency    float64 `json:"avg_latency"`
	P50Latency    float64 `json:"p50_latency"`
	P95Latency    float64 `json:"p95_latency"`
	P99Latency    float64 `json:"p99_latency"`
	MinLatency    float64 `json:"min_latency"`
	MaxLatency    float64 `json:"max_latency"`
	StdDev        float64 `json:"std_dev"`
	ErrorRate     float64 `json:"error_rate"`
	Throughput    float64 `json:"throughput"`
}

// generateRunReport generates a comprehensive report for a run
func (h *apiHandlers) generateRunReport(run *types.HistoricRun, result *types.BenchmarkResult) map[string]interface{} {
	report := map[string]interface{}{
		"run_id":             run.ID,
		"test_name":          run.TestName,
		"timestamp":          run.Timestamp,
		"duration":           run.Duration,
		"git_commit":         run.GitCommit,
		"git_branch":         run.GitBranch,
		"total_requests":     run.TotalRequests,
		"total_errors":       run.TotalErrors,
		"overall_error_rate": run.OverallErrorRate,
		"performance_summary": map[string]interface{}{
			"avg_latency_ms": run.AvgLatencyMs,
			"p95_latency_ms": run.P95LatencyMs,
			"p99_latency_ms": run.P99LatencyMs,
			"max_latency_ms": run.MaxLatencyMs,
			"best_client":    run.BestClient,
		},
		"client_count":   run.ClientsCount,
		"endpoint_count": run.EndpointsCount,
		"target_rps":     run.TargetRPS,
	}

	// Add client breakdown with method metrics
	clientBreakdown := make(map[string]interface{})
	methodMetrics := make([]MethodMetric, 0)

	for clientName, metrics := range result.ClientMetrics {
		// Collect method metrics for this client
		clientMethods := make([]MethodMetric, 0)

		for methodName, methodStats := range metrics.Methods {
			methodMetric := MethodMetric{
				Name:          methodName,
				TotalRequests: methodStats.Count,
				SuccessRate:   methodStats.SuccessRate,
				AvgLatency:    methodStats.Avg,
				P50Latency:    methodStats.P50,
				P95Latency:    methodStats.P95,
				P99Latency:    methodStats.P99,
				MinLatency:    methodStats.Min,
				MaxLatency:    methodStats.Max,
				StdDev:        methodStats.StdDev,
				ErrorRate:     methodStats.ErrorRate,
				Throughput:    methodStats.Throughput,
			}
			clientMethods = append(clientMethods, methodMetric)

			// Also add to the global method metrics list
			methodMetrics = append(methodMetrics, methodMetric)
		}

		clientBreakdown[clientName] = map[string]interface{}{
			"total_requests": metrics.TotalRequests,
			"total_errors":   metrics.TotalErrors,
			"error_rate":     metrics.ErrorRate,
			"latency": map[string]interface{}{
				"avg":        metrics.Latency.Avg,
				"p50":        metrics.Latency.P50,
				"p95":        metrics.Latency.P95,
				"p99":        metrics.Latency.P99,
				"max":        metrics.Latency.Max,
				"throughput": metrics.Latency.Throughput,
			},
			"method_count": len(metrics.Methods),
			"methods":      clientMethods, // Add method metrics per client
		}
	}
	report["clients"] = clientBreakdown

	// Add aggregated method metrics at the top level
	report["method_metrics"] = methodMetrics

	// Add performance scores
	report["performance_scores"] = run.PerformanceScores

	return report
}

// enhanceComparison adds detailed analysis to run comparison
func (h *apiHandlers) enhanceComparison(ctx context.Context, comparison *types.BaselineComparison, runID1, runID2 string) {
	// Add regression analysis
	// This would involve detecting regressions between the two runs
	// For now, we'll add a placeholder for enhanced analysis
	comparison.Summary += " (Enhanced analysis requested)"
}

// Request/Response types for API handlers

// RunSummary provides a summary view of a historic run
type RunSummary struct {
	ID               string    `json:"id"`
	TestName         string    `json:"test_name"`
	Description      string    `json:"description"`
	GitCommit        string    `json:"git_commit"`
	GitBranch        string    `json:"git_branch"`
	Timestamp        time.Time `json:"timestamp"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	Duration         string    `json:"duration"`
	ClientsCount     int       `json:"clients_count"`
	EndpointsCount   int       `json:"endpoints_count"`
	TargetRPS        int       `json:"target_rps"`
	TotalRequests    int64     `json:"total_requests"`
	TotalErrors      int64     `json:"total_errors"`
	OverallErrorRate float64   `json:"overall_error_rate"`
	AvgLatencyMs     float64   `json:"avg_latency_ms"`
	P95LatencyMs     float64   `json:"p95_latency_ms"`
	P99LatencyMs     float64   `json:"p99_latency_ms"`
	MaxLatencyMs     float64   `json:"max_latency_ms"`
	BestClient       string    `json:"best_client"`
	Notes            string    `json:"notes,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// CreateBaselineRequest represents a request to create a baseline
type CreateBaselineRequest struct {
	RunID       string `json:"run_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SetBaselineRequest represents a request to set a baseline
type SetBaselineRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// RegressionDetectionRequest represents a request to detect regressions
type RegressionDetectionRequest struct {
	ComparisonMode     string                                  `json:"comparison_mode"`
	BaselineName       string                                  `json:"baseline_name,omitempty"`
	LookbackCount      int                                     `json:"lookback_count,omitempty"`
	WindowSize         int                                     `json:"window_size,omitempty"`
	EnableStatistical  bool                                    `json:"enable_statistical"`
	CustomThresholds   map[string]analysis.RegressionThreshold `json:"custom_thresholds,omitempty"`
	IncludeClients     []string                                `json:"include_clients,omitempty"`
	ExcludeClients     []string                                `json:"exclude_clients,omitempty"`
	IncludeMethods     []string                                `json:"include_methods,omitempty"`
	ExcludeMethods     []string                                `json:"exclude_methods,omitempty"`
	MinConfidence      float64                                 `json:"min_confidence,omitempty"`
	IgnoreImprovements bool                                    `json:"ignore_improvements"`
}

// AcknowledgeRegressionRequest represents a request to acknowledge a regression
type AcknowledgeRegressionRequest struct {
	AcknowledgedBy string `json:"acknowledged_by"`
	Notes          string `json:"notes,omitempty"`
}
