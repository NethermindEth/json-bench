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

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// Server provides HTTP API endpoints for historic data access
type Server interface {
	Start(ctx context.Context) error
	Stop() error
}

// server implements the API server
type server struct {
	addr               string
	storage            storage.HistoricStorage
	baselineManager    analysis.BaselineManager
	trendAnalyzer      analysis.TrendAnalyzer
	regressionDetector analysis.RegressionDetector
	db                 *sql.DB
	log                logrus.FieldLogger
	httpServer         *http.Server
	upgrader           websocket.Upgrader
	wsClients          map[*websocket.Conn]bool
	wsBroadcast        chan []byte
}

// NewServer creates a new API server instance
func NewServer(
	addr string,
	historicStorage storage.HistoricStorage,
	baselineManager analysis.BaselineManager,
	trendAnalyzer analysis.TrendAnalyzer,
	regressionDetector analysis.RegressionDetector,
	db *sql.DB,
	log logrus.FieldLogger,
) Server {
	return &server{
		addr:               addr,
		storage:            historicStorage,
		baselineManager:    baselineManager,
		trendAnalyzer:      trendAnalyzer,
		regressionDetector: regressionDetector,
		db:                 db,
		log:                log.WithField("component", "api-server"),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow connections from any origin
			},
		},
		wsClients:   make(map[*websocket.Conn]bool),
		wsBroadcast: make(chan []byte),
	}
}

// Start initializes and starts the HTTP API server
func (s *server) Start(ctx context.Context) error {
	s.log.Info("Starting API server")

	// Setup routes
	router := s.setupRoutes()

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:         s.addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start WebSocket hub
	go s.handleWebSocketHub()

	// Start server in goroutine
	go func() {
		s.log.WithField("addr", s.httpServer.Addr).Info("API server listening")
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.WithError(err).Error("API server failed")
		}
	}()

	s.log.Info("API server started successfully")
	return nil
}

// Stop gracefully shuts down the HTTP API server
func (s *server) Stop() error {
	s.log.Info("Stopping API server")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Close all WebSocket connections
	for client := range s.wsClients {
		client.Close()
	}
	close(s.wsBroadcast)

	// Shutdown HTTP server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.log.WithError(err).Error("Failed to shutdown API server gracefully")
		return err
	}

	s.log.Info("API server stopped")
	return nil
}

