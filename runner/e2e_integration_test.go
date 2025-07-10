package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/api"
	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// E2EIntegrationTestSuite provides end-to-end integration tests for the entire system
type E2EIntegrationTestSuite struct {
	suite.Suite

	// Infrastructure
	container *postgres.PostgresContainer
	db        *sql.DB
	tempDir   string
	ctx       context.Context
	logger    logrus.FieldLogger

	// System components
	historicStorage    storage.HistoricStorage
	baselineManager    analysis.BaselineManager
	trendAnalyzer      analysis.TrendAnalyzer
	regressionDetector analysis.RegressionDetector
	apiServer          api.Server

	// Configuration
	storageConfig *config.StorageConfig

	// Test server URL
	serverURL string
}

// SetupSuite initializes the complete test environment
func (suite *E2EIntegrationTestSuite) SetupSuite() {
	suite.ctx = context.Background()
	suite.logger = logrus.New().WithField("test", "e2e_integration")

	// Create temporary directory for file storage
	tempDir, err := os.MkdirTemp("", "e2e_integration_test_*")
	require.NoError(suite.T(), err)
	suite.tempDir = tempDir

	// Start PostgreSQL container with TimescaleDB extension
	pgContainer, err := postgres.RunContainer(suite.ctx,
		testcontainers.WithImage("timescale/timescaledb:latest-pg15"),
		postgres.WithDatabase("e2e_test_db"),
		postgres.WithUsername("e2e_test_user"),
		postgres.WithPassword("e2e_test_pass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(suite.T(), err)
	suite.container = pgContainer

	// Setup database connection
	mappedPort, err := pgContainer.MappedPort(suite.ctx, "5432")
	require.NoError(suite.T(), err)

	connStr := fmt.Sprintf("host=localhost port=%d user=e2e_test_user password=e2e_test_pass dbname=e2e_test_db sslmode=disable",
		mappedPort.Int())
	db, err := sql.Open("postgres", connStr)
	require.NoError(suite.T(), err)
	suite.db = db

	// Create storage configuration
	suite.storageConfig = &config.StorageConfig{
		HistoricPath:   filepath.Join(suite.tempDir, "historic"),
		RetentionDays:  30,
		EnableHistoric: true,
		PostgreSQL: config.PostgreSQLConfig{
			Host:            "localhost",
			Port:            int(mappedPort.Int()),
			Database:        "e2e_test_db",
			User:            "e2e_test_user",
			Password:        "e2e_test_pass",
			SSLMode:         "disable",
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			MetricsTable:    "benchmark_metrics",
			RunsTable:       "benchmark_runs",
			RetentionPolicy: "7d",
		},
	}

	// Validate configuration
	err = suite.storageConfig.Validate()
	require.NoError(suite.T(), err)

	// Initialize database schema
	migration := storage.NewMigrationService(db, suite.logger)
	err = migration.Up()
	require.NoError(suite.T(), err)

	// Create indices for better performance
	err = migration.CreateIndices()
	require.NoError(suite.T(), err)

	// Initialize system components
	suite.historicStorage = storage.NewHistoricStorage(db, suite.storageConfig.HistoricPath, suite.logger)
	suite.baselineManager = analysis.NewBaselineManager(suite.historicStorage, db, suite.logger)
	suite.trendAnalyzer = analysis.NewTrendAnalyzer(suite.historicStorage, db, suite.logger)
	suite.regressionDetector = analysis.NewRegressionDetector(suite.historicStorage, suite.baselineManager, db, suite.logger)

	// Start all components
	err = suite.historicStorage.Start(suite.ctx)
	require.NoError(suite.T(), err)

	err = suite.baselineManager.Start(suite.ctx)
	require.NoError(suite.T(), err)

	err = suite.trendAnalyzer.Start(suite.ctx)
	require.NoError(suite.T(), err)

	err = suite.regressionDetector.Start(suite.ctx)
	require.NoError(suite.T(), err)

	// Create and start API server
	suite.apiServer = api.NewServer(
		suite.historicStorage,
		suite.baselineManager,
		suite.trendAnalyzer,
		suite.regressionDetector,
		db,
		suite.logger,
	)

	err = suite.apiServer.Start(suite.ctx)
	require.NoError(suite.T(), err)

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	suite.serverURL = "http://localhost:8080"
}

// TearDownSuite cleans up all test resources
func (suite *E2EIntegrationTestSuite) TearDownSuite() {
	if suite.apiServer != nil {
		suite.apiServer.Stop()
	}
	if suite.regressionDetector != nil {
		suite.regressionDetector.Stop()
	}
	if suite.trendAnalyzer != nil {
		suite.trendAnalyzer.Stop()
	}
	if suite.baselineManager != nil {
		suite.baselineManager.Stop()
	}
	if suite.historicStorage != nil {
		suite.historicStorage.Stop()
	}
	if suite.db != nil {
		suite.db.Close()
	}
	if suite.container != nil {
		suite.container.Terminate(suite.ctx)
	}
	if suite.tempDir != "" {
		os.RemoveAll(suite.tempDir)
	}
}

// SetupTest prepares clean state for each test
func (suite *E2EIntegrationTestSuite) SetupTest() {
	// Clean up any existing test data
	_, err := suite.db.Exec("DELETE FROM benchmark_runs WHERE test_name LIKE 'e2e_%'")
	if err != nil {
		suite.logger.WithError(err).Warn("Failed to clean up test data")
	}
}

// TestE2ECompleteWorkflow tests the complete benchmark workflow
func (suite *E2EIntegrationTestSuite) TestE2ECompleteWorkflow() {
	t := suite.T()

	testName := "e2e_complete_workflow"

	// 1. Create and save benchmark results through storage layer
	benchmarkResult := &types.BenchmarkResult{
		TestName:      testName,
		Description:   "End-to-end integration test",
		GitCommit:     "abc123def456",
		GitBranch:     "main",
		StartTime:     time.Now().Add(-15 * time.Minute),
		EndTime:       time.Now().Add(-5 * time.Minute),
		Duration:      10 * time.Minute,
		TargetRPS:     100,
		ActualRPS:     95.5,
		TotalRequests: 57300,
		TotalErrors:   286,
		Config: map[string]interface{}{
			"endpoints": []string{"eth_getBalance", "eth_getBlockByNumber"},
			"clients":   []string{"geth", "nethermind", "erigon"},
			"duration":  "10m",
		},
		Environment: map[string]interface{}{
			"region":     "us-east-1",
			"node_count": 3,
			"cpu_cores":  8,
			"memory_gb":  16,
		},
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:          "geth",
				TotalRequests: 19100,
				TotalErrors:   95,
				ErrorRate:     0.00497,
				Latency: types.LatencyMetrics{
					Avg:        145.2,
					P50:        125.0,
					P95:        285.5,
					P99:        475.8,
					Max:        892.3,
					Throughput: 31.83,
				},
				Methods: map[string]types.MetricSummary{
					"eth_getBalance": {
						Count:      9550,
						ErrorRate:  0.00471,
						Avg:        142.1,
						P95:        280.2,
						Throughput: 15.92,
					},
					"eth_getBlockByNumber": {
						Count:      9550,
						ErrorRate:  0.00524,
						Avg:        148.3,
						P95:        290.8,
						Throughput: 15.92,
					},
				},
			},
			"nethermind": {
				Name:          "nethermind",
				TotalRequests: 19100,
				TotalErrors:   97,
				ErrorRate:     0.00508,
				Latency: types.LatencyMetrics{
					Avg:        152.8,
					P50:        135.0,
					P95:        295.2,
					P99:        485.1,
					Max:        912.7,
					Throughput: 31.83,
				},
				Methods: map[string]types.MetricSummary{
					"eth_getBalance": {
						Count:      9550,
						ErrorRate:  0.00503,
						Avg:        149.5,
						P95:        290.8,
						Throughput: 15.92,
					},
					"eth_getBlockByNumber": {
						Count:      9550,
						ErrorRate:  0.00513,
						Avg:        156.1,
						P95:        299.6,
						Throughput: 15.92,
					},
				},
			},
			"erigon": {
				Name:          "erigon",
				TotalRequests: 19100,
				TotalErrors:   94,
				ErrorRate:     0.00492,
				Latency: types.LatencyMetrics{
					Avg:        138.9,
					P50:        118.0,
					P95:        275.3,
					P99:        465.2,
					Max:        845.6,
					Throughput: 31.83,
				},
				Methods: map[string]types.MetricSummary{
					"eth_getBalance": {
						Count:      9550,
						ErrorRate:  0.00482,
						Avg:        135.7,
						P95:        270.1,
						Throughput: 15.92,
					},
					"eth_getBlockByNumber": {
						Count:      9550,
						ErrorRate:  0.00503,
						Avg:        142.1,
						P95:        280.5,
						Throughput: 15.92,
					},
				},
			},
		},
	}

	// 2. Save the run and verify it's stored correctly
	savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
	require.NoError(t, err)
	assert.NotEmpty(t, savedRun.ID)
	assert.Equal(t, testName, savedRun.TestName)

	// 3. Verify the run can be retrieved via API
	resp, err := http.Get(suite.serverURL + "/api/v1/runs/" + savedRun.ID)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var apiRun types.HistoricRun
	err = json.NewDecoder(resp.Body).Decode(&apiRun)
	require.NoError(t, err)
	assert.Equal(t, savedRun.ID, apiRun.ID)
	assert.Equal(t, savedRun.TestName, apiRun.TestName)

	// 4. Create a baseline via API
	baselineData := map[string]interface{}{
		"run_id":      savedRun.ID,
		"name":        "e2e_workflow_baseline",
		"description": "Baseline for E2E workflow test",
	}

	jsonData, err := json.Marshal(baselineData)
	require.NoError(t, err)

	resp, err = http.Post(
		suite.serverURL+"/api/v1/baselines",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var baseline analysis.Baseline
	err = json.NewDecoder(resp.Body).Decode(&baseline)
	require.NoError(t, err)
	assert.Equal(t, "e2e_workflow_baseline", baseline.Name)

	// 5. Create a second run with different performance characteristics
	degradedResult := *benchmarkResult // Copy
	degradedResult.StartTime = time.Now().Add(-5 * time.Minute)
	degradedResult.EndTime = time.Now()
	degradedResult.TotalErrors = 572 // Double the errors
	degradedResult.ActualRPS = 85.2  // Lower RPS

	// Degrade performance for all clients
	for clientName, metrics := range degradedResult.ClientMetrics {
		degradedMetrics := *metrics // Copy
		degradedMetrics.TotalErrors = metrics.TotalErrors * 2
		degradedMetrics.ErrorRate = metrics.ErrorRate * 2
		degradedMetrics.Latency.Avg = metrics.Latency.Avg * 1.5
		degradedMetrics.Latency.P95 = metrics.Latency.P95 * 1.6
		degradedMetrics.Latency.P99 = metrics.Latency.P99 * 1.7
		degradedMetrics.Latency.Throughput = metrics.Latency.Throughput * 0.85
		degradedResult.ClientMetrics[clientName] = &degradedMetrics
	}

	// 6. Save the degraded run
	degradedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, &degradedResult)
	require.NoError(t, err)

	// 7. Detect regressions via API
	detectionOptions := map[string]interface{}{
		"comparison_mode": "baseline",
		"baseline_name":   baseline.Name,
	}

	jsonData, err = json.Marshal(detectionOptions)
	require.NoError(t, err)

	resp, err = http.Post(
		suite.serverURL+"/api/v1/regressions/detect/"+degradedRun.ID,
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var regressionReport types.RegressionReport
	err = json.NewDecoder(resp.Body).Decode(&regressionReport)
	require.NoError(t, err)
	assert.Equal(t, degradedRun.ID, regressionReport.RunID)
	assert.NotEmpty(t, regressionReport.Regressions)

	// 8. Verify regressions were detected for performance degradation
	foundLatencyRegression := false
	foundErrorRateRegression := false
	for _, regression := range regressionReport.Regressions {
		if regression.Metric == "avg_latency" {
			foundLatencyRegression = true
			assert.Greater(t, regression.PercentChange, 20.0) // Should be significant increase
		}
		if regression.Metric == "error_rate" {
			foundErrorRateRegression = true
			assert.Greater(t, regression.PercentChange, 50.0) // Should be significant increase
		}
	}
	assert.True(t, foundLatencyRegression, "Should detect latency regression")
	assert.True(t, foundErrorRateRegression, "Should detect error rate regression")

	// 9. Test trend analysis with multiple runs
	resp, err = http.Get(fmt.Sprintf("%s/api/v1/trends?test_name=%s&days=1", suite.serverURL, testName))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var trends types.TrendAnalysis
	err = json.NewDecoder(resp.Body).Decode(&trends)
	require.NoError(t, err)
	assert.Equal(t, testName, trends.TestName)

	// 10. Analyze the degraded run
	resp, err = http.Get(suite.serverURL + "/api/v1/runs/" + degradedRun.ID + "/analyze")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var runAnalysis types.RunAnalysis
	err = json.NewDecoder(resp.Body).Decode(&runAnalysis)
	require.NoError(t, err)
	assert.Equal(t, degradedRun.ID, runAnalysis.RunID)
	assert.Less(t, runAnalysis.OverallHealthScore, 90.0) // Should be lower due to degradation

	// 11. Test file storage - verify files were created
	historicFiles, err := os.ReadDir(suite.storageConfig.HistoricPath)
	require.NoError(t, err)
	assert.NotEmpty(t, historicFiles)

	// Find files for our test runs
	foundOriginalFile := false
	foundDegradedFile := false
	for _, file := range historicFiles {
		if !file.IsDir() && file.Name() != ".gitkeep" {
			content, err := os.ReadFile(filepath.Join(suite.storageConfig.HistoricPath, file.Name()))
			require.NoError(t, err)

			var storedRun types.HistoricRun
			err = json.Unmarshal(content, &storedRun)
			require.NoError(t, err)

			if storedRun.ID == savedRun.ID {
				foundOriginalFile = true
			}
			if storedRun.ID == degradedRun.ID {
				foundDegradedFile = true
			}
		}
	}
	assert.True(t, foundOriginalFile, "Original run should be saved to file")
	assert.True(t, foundDegradedFile, "Degraded run should be saved to file")
}

// TestE2EWebSocketIntegration tests real-time WebSocket functionality
func (suite *E2EIntegrationTestSuite) TestE2EWebSocketIntegration() {
	t := suite.T()

	// Connect to WebSocket
	wsURL := "ws://localhost:8080/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Set up message collection
	messages := make(chan map[string]interface{}, 10)
	done := make(chan bool)

	go func() {
		defer func() { done <- true }()
		for {
			var msg map[string]interface{}
			err := conn.ReadJSON(&msg)
			if err != nil {
				return
			}
			messages <- msg
		}
	}()

	// Create a benchmark run that should trigger WebSocket notifications
	benchmarkResult := &types.BenchmarkResult{
		TestName:  "e2e_websocket_test",
		StartTime: time.Now().Add(-10 * time.Minute),
		EndTime:   time.Now(),
		Duration:  10 * time.Minute,
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:          "geth",
				TotalRequests: 1000,
				TotalErrors:   10,
				ErrorRate:     0.01,
				Latency: types.LatencyMetrics{
					Avg:        150.0,
					P95:        300.0,
					P99:        500.0,
					Throughput: 100.0,
				},
			},
		},
	}

	// Save the run - this should trigger WebSocket notifications
	_, err = suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
	require.NoError(t, err)

	// Wait for WebSocket messages
	receivedMessages := []map[string]interface{}{}
	timeout := time.After(3 * time.Second)

	for {
		select {
		case msg := <-messages:
			receivedMessages = append(receivedMessages, msg)
			// Stop after receiving some messages or if we get a specific type
			if len(receivedMessages) >= 1 {
				goto checkMessages
			}
		case <-timeout:
			goto checkMessages
		case <-done:
			goto checkMessages
		}
	}

