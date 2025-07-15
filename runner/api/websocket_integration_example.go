package api

// This file provides an example of how to integrate the WSHub with the existing server.
// This shows how the server.go can be updated to use the new WSHub implementation.

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// ExampleServerWithWSHub demonstrates how to integrate WSHub with the existing server
type ExampleServerWithWSHub struct {
	// Existing server dependencies
	storage            storage.HistoricStorage
	baselineManager    analysis.BaselineManager
	trendAnalyzer      analysis.TrendAnalyzer
	regressionDetector analysis.RegressionDetector
	db                 *sql.DB
	log                logrus.FieldLogger
	httpServer         *http.Server

	// WebSocket integration
	wsHub    *WSHub             // New WebSocket hub
	upgrader websocket.Upgrader // WebSocket upgrader
}

// NewExampleServerWithWSHub creates a new server with WebSocket support
func NewExampleServerWithWSHub(
	historicStorage storage.HistoricStorage,
	baselineManager analysis.BaselineManager,
	trendAnalyzer analysis.TrendAnalyzer,
	regressionDetector analysis.RegressionDetector,
	db *sql.DB,
	log logrus.FieldLogger,
) *ExampleServerWithWSHub {
	// Create WebSocket hub
	wsHub := NewWSHub(log)

	return &ExampleServerWithWSHub{
		storage:            historicStorage,
		baselineManager:    baselineManager,
		trendAnalyzer:      trendAnalyzer,
		regressionDetector: regressionDetector,
		db:                 db,
		log:                log.WithField("component", "api-server"),
		wsHub:              wsHub,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow connections from any origin
			},
		},
	}
}

// Start initializes and starts the HTTP API server with WebSocket support
func (s *ExampleServerWithWSHub) Start(ctx context.Context) error {
	s.log.Info("Starting API server with WebSocket support")

	// Start WebSocket hub first
	if err := s.wsHub.Run(ctx); err != nil {
		return err
	}

	// Setup routes (including WebSocket endpoint)
	router := s.setupRoutes()

	// Create and start HTTP server
	s.httpServer = &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		s.log.WithField("addr", s.httpServer.Addr).Info("API server listening")
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.WithError(err).Error("API server failed")
		}
	}()

	s.log.Info("API server with WebSocket support started successfully")
	return nil
}

// Stop gracefully shuts down the server
func (s *ExampleServerWithWSHub) Stop() error {
	s.log.Info("Stopping API server")

	// Stop WebSocket hub first
	if err := s.wsHub.Stop(); err != nil {
		s.log.WithError(err).Error("Failed to stop WebSocket hub")
	}

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 30)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.log.WithError(err).Error("Failed to shutdown API server gracefully")
		return err
	}

	s.log.Info("API server stopped")
	return nil
}

// setupRoutes configures all HTTP routes including WebSocket endpoint
func (s *ExampleServerWithWSHub) setupRoutes() *mux.Router {
	router := mux.NewRouter()

	// API routes
	api := router.PathPrefix("/api").Subrouter()

	// WebSocket endpoint using the new WSHub
	api.HandleFunc("/ws", s.wsHub.HandleWebSocketConnection(&s.upgrader))

	// Other existing routes would go here...
	// api.HandleFunc("/runs", s.handleListRuns).Methods("GET")
	// etc.

	return router
}

// Example methods showing how to use WebSocket notifications

// ExampleHandleNewBenchmarkRun shows how to notify clients about new runs
func (s *ExampleServerWithWSHub) ExampleHandleNewBenchmarkRun(run *types.HistoricRun) {
	// Store the run (existing logic)
	// ... existing storage logic ...

	// Notify WebSocket clients about the new run
	s.wsHub.NotifyNewRun(run)

	s.log.WithField("run_id", run.ID).Info("New benchmark run processed and broadcasted")
}