// setupRoutes configures all HTTP routes and middleware
func (s *server) setupRoutes() *mux.Router {
	router := mux.NewRouter()

	// Apply CORS middleware
	router.Use(s.enableCORS)

	// Apply logging middleware
	router.Use(s.loggingMiddleware)

	// Apply error handling middleware
	router.Use(s.errorHandlingMiddleware)

	// Local Dashboard API routes
	api := router.PathPrefix("/api").Subrouter()

	// Historic runs endpoints
	api.HandleFunc("/runs", s.handleListRuns).Methods("GET", "OPTIONS")
	api.HandleFunc("/runs/{runId}", s.handleGetRun).Methods("GET", "OPTIONS")
	api.HandleFunc("/runs/{runId}/methods", s.handleGetRunMethods).Methods("GET", "OPTIONS")
	api.HandleFunc("/runs/{runId}", s.handleDeleteRun).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/runs/{runId}/compare/{compareRunId}", s.handleCompareRuns).Methods("GET", "OPTIONS")

	// Test summary endpoints
	api.HandleFunc("/tests", s.handleListTests).Methods("GET", "OPTIONS")
	api.HandleFunc("/tests/{testName}/summary", s.handleGetTestSummary).Methods("GET", "OPTIONS")
	api.HandleFunc("/tests/{testName}/trends", s.handleGetTestTrends).Methods("GET", "OPTIONS")

	// Baseline management endpoints
	api.HandleFunc("/baselines", s.handleListBaselines).Methods("GET", "OPTIONS")
	api.HandleFunc("/baselines", s.handleCreateBaseline).Methods("POST", "OPTIONS")
	api.HandleFunc("/baselines/{baselineName}", s.handleGetBaseline).Methods("GET", "OPTIONS")
	api.HandleFunc("/baselines/{baselineName}", s.handleDeleteBaseline).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/baselines/{baselineName}/compare/{runId}", s.handleCompareToBaseline).Methods("GET", "OPTIONS")

	// Regression detection endpoints
	api.HandleFunc("/runs/{runId}/regressions", s.handleDetectRegressions).Methods("POST", "OPTIONS")
	api.HandleFunc("/runs/{runId}/regressions", s.handleGetRegressions).Methods("GET", "OPTIONS")
	api.HandleFunc("/regressions/{regressionId}/acknowledge", s.handleAcknowledgeRegression).Methods("POST", "OPTIONS")

	// Analysis endpoints
	api.HandleFunc("/runs/{runId}/analysis", s.handleAnalyzeRun).Methods("GET", "OPTIONS")
	api.HandleFunc("/tests/{testName}/trends/{metric}", s.handleGetMetricTrends).Methods("GET", "OPTIONS")
	api.HandleFunc("/tests/{testName}/clients/{client}/trends", s.handleGetClientTrends).Methods("GET", "OPTIONS")
	api.HandleFunc("/tests/{testName}/methods/{method}/trends", s.handleGetMethodTrends).Methods("GET", "OPTIONS")

	// WebSocket endpoint for real-time updates
	api.HandleFunc("/ws", s.handleWebSocket)

	// Health and status endpoints
	api.HandleFunc("/health", s.handleHealth).Methods("GET", "OPTIONS")

	// Grafana SimpleJSON Datasource API routes
	grafana := router.PathPrefix("/grafana").Subrouter()
	grafana.Use(s.enableCORS)

	// SimpleJSON datasource endpoints
	grafana.HandleFunc("/", s.handleGrafanaRoot).Methods("GET", "POST", "OPTIONS")
	grafana.HandleFunc("/search", s.handleGrafanaSearch).Methods("POST", "OPTIONS")
	grafana.HandleFunc("/query", s.handleGrafanaQuery).Methods("POST", "OPTIONS")
	grafana.HandleFunc("/annotations", s.handleGrafanaAnnotations).Methods("POST", "OPTIONS")
	grafana.HandleFunc("/tag-keys", s.handleGrafanaTagKeys).Methods("POST", "OPTIONS")
	grafana.HandleFunc("/tag-values", s.handleGrafanaTagValues).Methods("POST", "OPTIONS")

	// Health check endpoint
	router.HandleFunc("/health", s.handleHealth).Methods("GET")

	return router
}

// enableCORS adds CORS headers to responses
func (s *server) enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs HTTP requests
func (s *server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapper := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapper, r)

		duration := time.Since(start)
		s.log.WithFields(logrus.Fields{
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      wrapper.statusCode,
			"duration_ms": duration.Milliseconds(),
			"user_agent":  r.UserAgent(),
			"remote_addr": r.RemoteAddr,
		}).Info("HTTP request processed")
	})
}

// errorHandlingMiddleware provides centralized error handling
func (s *server) errorHandlingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				s.log.WithField("error", err).Error("Panic in HTTP handler")
				s.writeErrorResponse(w, http.StatusInternalServerError, "Internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// responseWriterWrapper wraps http.ResponseWriter to capture status codes
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Historic Runs API Handlers

// handleListRuns lists historic benchmark runs
func (s *server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	testName := r.URL.Query().Get("test")
	limitStr := r.URL.Query().Get("limit")

	limit := 50 // Default limit
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	// Create filter for runs
	filter := types.RunFilter{
		TestName: testName,
		Limit:    limit,
	}

	// Get runs from storage
	runs, err := s.storage.ListHistoricRuns(ctx, filter)
	if err != nil {
		s.log.WithError(err).Error("Failed to list historic runs")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve runs")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"runs":  runs,
		"count": len(runs),
		"limit": limit,
	})
}

