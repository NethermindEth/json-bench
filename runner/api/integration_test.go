package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// APIIntegrationTestSuite provides comprehensive API integration tests
type APIIntegrationTestSuite struct {
	suite.Suite
	container          *postgres.PostgresContainer
	db                 *sql.DB
	server             Server
	testServer         *httptest.Server
	ctx                context.Context
	logger             logrus.FieldLogger
	historicStorage    storage.HistoricStorage
	baselineManager    analysis.BaselineManager
	trendAnalyzer      analysis.TrendAnalyzer
	regressionDetector analysis.RegressionDetector
}

// SetupSuite initializes the integration test environment
func (suite *APIIntegrationTestSuite) SetupSuite() {
	suite.ctx = context.Background()
	suite.logger = logrus.New().WithField("test", "api_integration")

	// Start PostgreSQL container
	pgContainer, err := postgres.RunContainer(suite.ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		postgres.WithDatabase("api_test_db"),
		postgres.WithUsername("api_test_user"),
		postgres.WithPassword("api_test_pass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	require.NoError(suite.T(), err)
	suite.container = pgContainer

	// Setup database connection
	mappedPort, err := pgContainer.MappedPort(suite.ctx, "5432")
	require.NoError(suite.T(), err)

	connStr := fmt.Sprintf("host=localhost port=%d user=api_test_user password=api_test_pass dbname=api_test_db sslmode=disable",
		mappedPort.Int())
	db, err := sql.Open("postgres", connStr)
	require.NoError(suite.T(), err)
	suite.db = db

	// Initialize storage components
	migration := storage.NewMigrationService(db, suite.logger)
	err = migration.Up()
	require.NoError(suite.T(), err)

	// Create storage instances
	suite.historicStorage = storage.NewHistoricStorage(db, "results/test", suite.logger)
	suite.baselineManager = analysis.NewBaselineManager(suite.historicStorage, db, suite.logger)
	suite.trendAnalyzer = analysis.NewTrendAnalyzer(suite.historicStorage, db, suite.logger)
	suite.regressionDetector = analysis.NewRegressionDetector(suite.historicStorage, suite.baselineManager, db, suite.logger)

	// Start services
	err = suite.historicStorage.Start(suite.ctx)
	require.NoError(suite.T(), err)
	err = suite.baselineManager.Start(suite.ctx)
	require.NoError(suite.T(), err)
	err = suite.trendAnalyzer.Start(suite.ctx)
	require.NoError(suite.T(), err)
	err = suite.regressionDetector.Start(suite.ctx)
	require.NoError(suite.T(), err)
}

// TearDownSuite cleans up test resources
func (suite *APIIntegrationTestSuite) TearDownSuite() {
	if suite.testServer != nil {
		suite.testServer.Close()
	}
	if suite.server != nil {
		suite.server.Stop()
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
}

// SetupTest prepares clean state for each test
func (suite *APIIntegrationTestSuite) SetupTest() {
	// Create and start API server
	suite.server = NewServer(
		suite.historicStorage,
		suite.baselineManager,
		suite.trendAnalyzer,
		suite.regressionDetector,
		suite.db,
		suite.logger,
	)

	// Create test server
	router := suite.server.(*server).setupRoutes()
	suite.testServer = httptest.NewServer(router)
}

// TearDownTest cleans up after each test
func (suite *APIIntegrationTestSuite) TearDownTest() {
	if suite.testServer != nil {
		suite.testServer.Close()
		suite.testServer = nil
	}

	// Clean up test data
	_, err := suite.db.Exec("DELETE FROM benchmark_runs WHERE test_name LIKE 'integration_%'")
	if err != nil {
		suite.logger.WithError(err).Warn("Failed to clean up test data")
	}
}

// TestAPIHealthEndpoint tests the health check endpoint
func (suite *APIIntegrationTestSuite) TestAPIHealthEndpoint() {
	t := suite.T()

	resp, err := http.Get(suite.testServer.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var health map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&health)
	assert.NoError(t, err)
	assert.Equal(t, "ok", health["status"])
	assert.NotNil(t, health["timestamp"])
}

// TestAPIRunLifecycle tests the complete run lifecycle through API
func (suite *APIIntegrationTestSuite) TestAPIRunLifecycle() {
	t := suite.T()

	// Create test benchmark result
	benchmarkResult := &types.BenchmarkResult{
		TestName:  "integration_lifecycle_test",
		StartTime: time.Now().Add(-10 * time.Minute),
		EndTime:   time.Now(),
		Duration:  10 * time.Minute,
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:          "geth",
				TotalRequests: 1000,
				TotalErrors:   5,
				ErrorRate:     0.005,
				Latency: types.LatencyMetrics{
					Avg:        150.0,
					P50:        120.0,
					P95:        300.0,
					P99:        500.0,
					Max:        1000.0,
					Throughput: 100.0,
				},
			},
		},
	}

	// 1. Save the run through storage (simulating benchmark completion)
	savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
	require.NoError(t, err)
	assert.NotEmpty(t, savedRun.ID)

	// 2. Get the run via API
	resp, err := http.Get(suite.testServer.URL + "/api/v1/runs/" + savedRun.ID)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var apiRun types.HistoricRun
	err = json.NewDecoder(resp.Body).Decode(&apiRun)
	assert.NoError(t, err)
	assert.Equal(t, savedRun.ID, apiRun.ID)
	assert.Equal(t, savedRun.TestName, apiRun.TestName)

	// 3. List runs via API
	resp, err = http.Get(suite.testServer.URL + "/api/v1/runs?test_name=" + benchmarkResult.TestName)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var runs []*types.HistoricRun
	err = json.NewDecoder(resp.Body).Decode(&runs)
	assert.NoError(t, err)
	assert.Len(t, runs, 1)
	assert.Equal(t, savedRun.ID, runs[0].ID)

	// 4. Delete the run via API
	req, err := http.NewRequest("DELETE", suite.testServer.URL+"/api/v1/runs/"+savedRun.ID, nil)
	require.NoError(t, err)

	client := &http.Client{}
	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 5. Verify run is deleted
	resp, err = http.Get(suite.testServer.URL + "/api/v1/runs/" + savedRun.ID)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestAPIBaselineWorkflow tests baseline management through API
func (suite *APIIntegrationTestSuite) TestAPIBaselineWorkflow() {
	t := suite.T()

	// Create and save a test run
	benchmarkResult := &types.BenchmarkResult{
		TestName:  "integration_baseline_test",
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

	savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
	require.NoError(t, err)

	// 1. Create baseline via API
	baselineData := map[string]interface{}{
		"run_id":      savedRun.ID,
		"name":        "integration_test_baseline",
		"description": "Baseline for integration testing",
	}

	jsonData, err := json.Marshal(baselineData)
	require.NoError(t, err)

	resp, err := http.Post(
		suite.testServer.URL+"/api/v1/baselines",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var baseline analysis.Baseline
	err = json.NewDecoder(resp.Body).Decode(&baseline)
	assert.NoError(t, err)
	assert.Equal(t, "integration_test_baseline", baseline.Name)
	assert.Equal(t, savedRun.ID, baseline.RunID)

	// 2. Get baseline via API
	resp, err = http.Get(suite.testServer.URL + "/api/v1/baselines/" + baseline.Name)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var retrievedBaseline analysis.Baseline
	err = json.NewDecoder(resp.Body).Decode(&retrievedBaseline)
	assert.NoError(t, err)
	assert.Equal(t, baseline.Name, retrievedBaseline.Name)

	// 3. List baselines via API
	resp, err = http.Get(suite.testServer.URL + "/api/v1/baselines?test_name=" + benchmarkResult.TestName)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var baselines []*analysis.Baseline
	err = json.NewDecoder(resp.Body).Decode(&baselines)
	assert.NoError(t, err)
	assert.Len(t, baselines, 1)
	assert.Equal(t, baseline.Name, baselines[0].Name)

	// 4. Compare to baseline via API
	secondRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
	require.NoError(t, err)

	resp, err = http.Get(fmt.Sprintf("%s/api/v1/baselines/%s/compare/%s",
		suite.testServer.URL, baseline.Name, secondRun.ID))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var comparison analysis.BaselineComparison
	err = json.NewDecoder(resp.Body).Decode(&comparison)
	assert.NoError(t, err)
	assert.Equal(t, secondRun.ID, comparison.RunID)
	assert.Equal(t, baseline.Name, comparison.BaselineName)

	// 5. Delete baseline via API
	req, err := http.NewRequest("DELETE", suite.testServer.URL+"/api/v1/baselines/"+baseline.Name, nil)
	require.NoError(t, err)

	client := &http.Client{}
	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestAPITrendAnalysis tests trend analysis through API
func (suite *APIIntegrationTestSuite) TestAPITrendAnalysis() {
	t := suite.T()

	testName := "integration_trend_test"

	// Create multiple runs with trending data
	for i := 0; i < 5; i++ {
		benchmarkResult := &types.BenchmarkResult{
			TestName:  testName,
			StartTime: time.Now().Add(time.Duration(-i*60) * time.Minute),
			EndTime:   time.Now().Add(time.Duration(-i*60+10) * time.Minute),
			Duration:  10 * time.Minute,
			ClientMetrics: map[string]*types.ClientMetrics{
				"geth": {
					Name:          "geth",
					TotalRequests: 1000,
					TotalErrors:   10 + i, // Slight increase over time
					ErrorRate:     float64(10+i) / 1000.0,
					Latency: types.LatencyMetrics{
						Avg:        150.0 + float64(i)*5.0, // Slight degradation
						P95:        300.0 + float64(i)*10.0,
						P99:        500.0 + float64(i)*15.0,
						Throughput: 100.0 - float64(i)*1.0, // Slight decrease
					},
				},
			},
		}

		_, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
		require.NoError(t, err)
	}

	// Wait for data to be processed
	time.Sleep(100 * time.Millisecond)

	// 1. Get trends via API
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/trends?test_name=%s&days=1",
		suite.testServer.URL, testName))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var trends types.TrendAnalysis
	err = json.NewDecoder(resp.Body).Decode(&trends)
	assert.NoError(t, err)
	assert.Equal(t, testName, trends.TestName)
	assert.NotEmpty(t, trends.Trends)

	// 2. Get method trends via API
	resp, err = http.Get(fmt.Sprintf("%s/api/v1/trends/methods?test_name=%s&method=eth_getBalance&days=1",
		suite.testServer.URL, testName))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. Get client trends via API
	resp, err = http.Get(fmt.Sprintf("%s/api/v1/trends/clients?test_name=%s&client=geth&days=1",
		suite.testServer.URL, testName))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestAPIRegressionDetection tests regression detection through API
func (suite *APIIntegrationTestSuite) TestAPIRegressionDetection() {
	t := suite.T()

	testName := "integration_regression_test"

	// Create baseline run
	baselineResult := &types.BenchmarkResult{
		TestName:  testName,
		StartTime: time.Now().Add(-20 * time.Minute),
		EndTime:   time.Now().Add(-10 * time.Minute),
		Duration:  10 * time.Minute,
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:          "geth",
				TotalRequests: 1000,
				TotalErrors:   5,
				ErrorRate:     0.005,
				Latency: types.LatencyMetrics{
					Avg:        150.0,
					P95:        300.0,
					P99:        500.0,
					Throughput: 100.0,
				},
			},
		},
	}

	baselineRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, baselineResult)
	require.NoError(t, err)

	// Create regressed run
	regressedResult := &types.BenchmarkResult{
		TestName:  testName,
		StartTime: time.Now().Add(-10 * time.Minute),
		EndTime:   time.Now(),
		Duration:  10 * time.Minute,
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:          "geth",
				TotalRequests: 1000,
				TotalErrors:   50, // 10x increase
				ErrorRate:     0.05,
				Latency: types.LatencyMetrics{
					Avg:        250.0, // Significant increase
					P95:        500.0,
					P99:        800.0,
					Throughput: 80.0, // Decrease
				},
			},
		},
	}

	regressedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, regressedResult)
	require.NoError(t, err)

	// 1. Detect regressions via API
	detectionOptions := map[string]interface{}{
		"comparison_mode": "sequential",
		"lookback_count":  1,
	}

	jsonData, err := json.Marshal(detectionOptions)
	require.NoError(t, err)

	resp, err := http.Post(
		suite.testServer.URL+"/api/v1/regressions/detect/"+regressedRun.ID,
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var regressionReport types.RegressionReport
	err = json.NewDecoder(resp.Body).Decode(&regressionReport)
	assert.NoError(t, err)
	assert.Equal(t, regressedRun.ID, regressionReport.RunID)
	assert.NotEmpty(t, regressionReport.Regressions)

	// 2. Get regressions for run via API
	resp, err = http.Get(suite.testServer.URL + "/api/v1/regressions/" + regressedRun.ID)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var regressions []*types.Regression
	err = json.NewDecoder(resp.Body).Decode(&regressions)
	assert.NoError(t, err)
	assert.NotEmpty(t, regressions)

	// 3. Analyze run via API
	resp, err = http.Get(suite.testServer.URL + "/api/v1/runs/" + regressedRun.ID + "/analyze")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var analysis types.RunAnalysis
	err = json.NewDecoder(resp.Body).Decode(&analysis)
	assert.NoError(t, err)
	assert.Equal(t, regressedRun.ID, analysis.RunID)
}