// ExampleHandleRegressionDetection shows how to notify clients about regressions
func (s *ExampleServerWithWSHub) ExampleHandleRegressionDetection(runID string) error {
	// Detect regressions (existing logic)
	// regressions, err := s.regressionDetector.DetectRegressions(ctx, runID, options)
	// if err != nil {
	//     return err
	// }

	// Example regression for demonstration
	regression := &types.Regression{
		ID:            "regression-1",
		RunID:         runID,
		BaselineRunID: "baseline-1",
		Client:        "geth",
		Metric:        "p95_latency",
		Severity:      "high",
		PercentChange: 25.5,
		IsSignificant: true,
	}

	// Get run details for notification
	run := &types.HistoricRun{
		ID:        runID,
		TestName:  "eth-benchmark",
		GitCommit: "abc123",
	}

	// Notify WebSocket clients about the regression
	s.wsHub.NotifyRegression(regression, run)

	s.log.WithFields(logrus.Fields{
		"run_id":        runID,
		"regression_id": regression.ID,
		"severity":      regression.Severity,
	}).Warn("Regression detected and broadcasted")

	return nil
}

// ExampleHandleBaselineUpdate shows how to notify clients about baseline updates
func (s *ExampleServerWithWSHub) ExampleHandleBaselineUpdate(baselineName, runID, testName string) {
	// Update baseline (existing logic)
	// ... existing baseline update logic ...

	// Notify WebSocket clients about the baseline update
	s.wsHub.NotifyBaselineUpdated(baselineName, runID, testName)

	s.log.WithFields(logrus.Fields{
		"baseline_name": baselineName,
		"run_id":        runID,
		"test_name":     testName,
	}).Info("Baseline updated and broadcasted")
}

// ExampleHandleAnalysisComplete shows how to notify clients about completed analysis
func (s *ExampleServerWithWSHub) ExampleHandleAnalysisComplete(runID, testName string) {
	// Perform analysis (existing logic)
	analysisResults := map[string]interface{}{
		"performance_score":  95.5,
		"regressions_found":  2,
		"improvements_found": 1,
		"recommendation":     "Consider optimizing database queries",
	}

	// Notify WebSocket clients about the completed analysis
	s.wsHub.NotifyAnalysisComplete(runID, testName, analysisResults)

	s.log.WithFields(logrus.Fields{
		"run_id":    runID,
		"test_name": testName,
	}).Info("Analysis completed and broadcasted")
}

// GetWebSocketStats returns statistics about WebSocket connections
func (s *ExampleServerWithWSHub) GetWebSocketStats() map[string]interface{} {
	return map[string]interface{}{
		"connected_clients": s.wsHub.GetConnectedClientsCount(),
		"client_info":       s.wsHub.GetClientInfo(),
	}
}

/*
Integration Guide:

1. Replace the existing server struct in server.go with this enhanced version
2. Update the constructor to include WSHub initialization
3. Modify the Start() method to initialize the WebSocket hub
4. Update the Stop() method to properly shutdown the WebSocket hub
5. Replace the existing WebSocket handler with the new WSHub-based handler
6. Add WebSocket notification calls throughout your application:
   - Call wsHub.NotifyNewRun() when a benchmark run completes
   - Call wsHub.NotifyRegression() when regressions are detected
   - Call wsHub.NotifyBaselineUpdated() when baselines are updated
   - Call wsHub.NotifyAnalysisComplete() when analysis finishes

Example WebSocket client JavaScript code:

const ws = new WebSocket('ws://localhost:8080/api/ws');

ws.onopen = function(event) {
    console.log('WebSocket connected');
};

ws.onmessage = function(event) {
    const message = JSON.parse(event.data);
    console.log('Received:', message);

    switch(message.type) {
        case 'new_run':
            handleNewRun(message.data);
            break;
        case 'regression_detected':
            handleRegression(message.data);
            break;
        case 'baseline_updated':
            handleBaselineUpdate(message.data);
            break;
        case 'analysis_complete':
            handleAnalysisComplete(message.data);
            break;
        case 'ping':
            ws.send(JSON.stringify({type: 'pong', timestamp: new Date()}));
            break;
    }
};

ws.onclose = function(event) {
    console.log('WebSocket disconnected');
    // Implement reconnection logic here
};

ws.onerror = function(error) {
    console.error('WebSocket error:', error);
};

function handleNewRun(data) {
    // Update dashboard with new run information
    console.log('New run completed:', data.run_id);
}

function handleRegression(data) {
    // Show regression alert
    console.log('Regression detected:', data);
    showAlert('Performance regression detected in ' + data.client, 'warning');
}

function handleBaselineUpdate(data) {
    // Update baseline information
    console.log('Baseline updated:', data.baseline_name);
}

function handleAnalysisComplete(data) {
    // Update analysis results
    console.log('Analysis complete for run:', data.run_id);
}
*/