// handleGetRun retrieves a specific historic run
func (s *server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	if runID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	run, err := s.storage.GetHistoricRun(ctx, runID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeErrorResponse(w, http.StatusNotFound, "Run not found")
		} else {
			s.log.WithError(err).Error("Failed to get historic run")
			s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve run")
		}
		return
	}

	// Parse full_results JSON to extract client_metrics
	response := map[string]interface{}{
		"run": run,
	}

	// Get client metrics from benchmark_metrics table
	clientMetrics, err := getClientMetricsForRun(s.db, runID)
	if err != nil {
		s.log.WithError(err).Warn("Failed to get client metrics from database")
	} else if len(clientMetrics) > 0 {
		response["client_metrics"] = clientMetrics
		s.log.WithField("run_id", runID).WithField("client_count", len(clientMetrics)).Debug("Added client metrics to response")
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleGetRunMethods returns method-specific metrics for a run
func (s *server) handleGetRunMethods(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	runID := vars["runId"]

	// Check if client parameter is provided for filtering
	clientFilter := r.URL.Query().Get("client")

	var query string
	var args []interface{}

	if clientFilter != "" {
		// Query method metrics for a specific client
		query = `
			SELECT 
				method,
				MAX(CASE WHEN metric_name = 'latency_avg' THEN value END) as avg_latency,
				MAX(CASE WHEN metric_name = 'latency_p50' THEN value END) as p50_latency,
				MAX(CASE WHEN metric_name = 'latency_p95' THEN value END) as p95_latency,
				MAX(CASE WHEN metric_name = 'latency_p99' THEN value END) as p99_latency,
				MAX(CASE WHEN metric_name = 'latency_min' THEN value END) as min_latency,
				MAX(CASE WHEN metric_name = 'latency_max' THEN value END) as max_latency,
				MAX(CASE WHEN metric_name = 'success_rate' THEN value END) as success_rate,
				MAX(CASE WHEN metric_name = 'throughput' THEN value END) as throughput,
				MAX(CASE WHEN metric_name = 'total_requests' THEN value END) as total_requests,
				MAX(CASE WHEN metric_name = 'error_rate' THEN value END) as error_rate
			FROM benchmark_metrics
			WHERE run_id = $1 AND client = $2 AND method != 'all'
			GROUP BY method
			ORDER BY method`
		args = []interface{}{runID, clientFilter}
	} else {
		// Query method metrics with client grouping for all clients
		query = `
			SELECT 
				client,
				method,
				MAX(CASE WHEN metric_name = 'latency_avg' THEN value END) as avg_latency,
				MAX(CASE WHEN metric_name = 'latency_p50' THEN value END) as p50_latency,
				MAX(CASE WHEN metric_name = 'latency_p95' THEN value END) as p95_latency,
				MAX(CASE WHEN metric_name = 'latency_p99' THEN value END) as p99_latency,
				MAX(CASE WHEN metric_name = 'latency_min' THEN value END) as min_latency,
				MAX(CASE WHEN metric_name = 'latency_max' THEN value END) as max_latency,
				MAX(CASE WHEN metric_name = 'success_rate' THEN value END) as success_rate,
				MAX(CASE WHEN metric_name = 'throughput' THEN value END) as throughput,
				MAX(CASE WHEN metric_name = 'total_requests' THEN value END) as total_requests,
				MAX(CASE WHEN metric_name = 'error_rate' THEN value END) as error_rate
			FROM benchmark_metrics
			WHERE run_id = $1 AND method != 'all'
			GROUP BY client, method
			ORDER BY client, method`
		args = []interface{}{runID}
	}

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.log.WithError(err).Error("Failed to query method metrics")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve method metrics")
		return
	}
	defer rows.Close()

	methods := make(map[string]interface{})
	clientMethods := make(map[string]map[string]interface{})

	if clientFilter != "" {
		// Single client response
		for rows.Next() {
			var method string
			var avgLatency, p50Latency, p95Latency, p99Latency, minLatency, maxLatency, successRate, throughput, totalRequests, errorRate sql.NullFloat64

			err := rows.Scan(&method, &avgLatency, &p50Latency, &p95Latency, &p99Latency,
				&minLatency, &maxLatency, &successRate, &throughput, &totalRequests, &errorRate)
			if err != nil {
				s.log.WithError(err).Error("Failed to scan method metrics")
				continue
			}

			// Helper function to get value or nil
			getValue := func(nf sql.NullFloat64) interface{} {
				if nf.Valid {
					return nf.Float64
				}
				return nil
			}

			methods[method] = map[string]interface{}{
				"avg_latency":    getValue(avgLatency),
				"p50_latency":    getValue(p50Latency),
				"p95_latency":    getValue(p95Latency),
				"p99_latency":    getValue(p99Latency),
				"min_latency":    getValue(minLatency),
				"max_latency":    getValue(maxLatency),
				"success_rate":   getValue(successRate),
				"throughput":     getValue(throughput),
				"total_requests": getValue(totalRequests),
				"error_rate":     getValue(errorRate),
			}
		}
	} else {
		// Multiple clients response
		for rows.Next() {
			var client, method string
			var avgLatency, p50Latency, p95Latency, p99Latency, minLatency, maxLatency, successRate, throughput, totalRequests, errorRate sql.NullFloat64

			err := rows.Scan(&client, &method, &avgLatency, &p50Latency, &p95Latency, &p99Latency,
				&minLatency, &maxLatency, &successRate, &throughput, &totalRequests, &errorRate)
			if err != nil {
				s.log.WithError(err).Error("Failed to scan method metrics")
				continue
			}

			// Initialize client map if not exists
			if _, exists := clientMethods[client]; !exists {
				clientMethods[client] = make(map[string]interface{})
			}

			// Helper function to get value or nil
			getValue := func(nf sql.NullFloat64) interface{} {
				if nf.Valid {
					return nf.Float64
				}
				return nil
			}

			clientMethods[client][method] = map[string]interface{}{
				"avg_latency":    getValue(avgLatency),
				"p50_latency":    getValue(p50Latency),
				"p95_latency":    getValue(p95Latency),
				"p99_latency":    getValue(p99Latency),
				"min_latency":    getValue(minLatency),
				"max_latency":    getValue(maxLatency),
				"success_rate":   getValue(successRate),
				"throughput":     getValue(throughput),
				"total_requests": getValue(totalRequests),
				"error_rate":     getValue(errorRate),
			}
		}
	}

	var response map[string]interface{}

	if clientFilter != "" {
		response = map[string]interface{}{
			"run_id":  runID,
			"client":  clientFilter,
			"methods": methods,
		}
	} else {
		response = map[string]interface{}{
			"run_id":            runID,
			"methods_by_client": clientMethods,
		}
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleDeleteRun deletes a historic run
func (s *server) handleDeleteRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	if runID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	err := s.storage.DeleteHistoricRun(ctx, runID)
	if err != nil {
		s.log.WithError(err).Error("Failed to delete historic run")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete run")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Run deleted successfully",
	})
}