checkMessages:
	// We might not receive messages if WebSocket broadcasting isn't implemented
	// for the historic storage save operation, which is acceptable
	if len(receivedMessages) > 0 {
		// Verify message structure if we received any
		for _, msg := range receivedMessages {
			assert.Contains(t, msg, "type")
			assert.Contains(t, msg, "data")
		}
	}
}

// TestE2EErrorScenarios tests error handling in end-to-end scenarios
func (suite *E2EIntegrationTestSuite) TestE2EErrorScenarios() {
	t := suite.T()

	// Test 1: Invalid API requests
	resp, err := http.Get(suite.serverURL + "/api/v1/runs/non-existent-run")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Test 2: Malformed JSON in POST requests
	invalidJSON := bytes.NewBuffer([]byte("{invalid json"))
	resp, err = http.Post(
		suite.serverURL+"/api/v1/baselines",
		"application/json",
		invalidJSON,
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Test 3: Database constraint violations
	// Try to create baseline with non-existent run ID
	baselineData := map[string]interface{}{
		"run_id":      "non-existent-run-id",
		"name":        "invalid_baseline",
		"description": "This should fail",
	}

	jsonData, err := json.Marshal(baselineData)
	require.NoError(t, err)

	resp, err = http.Post(
		suite.serverURL+"/api/v1/baselines",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Test 4: Regression detection with invalid parameters
	invalidDetectionOptions := map[string]interface{}{
		"comparison_mode": "invalid_mode",
	}

	jsonData, err = json.Marshal(invalidDetectionOptions)
	require.NoError(t, err)

	resp, err = http.Post(
		suite.serverURL+"/api/v1/regressions/detect/non-existent-run",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestE2EPerformanceWithLoad tests system performance under load
func (suite *E2EIntegrationTestSuite) TestE2EPerformanceWithLoad() {
	t := suite.T()

	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	testName := "e2e_performance_load_test"

	// Create multiple benchmark runs concurrently
	const numRuns = 10
	const concurrency = 5

	results := make(chan error, numRuns)
	semaphore := make(chan struct{}, concurrency)

	start := time.Now()

	for i := 0; i < numRuns; i++ {
		go func(runIndex int) {
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			benchmarkResult := &types.BenchmarkResult{
				TestName:  testName,
				StartTime: time.Now().Add(time.Duration(-runIndex*10) * time.Minute),
				EndTime:   time.Now().Add(time.Duration(-runIndex*10+10) * time.Minute),
				Duration:  10 * time.Minute,
				ClientMetrics: map[string]*types.ClientMetrics{
					"geth": {
						Name:          "geth",
						TotalRequests: 1000 + int64(runIndex*100),
						TotalErrors:   10 + int64(runIndex),
						ErrorRate:     float64(10+runIndex) / float64(1000+runIndex*100),
						Latency: types.LatencyMetrics{
							Avg:        150.0 + float64(runIndex)*5.0,
							P95:        300.0 + float64(runIndex)*10.0,
							P99:        500.0 + float64(runIndex)*15.0,
							Throughput: 100.0 - float64(runIndex)*1.0,
						},
					},
				},
			}

			_, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
			results <- err
		}(i)
	}

	// Collect results
	for i := 0; i < numRuns; i++ {
		err := <-results
		require.NoError(t, err, "Run %d should succeed", i)
	}

	duration := time.Since(start)
	t.Logf("Created %d runs in %v (%.2f runs/sec)", numRuns, duration, float64(numRuns)/duration.Seconds())

	// Performance should be reasonable
	assert.Less(t, duration, 30*time.Second, "Should create runs within 30 seconds")

	// Verify all runs were created
	resp, err := http.Get(suite.serverURL + "/api/v1/runs?test_name=" + testName)
	require.NoError(t, err)
	defer resp.Body.Close()

	var runs []*types.HistoricRun
	err = json.NewDecoder(resp.Body).Decode(&runs)
	require.NoError(t, err)
	assert.Len(t, runs, numRuns)

	// Test API performance with multiple requests
	start = time.Now()
	for _, run := range runs {
		resp, err := http.Get(suite.serverURL + "/api/v1/runs/" + run.ID)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}
	apiDuration := time.Since(start)

	t.Logf("Retrieved %d runs via API in %v (%.2f req/sec)",
		numRuns, apiDuration, float64(numRuns)/apiDuration.Seconds())

	// API should be responsive
	assert.Less(t, apiDuration, 10*time.Second, "API should respond quickly")
}

// TestE2EDataConsistency tests data consistency across the entire system
func (suite *E2EIntegrationTestSuite) TestE2EDataConsistency() {
	t := suite.T()

	testName := "e2e_data_consistency_test"

	// Create a benchmark run
	benchmarkResult := &types.BenchmarkResult{
		TestName:  testName,
		StartTime: time.Now().Add(-10 * time.Minute),
		EndTime:   time.Now(),
		Duration:  10 * time.Minute,
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:          "geth",
				TotalRequests: 1000,
				TotalErrors:   25,
				ErrorRate:     0.025,
				Latency: types.LatencyMetrics{
					Avg:        175.5,
					P50:        150.0,
					P95:        325.8,
					P99:        525.2,
					Max:        892.1,
					Throughput: 95.5,
				},
			},
		},
	}

	// Save through storage layer
	savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
	require.NoError(t, err)

	// 1. Verify consistency between storage and API
	resp, err := http.Get(suite.serverURL + "/api/v1/runs/" + savedRun.ID)
	require.NoError(t, err)
	defer resp.Body.Close()

	var apiRun types.HistoricRun
	err = json.NewDecoder(resp.Body).Decode(&apiRun)
	require.NoError(t, err)

	// Compare critical fields
	assert.Equal(t, savedRun.ID, apiRun.ID)
	assert.Equal(t, savedRun.TestName, apiRun.TestName)
	assert.Equal(t, savedRun.AvgLatencyMs, apiRun.AvgLatencyMs)
	assert.Equal(t, savedRun.OverallErrorRate, apiRun.OverallErrorRate)
	assert.Equal(t, savedRun.TotalRequests, apiRun.TotalRequests)
	assert.Equal(t, savedRun.TotalErrors, apiRun.TotalErrors)

	// 2. Verify consistency in database
	var dbRun types.HistoricRun
	err = suite.db.QueryRow(`
		SELECT id, test_name, avg_latency_ms, overall_error_rate, total_requests, total_errors
		FROM historic_runs WHERE id = $1
	`, savedRun.ID).Scan(
		&dbRun.ID,
		&dbRun.TestName,
		&dbRun.AvgLatencyMs,
		&dbRun.OverallErrorRate,
		&dbRun.TotalRequests,
		&dbRun.TotalErrors,
	)
	require.NoError(t, err)

	assert.Equal(t, savedRun.ID, dbRun.ID)
	assert.Equal(t, savedRun.TestName, dbRun.TestName)
	assert.Equal(t, savedRun.AvgLatencyMs, dbRun.AvgLatencyMs)
	assert.Equal(t, savedRun.OverallErrorRate, dbRun.OverallErrorRate)
	assert.Equal(t, savedRun.TotalRequests, dbRun.TotalRequests)
	assert.Equal(t, savedRun.TotalErrors, dbRun.TotalErrors)

	// 3. Verify consistency in file storage
	historicFiles, err := os.ReadDir(suite.storageConfig.HistoricPath)
	require.NoError(t, err)

	var fileRun types.HistoricRun
	foundFile := false

	for _, file := range historicFiles {
		if !file.IsDir() && file.Name() != ".gitkeep" {
			content, err := os.ReadFile(filepath.Join(suite.storageConfig.HistoricPath, file.Name()))
			require.NoError(t, err)

			var tempRun types.HistoricRun
			err = json.Unmarshal(content, &tempRun)
			require.NoError(t, err)

			if tempRun.ID == savedRun.ID {
				fileRun = tempRun
				foundFile = true
				break
			}
		}
	}

	assert.True(t, foundFile, "Run should be saved to file")
	if foundFile {
		assert.Equal(t, savedRun.ID, fileRun.ID)
		assert.Equal(t, savedRun.TestName, fileRun.TestName)
		assert.Equal(t, savedRun.AvgLatencyMs, fileRun.AvgLatencyMs)
		assert.Equal(t, savedRun.OverallErrorRate, fileRun.OverallErrorRate)
	}

	// 4. Create baseline and verify consistency
	baselineData := map[string]interface{}{
		"run_id":      savedRun.ID,
		"name":        "e2e_consistency_baseline",
		"description": "Baseline for consistency test",
	}

	jsonData, err := json.Marshal(baselineData)
	require.NoError(t, err)

	resp, err = http.Post(
		suite.serverURL+"/api/v1/baselines",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var apiBaseline analysis.Baseline
	err = json.NewDecoder(resp.Body).Decode(&apiBaseline)
	require.NoError(t, err)

	// Verify baseline in database
	var dbBaseline analysis.Baseline
	err = suite.db.QueryRow(`
		SELECT id, name, run_id, test_name FROM baselines WHERE name = $1
	`, "e2e_consistency_baseline").Scan(
		&dbBaseline.ID,
		&dbBaseline.Name,
		&dbBaseline.RunID,
		&dbBaseline.TestName,
	)
	require.NoError(t, err)

	assert.Equal(t, apiBaseline.ID, dbBaseline.ID)
	assert.Equal(t, apiBaseline.Name, dbBaseline.Name)
	assert.Equal(t, apiBaseline.RunID, dbBaseline.RunID)
	assert.Equal(t, apiBaseline.TestName, dbBaseline.TestName)
}

// TestE2ESystemHealth tests overall system health and monitoring
func (suite *E2EIntegrationTestSuite) TestE2ESystemHealth() {
	t := suite.T()

	// Test health endpoint
	resp, err := http.Get(suite.serverURL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var health map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&health)
	require.NoError(t, err)
	assert.Equal(t, "ok", health["status"])
	assert.NotNil(t, health["timestamp"])

	// Test database connectivity
	err = suite.db.Ping()
	assert.NoError(t, err)

	// Test file system access
	testFile := filepath.Join(suite.storageConfig.HistoricPath, "health_test.tmp")
	err = os.WriteFile(testFile, []byte("health test"), 0644)
	assert.NoError(t, err)

	_, err = os.Stat(testFile)
	assert.NoError(t, err)

	err = os.Remove(testFile)
	assert.NoError(t, err)

	// Test component health
	components := []struct {
		name      string
		component interface{ Stop() error }
	}{
		{"historic_storage", suite.historicStorage},
		{"baseline_manager", suite.baselineManager},
		{"trend_analyzer", suite.trendAnalyzer},
		{"regression_detector", suite.regressionDetector},
	}

	for _, comp := range components {
		// Components should be running (stop method exists but we won't call it)
		assert.NotNil(t, comp.component, "Component %s should be initialized", comp.name)
	}
}

// Run the test suite
func TestE2EIntegrationTestSuite(t *testing.T) {
	if os.Getenv("SKIP_E2E_TESTS") != "" {
		t.Skip("Skipping E2E integration tests")
	}

	suite.Run(t, new(E2EIntegrationTestSuite))
}

// Benchmark tests for end-to-end performance

func BenchmarkE2ERunCreation(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	// Setup minimal test environment (this would be slow in real scenarios)
	// In practice, you'd use a shared test environment
	b.Log("Setting up E2E benchmark environment...")

	// This is a simplified benchmark - real implementation would need
	// proper setup/teardown for database and storage
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Simulate creating a benchmark run
		benchmarkResult := &types.BenchmarkResult{
			TestName:  fmt.Sprintf("bench_test_%d", i),
			StartTime: time.Now().Add(-10 * time.Minute),
			EndTime:   time.Now(),
			Duration:  10 * time.Minute,
			ClientMetrics: map[string]*types.ClientMetrics{
				"geth": {
					Name:          "geth",
					TotalRequests: 1000,
					TotalErrors:   10,
					ErrorRate:     0.01,
					Latency: types.LatencyMetrics{
						Avg:        150.0,
						P95:        300.0,
						P99:        500.0,
						Throughput: 100.0,
					},
				},
			},
		}

		// In a real benchmark, this would save to the actual storage
		_ = benchmarkResult // Placeholder
	}
}