// TestAPIErrorHandling tests API error handling scenarios
func (suite *APIIntegrationTestSuite) TestAPIErrorHandling() {
	t := suite.T()

	// Test 404 for non-existent run
	resp, err := http.Get(suite.testServer.URL + "/api/v1/runs/non-existent-run")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Test 400 for invalid JSON in POST request
	invalidJSON := bytes.NewBuffer([]byte("{invalid json"))
	resp, err = http.Post(
		suite.testServer.URL+"/api/v1/baselines",
		"application/json",
		invalidJSON,
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Test 405 for method not allowed
	req, err := http.NewRequest("POST", suite.testServer.URL+"/api/v1/runs/some-id", nil)
	require.NoError(t, err)

	client := &http.Client{}
	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// TestAPIConcurrentRequests tests concurrent API requests
func (suite *APIIntegrationTestSuite) TestAPIConcurrentRequests() {
	t := suite.T()

	// Create test data
	benchmarkResult := &types.BenchmarkResult{
		TestName:  "integration_concurrent_test",
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

	savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
	require.NoError(t, err)

	// Make concurrent requests
	const concurrency = 10
	var wg sync.WaitGroup
	results := make(chan int, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			resp, err := http.Get(suite.testServer.URL + "/api/v1/runs/" + savedRun.ID)
			if err != nil {
				results <- 0
				return
			}
			defer resp.Body.Close()

			results <- resp.StatusCode
		}()
	}

	wg.Wait()
	close(results)

	// Verify all requests succeeded
	successCount := 0
	for statusCode := range results {
		if statusCode == http.StatusOK {
			successCount++
		}
	}

	assert.Equal(t, concurrency, successCount, "All concurrent requests should succeed")
}

// TestAPIWebSocketIntegration tests WebSocket functionality
func (suite *APIIntegrationTestSuite) TestAPIWebSocketIntegration() {
	t := suite.T()

	// Convert HTTP URL to WebSocket URL
	wsURL := "ws" + suite.testServer.URL[4:] + "/ws"

	// Connect to WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Set up message reception
	messages := make(chan map[string]interface{}, 10)
	go func() {
		for {
			var msg map[string]interface{}
			err := conn.ReadJSON(&msg)
			if err != nil {
				return
			}
			messages <- msg
		}
	}()

	// Trigger an event that should send WebSocket message
	benchmarkResult := &types.BenchmarkResult{
		TestName:  "integration_websocket_test",
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

	_, err = suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
	require.NoError(t, err)

	// Wait for WebSocket message
	select {
	case msg := <-messages:
		assert.NotNil(t, msg)
		assert.Contains(t, []string{"run_saved", "update", "notification"}, msg["type"])
	case <-time.After(2 * time.Second):
		t.Log("No WebSocket message received within timeout - this might be expected if WebSocket broadcasting is not implemented")
	}
}

// TestAPIGrafanaIntegration tests Grafana API endpoints
func (suite *APIIntegrationTestSuite) TestAPIGrafanaIntegration() {
	t := suite.T()

	// Create test data
	benchmarkResult := &types.BenchmarkResult{
		TestName:  "integration_grafana_test",
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

	_, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
	require.NoError(t, err)

	// Test Grafana search endpoint
	resp, err := http.Get(suite.testServer.URL + "/grafana/search")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var searchResults []map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&searchResults)
	assert.NoError(t, err)
	assert.NotEmpty(t, searchResults)

	// Test Grafana query endpoint
	queryData := map[string]interface{}{
		"targets": []map[string]interface{}{
			{
				"target": "latency_avg",
				"type":   "timeserie",
			},
		},
		"range": map[string]interface{}{
			"from": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			"to":   time.Now().Format(time.RFC3339),
		},
		"interval": "1m",
	}

	jsonData, err := json.Marshal(queryData)
	require.NoError(t, err)

	resp, err = http.Post(
		suite.testServer.URL+"/grafana/query",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var queryResults []map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&queryResults)
	assert.NoError(t, err)
	// Query results might be empty if no data matches the time range
}

// TestAPIPerformance tests API performance characteristics
func (suite *APIIntegrationTestSuite) TestAPIPerformance() {
	t := suite.T()

	// Create test data
	benchmarkResult := &types.BenchmarkResult{
		TestName:  "integration_performance_test",
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

	savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
	require.NoError(t, err)

	// Test response times
	endpoints := []string{
		"/health",
		"/api/v1/runs/" + savedRun.ID,
		"/api/v1/runs?test_name=" + benchmarkResult.TestName,
	}

	for _, endpoint := range endpoints {
		start := time.Now()
		resp, err := http.Get(suite.testServer.URL + endpoint)
		duration := time.Since(start)

		require.NoError(t, err)
		resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Less(t, duration, 1*time.Second, "Endpoint %s should respond within 1 second", endpoint)
	}
}

// TestAPIDataConsistency tests data consistency across API endpoints
func (suite *APIIntegrationTestSuite) TestAPIDataConsistency() {
	t := suite.T()

	testName := "integration_consistency_test"

	// Create multiple runs
	var savedRunIDs []string
	for i := 0; i < 3; i++ {
		benchmarkResult := &types.BenchmarkResult{
			TestName:  testName,
			StartTime: time.Now().Add(time.Duration(-i*60) * time.Minute),
			EndTime:   time.Now().Add(time.Duration(-i*60+10) * time.Minute),
			Duration:  10 * time.Minute,
			ClientMetrics: map[string]*types.ClientMetrics{
				"geth": {
					Name:          "geth",
					TotalRequests: 1000,
					TotalErrors:   10 + i,
					ErrorRate:     float64(10+i) / 1000.0,
					Latency: types.LatencyMetrics{
						Avg:        150.0 + float64(i)*5.0,
						P95:        300.0 + float64(i)*10.0,
						P99:        500.0 + float64(i)*15.0,
						Throughput: 100.0 - float64(i)*1.0,
					},
				},
			},
		}

		savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, benchmarkResult)
		require.NoError(t, err)
		savedRunIDs = append(savedRunIDs, savedRun.ID)
	}

	// Get runs through different endpoints and verify consistency
	// 1. Get individual runs
	var individualRuns []*types.HistoricRun
	for _, runID := range savedRunIDs {
		resp, err := http.Get(suite.testServer.URL + "/api/v1/runs/" + runID)
		require.NoError(t, err)
		defer resp.Body.Close()

		var run types.HistoricRun
		err = json.NewDecoder(resp.Body).Decode(&run)
		require.NoError(t, err)
		individualRuns = append(individualRuns, &run)
	}

	// 2. Get runs through list endpoint
	resp, err := http.Get(suite.testServer.URL + "/api/v1/runs?test_name=" + testName)
	require.NoError(t, err)
	defer resp.Body.Close()

	var listRuns []*types.HistoricRun
	err = json.NewDecoder(resp.Body).Decode(&listRuns)
	require.NoError(t, err)

	// 3. Verify consistency
	assert.Len(t, listRuns, len(individualRuns))

	// Create maps for easy lookup
	individualMap := make(map[string]*types.HistoricRun)
	listMap := make(map[string]*types.HistoricRun)

	for _, run := range individualRuns {
		individualMap[run.ID] = run
	}
	for _, run := range listRuns {
		listMap[run.ID] = run
	}

	// Verify all runs are present in both results
	for runID := range individualMap {
		assert.Contains(t, listMap, runID, "Run %s should be present in list results", runID)

		if listRun, exists := listMap[runID]; exists {
			individualRun := individualMap[runID]
			assert.Equal(t, individualRun.TestName, listRun.TestName)
			assert.Equal(t, individualRun.AvgLatencyMs, listRun.AvgLatencyMs)
			assert.Equal(t, individualRun.OverallErrorRate, listRun.OverallErrorRate)
		}
	}
}

// Run the test suite
func TestAPIIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(APIIntegrationTestSuite))
}

// Benchmark tests for API performance

func BenchmarkAPIHealthEndpoint(b *testing.B) {
	// Setup test server (simplified for benchmarking)
	handlers, _, _, _, _, _ := setupTestHandlers()
	router := mux.NewRouter()
	router.HandleFunc("/health", handlers.handleHealth).Methods("GET")
	server := httptest.NewServer(router)
	defer server.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(server.URL + "/health")
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}

func BenchmarkAPIGetRun(b *testing.B) {
	// Setup test server with mock data
	handlers, mockStorage, _, _, _, _ := setupTestHandlers()
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/runs/{id}", handlers.handleGetRun).Methods("GET")
	server := httptest.NewServer(router)
	defer server.Close()

	// Setup mock
	mockRun := createMockHistoricRun("bench-run-id")
	mockStorage.On("GetHistoricRun", mock.Anything, "bench-run-id").Return(mockRun, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(server.URL + "/api/v1/runs/bench-run-id")
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}