// handleCompareRuns compares two historic runs
func (s *server) handleCompareRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID1 := vars["runId"]
	runID2 := vars["compareRunId"]

	if runID1 == "" || runID2 == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Both run IDs are required")
		return
	}

	comparison, err := s.storage.CompareRuns(ctx, runID1, runID2)
	if err != nil {
		s.log.WithError(err).Error("Failed to compare runs")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to compare runs")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, comparison)
}

// Test Summary API Handlers

// handleListTests lists available tests
func (s *server) handleListTests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Query distinct test names from database
	query := `SELECT DISTINCT test_name FROM benchmark_runs ORDER BY test_name`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		s.log.WithError(err).Error("Failed to query test names")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve tests")
		return
	}
	defer rows.Close()

	var tests []string
	for rows.Next() {
		var testName string
		if err := rows.Scan(&testName); err != nil {
			continue
		}
		tests = append(tests, testName)
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"tests": tests,
		"count": len(tests),
	})
}

// handleGetTestSummary provides summary information for a test
func (s *server) handleGetTestSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	testName := vars["testName"]

	if testName == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Test name is required")
		return
	}

	// Create filter for summary
	filter := types.RunFilter{
		TestName: testName,
	}

	summary, err := s.storage.GetHistoricSummary(ctx, filter)
	if err != nil {
		s.log.WithError(err).Error("Failed to get test summary")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve test summary")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, summary)
}

// handleGetTestTrends provides trend analysis for a test
func (s *server) handleGetTestTrends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	testName := vars["testName"]

	if testName == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Test name is required")
		return
	}

	// Parse query parameters
	daysStr := r.URL.Query().Get("days")
	days := 30 // Default to 30 days
	if daysStr != "" {
		if parsed, err := strconv.Atoi(daysStr); err == nil && parsed > 0 {
			days = parsed
		}
	}

	trends, err := s.trendAnalyzer.CalculateTrends(ctx, testName, days)
	if err != nil {
		s.log.WithError(err).Error("Failed to calculate trends")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to calculate trends")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, trends)
}

// Baseline Management API Handlers

// handleListBaselines lists all baselines
func (s *server) handleListBaselines(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	testName := r.URL.Query().Get("test")

	baselines, err := s.baselineManager.ListBaselines(ctx, testName)
	if err != nil {
		s.log.WithError(err).Error("Failed to list baselines")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve baselines")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"baselines": baselines,
		"count":     len(baselines),
	})
}

// BaselineRequest represents a request to create a baseline
type BaselineRequest struct {
	RunID       string `json:"run_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleCreateBaseline creates a new baseline
func (s *server) handleCreateBaseline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req BaselineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.RunID == "" || req.Name == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Run ID and name are required")
		return
	}

	baseline, err := s.baselineManager.SetBaseline(ctx, req.RunID, req.Name, req.Description)
	if err != nil {
		s.log.WithError(err).Error("Failed to create baseline")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to create baseline")
		return
	}

	s.writeJSONResponse(w, http.StatusCreated, baseline)
}

// handleGetBaseline retrieves a specific baseline
func (s *server) handleGetBaseline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	baselineName := vars["baselineName"]

	if baselineName == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Baseline name is required")
		return
	}

	baseline, err := s.baselineManager.GetBaseline(ctx, baselineName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeErrorResponse(w, http.StatusNotFound, "Baseline not found")
		} else {
			s.log.WithError(err).Error("Failed to get baseline")
			s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve baseline")
		}
		return
	}

	s.writeJSONResponse(w, http.StatusOK, baseline)
}

// handleDeleteBaseline deletes a baseline
func (s *server) handleDeleteBaseline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	baselineName := vars["baselineName"]

	if baselineName == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Baseline name is required")
		return
	}

	err := s.baselineManager.DeleteBaseline(ctx, baselineName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeErrorResponse(w, http.StatusNotFound, "Baseline not found")
		} else {
			s.log.WithError(err).Error("Failed to delete baseline")
			s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete baseline")
		}
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Baseline deleted successfully",
	})
}

// handleCompareToBaseline compares a run to a baseline
func (s *server) handleCompareToBaseline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	baselineName := vars["baselineName"]
	runID := vars["runId"]

	if baselineName == "" || runID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Baseline name and run ID are required")
		return
	}

	comparison, err := s.baselineManager.CompareToBaseline(ctx, runID, baselineName)
	if err != nil {
		s.log.WithError(err).Error("Failed to compare to baseline")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to compare to baseline")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, comparison)
}

// Regression Detection API Handlers

// handleDetectRegressions detects regressions for a run
func (s *server) handleDetectRegressions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	if runID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	var req RegressionDetectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
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
		EnableStatistical:  true,
		MinConfidence:      0.95,
		IgnoreImprovements: false,
	}

	report, err := s.regressionDetector.DetectRegressions(ctx, runID, options)
	if err != nil {
		s.log.WithError(err).Error("Failed to detect regressions")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to detect regressions")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, report)
}

// handleGetRegressions retrieves regressions for a run
func (s *server) handleGetRegressions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	if runID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	regressions, err := s.regressionDetector.GetRegressions(ctx, runID)
	if err != nil {
		s.log.WithError(err).Error("Failed to get regressions")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve regressions")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"regressions": regressions,
		"count":       len(regressions),
	})
}

// handleAcknowledgeRegression acknowledges a regression
func (s *server) handleAcknowledgeRegression(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	regressionID := vars["regressionId"]

	if regressionID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Regression ID is required")
		return
	}

	var req AcknowledgeRegressionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AcknowledgedBy == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Acknowledged by is required")
		return
	}

	err := s.regressionDetector.AcknowledgeRegression(ctx, regressionID, req.AcknowledgedBy)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeErrorResponse(w, http.StatusNotFound, "Regression not found")
		} else {
			s.log.WithError(err).Error("Failed to acknowledge regression")
			s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to acknowledge regression")
		}
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Regression acknowledged successfully",
	})
}

// Analysis API Handlers

// handleAnalyzeRun provides comprehensive analysis for a run
func (s *server) handleAnalyzeRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	runID := vars["runId"]

	if runID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Run ID is required")
		return
	}

	analysis, err := s.regressionDetector.AnalyzeRun(ctx, runID)
	if err != nil {
		s.log.WithError(err).Error("Failed to analyze run")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to analyze run")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, analysis)
}

// handleGetMetricTrends provides trend analysis for a specific metric
func (s *server) handleGetMetricTrends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	testName := vars["testName"]
	metric := vars["metric"]

	if testName == "" || metric == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Test name and metric are required")
		return
	}

	// Parse query parameters
	daysStr := r.URL.Query().Get("days")
	client := r.URL.Query().Get("client")

	days := 30 // Default to 30 days
	if daysStr != "" {
		if parsed, err := strconv.Atoi(daysStr); err == nil && parsed > 0 {
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

	trend, err := s.storage.GetHistoricTrends(ctx, filter)
	if err != nil {
		s.log.WithError(err).Error("Failed to get metric trends")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve metric trends")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, trend)
}

// handleGetClientTrends provides trend analysis for a specific client
func (s *server) handleGetClientTrends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	testName := vars["testName"]
	client := vars["client"]

	if testName == "" || client == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Test name and client are required")
		return
	}

	// Parse query parameters
	daysStr := r.URL.Query().Get("days")
	days := 30 // Default to 30 days
	if daysStr != "" {
		if parsed, err := strconv.Atoi(daysStr); err == nil && parsed > 0 {
			days = parsed
		}
	}

	trends, err := s.trendAnalyzer.GetClientTrends(ctx, testName, client, days)
	if err != nil {
		s.log.WithError(err).Error("Failed to get client trends")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve client trends")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, trends)
}

// handleGetMethodTrends provides trend analysis for a specific method
func (s *server) handleGetMethodTrends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	testName := vars["testName"]
	method := vars["method"]

	if testName == "" || method == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Test name and method are required")
		return
	}

	// Parse query parameters
	daysStr := r.URL.Query().Get("days")
	days := 30 // Default to 30 days
	if daysStr != "" {
		if parsed, err := strconv.Atoi(daysStr); err == nil && parsed > 0 {
			days = parsed
		}
	}

	trends, err := s.trendAnalyzer.GetMethodTrends(ctx, testName, method, days)
	if err != nil {
		s.log.WithError(err).Error("Failed to get method trends")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve method trends")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, trends)
}

// WebSocket Handler

// handleWebSocket handles WebSocket connections for real-time updates
func (s *server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.WithError(err).Error("Failed to upgrade WebSocket connection")
		return
	}
	defer conn.Close()

	// Register client
	s.wsClients[conn] = true

	s.log.WithField("remote_addr", r.RemoteAddr).Info("WebSocket client connected")

	// Send initial connection message
	message := map[string]interface{}{
		"type":      "connection",
		"status":    "connected",
		"timestamp": time.Now(),
	}

	if err := conn.WriteJSON(message); err != nil {
		s.log.WithError(err).Error("Failed to send initial WebSocket message")
		delete(s.wsClients, conn)
		return
	}

	// Handle incoming messages (if any)
	for {
		var msg map[string]interface{}
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.log.WithError(err).Error("WebSocket connection error")
			}
			break
		}

		// Handle ping messages
		if msgType, ok := msg["type"].(string); ok && msgType == "ping" {
			pong := map[string]interface{}{
				"type":      "pong",
				"timestamp": time.Now(),
			}
			if err := conn.WriteJSON(pong); err != nil {
				break
			}
		}
	}

	// Unregister client
	delete(s.wsClients, conn)
	s.log.WithField("remote_addr", r.RemoteAddr).Info("WebSocket client disconnected")
}

// handleWebSocketHub manages WebSocket message broadcasting
func (s *server) handleWebSocketHub() {
	for {
		select {
		case message, ok := <-s.wsBroadcast:
			if !ok {
				return
			}

			// Broadcast to all connected clients
			for client := range s.wsClients {
				err := client.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					client.Close()
					delete(s.wsClients, client)
				}
			}
		}
	}
}

// BroadcastUpdate sends a real-time update to all WebSocket clients
func (s *server) BroadcastUpdate(updateType string, data interface{}) {
	message := map[string]interface{}{
		"type":      updateType,
		"data":      data,
		"timestamp": time.Now(),
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		s.log.WithError(err).Error("Failed to marshal WebSocket message")
		return
	}

	select {
	case s.wsBroadcast <- messageBytes:
	default:
		// Channel is full, skip this update
	}
}

// Grafana SimpleJSON Datasource Handlers

// handleGrafanaRoot handles the root endpoint for Grafana datasource
func (s *server) handleGrafanaRoot(w http.ResponseWriter, r *http.Request) {
	s.writeJSONResponse(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

// handleGrafanaSearch handles search requests from Grafana
func (s *server) handleGrafanaSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req GrafanaSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get available metrics based on search target
	metrics := []string{}

	// Query distinct test names and metrics from database
	query := `SELECT DISTINCT test_name FROM benchmark_runs ORDER BY test_name`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		s.log.WithError(err).Error("Failed to query test names for Grafana")
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve metrics")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var testName string
		if err := rows.Scan(&testName); err != nil {
			continue
		}

		// Add different metric types for each test
		metricTypes := []string{"avg_latency", "p95_latency", "p99_latency", "error_rate"}
		for _, metricType := range metricTypes {
			metrics = append(metrics, fmt.Sprintf("%s.%s", testName, metricType))
		}
	}

	// Filter metrics based on search target
	if req.Target != "" {
		filtered := []string{}
		for _, metric := range metrics {
			if strings.Contains(strings.ToLower(metric), strings.ToLower(req.Target)) {
				filtered = append(filtered, metric)
			}
		}
		metrics = filtered
	}

	s.writeJSONResponse(w, http.StatusOK, metrics)
}

// handleGrafanaQuery handles query requests from Grafana
func (s *server) handleGrafanaQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req GrafanaQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	var response []GrafanaTimeSeries

	for _, target := range req.Targets {
		if target.Target == "" {
			continue
		}

		// Parse target format: test_name.metric_type
		parts := strings.Split(target.Target, ".")
		if len(parts) != 2 {
			continue
		}

		testName := parts[0]
		metricType := parts[1]

		// Parse time range
		fromTime, err := time.Parse(time.RFC3339, req.Range.From)
		if err != nil {
			continue
		}

		toTime, err := time.Parse(time.RFC3339, req.Range.To)
		if err != nil {
			continue
		}

		// Query historic data
		query := `
			SELECT timestamp, 
				   CASE 
					   WHEN $2 = 'avg_latency' THEN avg_latency
					   WHEN $2 = 'p95_latency' THEN p95_latency
					   WHEN $2 = 'p99_latency' THEN p95_latency
					   WHEN $2 = 'error_rate' THEN (100.0 - success_rate)
					   ELSE 0
				   END as value
			FROM benchmark_runs
			WHERE test_name = $1
			  AND timestamp >= $3
			  AND timestamp <= $4
			ORDER BY timestamp`

		rows, err := s.db.QueryContext(ctx, query, testName, metricType, fromTime, toTime)
		if err != nil {
			s.log.WithError(err).Error("Failed to query data for Grafana")
			continue
		}

		var dataPoints [][]interface{}
		for rows.Next() {
			var timestamp time.Time
			var value float64

			if err := rows.Scan(&timestamp, &value); err != nil {
				continue
			}

			// Grafana expects [value, timestamp_ms] format
			dataPoints = append(dataPoints, []interface{}{value, timestamp.UnixNano() / 1000000})
		}
		rows.Close()

		// Sort data points by timestamp
		sort.Slice(dataPoints, func(i, j int) bool {
			return dataPoints[i][1].(int64) < dataPoints[j][1].(int64)
		})

		response = append(response, GrafanaTimeSeries{
			Target:     target.Target,
			DataPoints: dataPoints,
		})
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleGrafanaAnnotations handles annotation requests from Grafana
func (s *server) handleGrafanaAnnotations(w http.ResponseWriter, r *http.Request) {
	// Return empty annotations for now
	s.writeJSONResponse(w, http.StatusOK, []interface{}{})
}

// handleGrafanaTagKeys handles tag key requests from Grafana
func (s *server) handleGrafanaTagKeys(w http.ResponseWriter, r *http.Request) {
	tags := []map[string]string{
		{"type": "string", "text": "test_name"},
		{"type": "string", "text": "client"},
		{"type": "string", "text": "metric_type"},
	}

	s.writeJSONResponse(w, http.StatusOK, tags)
}

// handleGrafanaTagValues handles tag value requests from Grafana
func (s *server) handleGrafanaTagValues(w http.ResponseWriter, r *http.Request) {
	// Return empty tag values for now
	s.writeJSONResponse(w, http.StatusOK, []string{})
}

// Health Check Handler

// handleHealth provides a health check endpoint
func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
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
	if err := s.db.Ping(); err != nil {
		status["status"] = "unhealthy"
		status["services"].(map[string]string)["database"] = "disconnected"
		s.writeJSONResponse(w, http.StatusServiceUnavailable, status)
		return
	}

	s.writeJSONResponse(w, http.StatusOK, status)
}

// Utility Methods

// writeJSONResponse writes a JSON response with the given status code
func (s *server) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.log.WithError(err).Error("Failed to encode JSON response")
	}
}

// writeErrorResponse writes an error response with the given status code and message
func (s *server) writeErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	errorResponse := map[string]interface{}{
		"error":   true,
		"message": message,
		"status":  statusCode,
	}

	s.writeJSONResponse(w, statusCode, errorResponse)
}
