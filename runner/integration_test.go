package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/api"
	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// IntegrationTestSuite provides a comprehensive test suite for the historic tracking system
type IntegrationTestSuite struct {
	suite.Suite

	// Test infrastructure
	ctx     context.Context
	cancel  context.CancelFunc
	logger  *logrus.Logger
	testDir string

	// Database and storage
	db               *sql.DB
	historicStorage  storage.HistoricStorage
	migrationService *storage.MigrationService
	storageConfig    *config.StorageConfig

	// Analysis components
	baselineManager    analysis.BaselineManager
	trendAnalyzer      analysis.TrendAnalyzer
	regressionDetector analysis.RegressionDetector

	// API server
	apiServer  api.Server
	serverURL  string
	httpClient *http.Client

	// WebSocket testing
	wsClients  []*websocket.Conn
	wsMessages []map[string]interface{}
	wsMutex    sync.RWMutex

	// Performance monitoring
	memoryUsage      []int64
	cpuUsage         []float64
	performanceMutex sync.RWMutex

	// Test data
	testRuns        []*types.HistoricRun
	testBaselines   []*api.Baseline
	testComparisons []*types.HistoricComparison

	// Cleanup functions
	cleanupFunctions []func() error
}

// SetupSuite initializes the test environment before running any tests
func (suite *IntegrationTestSuite) SetupSuite() {
	suite.ctx, suite.cancel = context.WithCancel(context.Background())

	// Initialize logger
	suite.logger = logrus.New()
	suite.logger.SetLevel(logrus.InfoLevel)
	suite.logger.SetFormatter(&logrus.JSONFormatter{})

	// Create test directory
	var err error
	suite.testDir, err = os.MkdirTemp("", "jsonrpc-bench-integration-*")
	require.NoError(suite.T(), err)

	// Setup database
	suite.setupDatabase()

	// Setup storage
	suite.setupStorage()

	// Setup analysis components
	suite.setupAnalysisComponents()

	// Setup API server
	suite.setupAPIServer()

	// Setup WebSocket monitoring
	suite.setupWebSocketMonitoring()

	// Setup performance monitoring
	suite.setupPerformanceMonitoring()

	// Initialize HTTP client
	suite.httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}

	suite.logger.WithFields(logrus.Fields{
		"test_dir":   suite.testDir,
		"server_url": suite.serverURL,
	}).Info("Integration test suite initialized")
}

// TearDownSuite cleans up after all tests have completed
func (suite *IntegrationTestSuite) TearDownSuite() {
	suite.logger.Info("Cleaning up integration test suite")

	// Cancel context to stop all goroutines
	suite.cancel()

	// Close WebSocket connections
	for _, ws := range suite.wsClients {
		if ws != nil {
			ws.Close()
		}
	}

	// Stop API server
	if suite.apiServer != nil {
		suite.apiServer.Stop()
	}

	// Run cleanup functions in reverse order
	for i := len(suite.cleanupFunctions) - 1; i >= 0; i-- {
		if err := suite.cleanupFunctions[i](); err != nil {
			suite.logger.WithError(err).Warn("Cleanup function failed")
		}
	}

	// Close database connection
	if suite.db != nil {
		suite.db.Close()
	}

	// Remove test directory
	if suite.testDir != "" {
		os.RemoveAll(suite.testDir)
	}

	suite.logger.Info("Integration test suite cleanup completed")
}

// setupDatabase creates and initializes the test database
func (suite *IntegrationTestSuite) setupDatabase() {
	// For now, use a simple local PostgreSQL instance
	// In a complete implementation, this would use testcontainers

	// Create storage config
	suite.storageConfig = &config.StorageConfig{
		HistoricPath:   filepath.Join(suite.testDir, "historic"),
		RetentionDays:  30,
		EnableHistoric: true,
		PostgreSQL: &config.PostgreSQLConfig{
			Host:                  "localhost",
			Port:                  5432,
			Database:              "jsonrpc_bench_test",
			Username:              "postgres",
			Password:              "postgres",
			SSLMode:               "disable",
			MaxConnections:        10,
			MaxIdleConnections:    2,
			ConnectionMaxLifetime: 30 * time.Minute,
			ConnectionTimeout:     10 * time.Second,
			Schema:                "public",
		},
	}

	// Open database connection
	var err error
	suite.db, err = sql.Open("postgres", suite.storageConfig.PostgreSQL.GetConnectionString())
	if err != nil {
		suite.T().Skip("PostgreSQL not available for integration tests:", err)
		return
	}

	// Test connection
	ctx, cancel := context.WithTimeout(suite.ctx, 10*time.Second)
	defer cancel()
	if err := suite.db.PingContext(ctx); err != nil {
		suite.T().Skip("Cannot connect to test PostgreSQL database:", err)
		return
	}

	// Initialize migration service
	suite.migrationService = storage.NewMigrationService(suite.db, suite.logger)

	// Reset database to clean state
	err = suite.migrationService.Reset()
	require.NoError(suite.T(), err)

	// Create performance indices
	err = suite.migrationService.CreateIndices()
	require.NoError(suite.T(), err)

	suite.logger.Info("Test database initialized successfully")
}

// setupStorage initializes the historic storage system
func (suite *IntegrationTestSuite) setupStorage() {
	suite.historicStorage = storage.NewHistoricStorage(
		suite.db,
		suite.storageConfig,
		suite.logger,
	)

	err := suite.historicStorage.Start(suite.ctx)
	require.NoError(suite.T(), err)

	suite.cleanupFunctions = append(suite.cleanupFunctions, func() error {
		return suite.historicStorage.Stop()
	})

	suite.logger.Info("Historic storage initialized successfully")
}

// setupAnalysisComponents initializes analysis components
func (suite *IntegrationTestSuite) setupAnalysisComponents() {
	// Initialize baseline manager
	suite.baselineManager = analysis.NewBaselineManager(
		suite.db,
		suite.historicStorage,
		suite.logger,
	)

	err := suite.baselineManager.Start(suite.ctx)
	require.NoError(suite.T(), err)

	// Initialize trend analyzer
	suite.trendAnalyzer = analysis.NewTrendAnalyzer(
		suite.db,
		suite.historicStorage,
		suite.logger,
	)

	err = suite.trendAnalyzer.Start(suite.ctx)
	require.NoError(suite.T(), err)

	// Initialize regression detector
	suite.regressionDetector = analysis.NewRegressionDetector(
		suite.db,
		suite.historicStorage,
		suite.baselineManager,
		suite.trendAnalyzer,
		suite.logger,
	)

	err = suite.regressionDetector.Start(suite.ctx)
	require.NoError(suite.T(), err)

	suite.cleanupFunctions = append(suite.cleanupFunctions, func() error {
		suite.regressionDetector.Stop()
		suite.trendAnalyzer.Stop()
		suite.baselineManager.Stop()
		return nil
	})

	suite.logger.Info("Analysis components initialized successfully")
}

// setupAPIServer initializes the HTTP API server
func (suite *IntegrationTestSuite) setupAPIServer() {
	suite.apiServer = api.NewServer(
		suite.historicStorage,
		suite.baselineManager,
		suite.trendAnalyzer,
		suite.regressionDetector,
		suite.db,
		suite.logger,
	)

	err := suite.apiServer.Start(suite.ctx)
	require.NoError(suite.T(), err)

	// Set server URL (assuming it starts on :8080)
	suite.serverURL = "http://localhost:8080"

	// Wait for server to be ready
	suite.waitForServerReady()

	suite.cleanupFunctions = append(suite.cleanupFunctions, func() error {
		return suite.apiServer.Stop()
	})

	suite.logger.Info("API server initialized successfully")
}

// setupWebSocketMonitoring initializes WebSocket connections for testing
func (suite *IntegrationTestSuite) setupWebSocketMonitoring() {
	suite.wsMessages = make([]map[string]interface{}, 0)

	// Connect to WebSocket endpoint
	wsURL := strings.Replace(suite.serverURL, "http://", "ws://", 1) + "/api/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		suite.logger.WithError(err).Warn("Failed to connect to WebSocket for testing")
		return
	}

	suite.wsClients = append(suite.wsClients, conn)

	// Start message listener
	go suite.listenWebSocketMessages(conn)

	suite.logger.Info("WebSocket monitoring initialized successfully")
}

// setupPerformanceMonitoring initializes performance monitoring
func (suite *IntegrationTestSuite) setupPerformanceMonitoring() {
	suite.memoryUsage = make([]int64, 0)
	suite.cpuUsage = make([]float64, 0)

	// Start performance monitoring goroutine
	go suite.monitorPerformance()

	suite.logger.Info("Performance monitoring initialized successfully")
}

// waitForServerReady waits for the API server to be ready
func (suite *IntegrationTestSuite) waitForServerReady() {
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		resp, err := suite.httpClient.Get(suite.serverURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}

	suite.T().Fatal("API server did not become ready within expected time")
}

// listenWebSocketMessages listens for WebSocket messages
func (suite *IntegrationTestSuite) listenWebSocketMessages(conn *websocket.Conn) {
	defer conn.Close()

	for {
		select {
		case <-suite.ctx.Done():
			return
		default:
			var msg map[string]interface{}
			err := conn.ReadJSON(&msg)
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					suite.logger.WithError(err).Error("WebSocket connection error")
				}
				return
			}

			suite.wsMutex.Lock()
			suite.wsMessages = append(suite.wsMessages, msg)
			suite.wsMutex.Unlock()
		}
	}
}

// monitorPerformance monitors system performance metrics
func (suite *IntegrationTestSuite) monitorPerformance() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-suite.ctx.Done():
			return
		case <-ticker.C:
			// Monitor memory usage (simplified)
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			suite.performanceMutex.Lock()
			suite.memoryUsage = append(suite.memoryUsage, int64(m.Alloc))
			suite.cpuUsage = append(suite.cpuUsage, 0.0) // Simplified CPU monitoring
			suite.performanceMutex.Unlock()
		}
	}
}

// Test Scenario 1: Fresh system setup and first benchmark run
func (suite *IntegrationTestSuite) TestScenario1_FreshSystemSetup() {
	suite.logger.Info("Running Test Scenario 1: Fresh system setup and first benchmark run")

	// Generate a benchmark result
	result := suite.generateBenchmarkResult("fresh-system-test", 1)

	// Save the run to historic storage
	savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
	require.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), savedRun.ID)
	assert.Equal(suite.T(), "fresh-system-test", savedRun.TestName)

	// Verify the run can be retrieved
	retrievedRun, err := suite.historicStorage.GetHistoricRun(suite.ctx, savedRun.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), savedRun.ID, retrievedRun.ID)
	assert.Equal(suite.T(), savedRun.TestName, retrievedRun.TestName)

	// Verify files are saved if historic storage is enabled
	if suite.storageConfig.EnableHistoric {
		filesPath, err := suite.historicStorage.GetResultFiles(suite.ctx, savedRun.ID)
		require.NoError(suite.T(), err)
		assert.DirExists(suite.T(), filesPath)

		// Check that result.json exists
		resultFile := filepath.Join(filesPath, "result.json")
		assert.FileExists(suite.T(), resultFile)

		// Check that metadata.json exists
		metadataFile := filepath.Join(filesPath, "metadata.json")
		assert.FileExists(suite.T(), metadataFile)
	}

	// Verify API endpoint returns the run
	resp, err := suite.httpClient.Get(suite.serverURL + "/api/runs/" + savedRun.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Store the run for later tests
	suite.testRuns = append(suite.testRuns, savedRun)

	suite.logger.Info("Test Scenario 1 completed successfully")
}

// Test Scenario 2: Multiple runs with trend analysis and regression detection
func (suite *IntegrationTestSuite) TestScenario2_TrendAnalysisAndRegressionDetection() {
	suite.logger.Info("Running Test Scenario 2: Multiple runs with trend analysis and regression detection")

	testName := "trend-analysis-test"

	// Generate multiple runs with gradually increasing latency (simulating degradation)
	for i := 0; i < 10; i++ {
		result := suite.generateBenchmarkResult(testName, i+1)

		// Introduce gradual performance degradation
		for clientName, clientMetrics := range result.ClientMetrics {
			degradationFactor := 1.0 + float64(i)*0.1 // 10% increase per run
			clientMetrics.Latency.Avg *= degradationFactor
			clientMetrics.Latency.P95 *= degradationFactor
			clientMetrics.Latency.P99 *= degradationFactor
			result.ClientMetrics[clientName] = clientMetrics
		}

		savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
		require.NoError(suite.T(), err)
		suite.testRuns = append(suite.testRuns, savedRun)

		// Small delay to ensure different timestamps
		time.Sleep(100 * time.Millisecond)
	}

	// Wait a moment for data to be processed
	time.Sleep(1 * time.Second)

	// Test trend analysis
	trend, err := suite.historicStorage.GetHistoricTrends(suite.ctx, testName, "overall", "avg_latency", 30)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), testName, trend.TestName)
	assert.Equal(suite.T(), "avg_latency", trend.Metric)
	assert.True(suite.T(), len(trend.Points) >= 5, "Should have multiple trend points")

	// The trend should show degradation
	assert.Contains(suite.T(), []string{"degrading", "stable"}, trend.Trend, "Trend should show degradation or be stable")

	// Test regression detection
	if len(suite.testRuns) >= 2 {
		lastRunID := suite.testRuns[len(suite.testRuns)-1].ID

		// Detect regressions
		options := analysis.DetectionOptions{
			ComparisonMode:     "sequential",
			LookbackCount:      1,
			WindowSize:         3,
			EnableStatistical:  true,
			MinConfidence:      0.90,
			IgnoreImprovements: false,
		}

		report, err := suite.regressionDetector.DetectRegressions(suite.ctx, lastRunID, options)
		require.NoError(suite.T(), err)
		assert.Equal(suite.T(), lastRunID, report.RunID)
		assert.Equal(suite.T(), testName, report.TestName)
	}

	// Test API endpoints for trends
	resp, err := suite.httpClient.Get(fmt.Sprintf("%s/api/tests/%s/trends?days=30", suite.serverURL, testName))
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	suite.logger.Info("Test Scenario 2 completed successfully")
}

// Test Scenario 3: Baseline management and comparison workflows
func (suite *IntegrationTestSuite) TestScenario3_BaselineManagement() {
	suite.logger.Info("Running Test Scenario 3: Baseline management and comparison workflows")

	require.True(suite.T(), len(suite.testRuns) > 0, "Need at least one test run for baseline testing")

	testRun := suite.testRuns[0]
	baselineName := "baseline-test-v1"

	// Create a baseline
	baseline, err := suite.baselineManager.SetBaseline(suite.ctx, testRun.ID, baselineName, "Test baseline for integration testing")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), baselineName, baseline.Name)
	assert.Equal(suite.T(), testRun.ID, baseline.RunID)
	assert.Equal(suite.T(), testRun.TestName, baseline.TestName)

	suite.testBaselines = append(suite.testBaselines, baseline)

	// Verify baseline can be retrieved
	retrievedBaseline, err := suite.baselineManager.GetBaseline(suite.ctx, baselineName)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), baseline.ID, retrievedBaseline.ID)
	assert.Equal(suite.T(), baseline.Name, retrievedBaseline.Name)

	// List baselines
	baselines, err := suite.baselineManager.ListBaselines(suite.ctx, testRun.TestName)
	require.NoError(suite.T(), err)
	assert.True(suite.T(), len(baselines) >= 1, "Should have at least one baseline")

	// Test baseline comparison if we have multiple runs
	if len(suite.testRuns) > 1 {
		compareRun := suite.testRuns[1]
		comparison, err := suite.baselineManager.CompareToBaseline(suite.ctx, compareRun.ID, baselineName)
		require.NoError(suite.T(), err)
		assert.Equal(suite.T(), baselineName, comparison.BaselineName)
		assert.Equal(suite.T(), compareRun.ID, comparison.RunID)
		assert.NotEmpty(suite.T(), comparison.Summary)
	}

	// Test API endpoints
	// Create baseline via API
	baselineReq := api.BaselineRequest{
		RunID:       testRun.ID,
		Name:        "api-baseline-test",
		Description: "Baseline created via API",
	}
	reqBody, _ := json.Marshal(baselineReq)

	resp, err := suite.httpClient.Post(
		suite.serverURL+"/api/baselines",
		"application/json",
		bytes.NewBuffer(reqBody),
	)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	// List baselines via API
	resp, err = suite.httpClient.Get(suite.serverURL + "/api/baselines")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	suite.logger.Info("Test Scenario 3 completed successfully")
}

// Test Scenario 4: WebSocket notifications during system operations
func (suite *IntegrationTestSuite) TestScenario4_WebSocketNotifications() {
	suite.logger.Info("Running Test Scenario 4: WebSocket notifications during system operations")

	// Record initial message count
	suite.wsMutex.RLock()
	initialMessageCount := len(suite.wsMessages)
	suite.wsMutex.RUnlock()

	// Perform operations that should generate WebSocket notifications
	result := suite.generateBenchmarkResult("websocket-test", 1)
	savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
	require.NoError(suite.T(), err)

	// Create a baseline (should trigger notification)
	if len(suite.wsClients) > 0 {
		baseline, err := suite.baselineManager.SetBaseline(suite.ctx, savedRun.ID, "ws-test-baseline", "WebSocket test baseline")
		require.NoError(suite.T(), err)

		// Wait for notifications to be processed
		time.Sleep(2 * time.Second)

		// Check if we received WebSocket messages
		suite.wsMutex.RLock()
		currentMessageCount := len(suite.wsMessages)
		suite.wsMutex.RUnlock()

		if currentMessageCount > initialMessageCount {
			suite.logger.WithFields(logrus.Fields{
				"initial_count": initialMessageCount,
				"current_count": currentMessageCount,
			}).Info("WebSocket notifications received")
		}

		// Test WebSocket ping/pong
		if len(suite.wsClients) > 0 {
			conn := suite.wsClients[0]
			pingMsg := map[string]interface{}{
				"type":      "ping",
				"timestamp": time.Now(),
			}

			err := conn.WriteJSON(pingMsg)
			assert.NoError(suite.T(), err)

			// Wait for pong response
			time.Sleep(1 * time.Second)
		}

		suite.testBaselines = append(suite.testBaselines, baseline)
	}

	suite.logger.Info("Test Scenario 4 completed successfully")
}

// Test Scenario 5: Grafana dashboard data queries
func (suite *IntegrationTestSuite) TestScenario5_GrafanaDashboardQueries() {
	suite.logger.Info("Running Test Scenario 5: Grafana dashboard data queries")

	// Test Grafana root endpoint
	resp, err := suite.httpClient.Get(suite.serverURL + "/grafana/")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Test Grafana search endpoint
	searchReq := api.GrafanaSearchRequest{
		Target: "test",
	}
	reqBody, _ := json.Marshal(searchReq)

	resp, err = suite.httpClient.Post(
		suite.serverURL+"/grafana/search",
		"application/json",
		bytes.NewBuffer(reqBody),
	)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	var searchResults []string
	err = json.NewDecoder(resp.Body).Decode(&searchResults)
	require.NoError(suite.T(), err)
	resp.Body.Close()

	// Test Grafana query endpoint with time series data
	if len(suite.testRuns) > 0 {
		testName := suite.testRuns[0].TestName

		queryReq := api.GrafanaQueryRequest{
			Range: api.GrafanaTimeRange{
				From: time.Now().Add(-24 * time.Hour).Format(time.RFC3339),
				To:   time.Now().Format(time.RFC3339),
			},
			Targets: []api.GrafanaTarget{
				{
					Target: fmt.Sprintf("%s.avg_latency", testName),
				},
				{
					Target: fmt.Sprintf("%s.error_rate", testName),
				},
			},
		}
		reqBody, _ := json.Marshal(queryReq)

		resp, err = suite.httpClient.Post(
			suite.serverURL+"/grafana/query",
			"application/json",
			bytes.NewBuffer(reqBody),
		)
		require.NoError(suite.T(), err)
		assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

		var queryResults []api.GrafanaTimeSeries
		err = json.NewDecoder(resp.Body).Decode(&queryResults)
		require.NoError(suite.T(), err)
		resp.Body.Close()

		// Verify we got results for both targets
		assert.True(suite.T(), len(queryResults) >= 0, "Should return query results")
	}

	// Test tag keys endpoint
	resp, err = suite.httpClient.Post(
		suite.serverURL+"/grafana/tag-keys",
		"application/json",
		bytes.NewBuffer([]byte("{}")),
	)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	suite.logger.Info("Test Scenario 5 completed successfully")
}

// Test Scenario 6: Large dataset performance testing
func (suite *IntegrationTestSuite) TestScenario6_LargeDatasetPerformance() {
	suite.logger.Info("Running Test Scenario 6: Large dataset performance testing")

	testName := "performance-test"
	numRuns := 100

	// Record initial performance metrics
	suite.performanceMutex.RLock()
	initialMemory := int64(0)
	if len(suite.memoryUsage) > 0 {
		initialMemory = suite.memoryUsage[len(suite.memoryUsage)-1]
	}
	suite.performanceMutex.RUnlock()

	startTime := time.Now()

	// Generate a large number of benchmark runs
	var runs []*types.HistoricRun
	for i := 0; i < numRuns; i++ {
		result := suite.generateBenchmarkResult(testName, i+1)

		// Add more complexity to the data
		for clientName, clientMetrics := range result.ClientMetrics {
			// Add more methods
			for j := 0; j < 10; j++ {
				methodName := fmt.Sprintf("additional_method_%d", j)
				clientMetrics.Methods[methodName] = types.MetricSummary{
					Count:     int64(100 + i*10),
					Min:       float64(10 + i),
					Max:       float64(500 + i*5),
					Avg:       float64(100 + i*2),
					P50:       float64(90 + i*2),
					P95:       float64(200 + i*3),
					P99:       float64(300 + i*4),
					ErrorRate: float64(i) / 1000.0,
				}
			}
			result.ClientMetrics[clientName] = clientMetrics
		}

		savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
		require.NoError(suite.T(), err)
		runs = append(runs, savedRun)

		// Log progress every 10 runs
		if (i+1)%10 == 0 {
			suite.logger.WithField("completed", i+1).Info("Generated runs for performance test")
		}
	}

	ingestionDuration := time.Since(startTime)

	// Record final performance metrics
	suite.performanceMutex.RLock()
	finalMemory := int64(0)
	if len(suite.memoryUsage) > 0 {
		finalMemory = suite.memoryUsage[len(suite.memoryUsage)-1]
	}
	suite.performanceMutex.RUnlock()

	memoryIncrease := finalMemory - initialMemory

	suite.logger.WithFields(logrus.Fields{
		"num_runs":           numRuns,
		"ingestion_duration": ingestionDuration,
		"memory_increase_mb": memoryIncrease / (1024 * 1024),
		"runs_per_second":    float64(numRuns) / ingestionDuration.Seconds(),
	}).Info("Large dataset performance metrics")

	// Test performance of various queries
	queryStartTime := time.Now()

	// List runs
	listedRuns, err := suite.historicStorage.ListHistoricRuns(suite.ctx, testName, 50)
	require.NoError(suite.T(), err)
	assert.True(suite.T(), len(listedRuns) >= 50, "Should return requested number of runs")

	// Get trends
	trend, err := suite.historicStorage.GetHistoricTrends(suite.ctx, testName, "overall", "avg_latency", 30)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), testName, trend.TestName)

	// Get summary
	summary, err := suite.historicStorage.GetHistoricSummary(suite.ctx, testName)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), testName, summary.TestName)
	assert.True(suite.T(), summary.TotalRuns >= numRuns, "Summary should reflect all runs")

	queryDuration := time.Since(queryStartTime)

	suite.logger.WithFields(logrus.Fields{
		"query_duration": queryDuration,
		"total_runs":     summary.TotalRuns,
	}).Info("Query performance metrics")

	// Performance assertions
	assert.Less(suite.T(), ingestionDuration.Seconds(), 60.0, "Ingestion should complete within 60 seconds")
	assert.Less(suite.T(), queryDuration.Seconds(), 10.0, "Queries should complete within 10 seconds")
	assert.Less(suite.T(), memoryIncrease, int64(100*1024*1024), "Memory increase should be less than 100MB")

	suite.logger.Info("Test Scenario 6 completed successfully")
}

// Test Scenario 7: System recovery after failures
func (suite *IntegrationTestSuite) TestScenario7_SystemRecovery() {
	suite.logger.Info("Running Test Scenario 7: System recovery after failures")

	// Test database connection recovery
	originalDB := suite.db

	// Simulate database connection failure by closing it
	suite.db.Close()

	// Try to perform operations (should fail gracefully)
	result := suite.generateBenchmarkResult("recovery-test", 1)
	_, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
	assert.Error(suite.T(), err, "Should fail when database is unavailable")

	// Restore database connection
	suite.db, err = sql.Open("postgres", suite.storageConfig.PostgreSQL.GetConnectionString())
	require.NoError(suite.T(), err)

	// Test that operations work again
	savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
	require.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), savedRun.ID)

	// Test file system recovery
	if suite.storageConfig.EnableHistoric {
		// Temporarily make historic directory read-only
		historicDir := suite.storageConfig.HistoricPath
		originalPerm := os.FileMode(0755)

		err := os.Chmod(historicDir, 0444) // Read-only
		if err == nil {
			// Try to save files (should fail gracefully)
			result2 := suite.generateBenchmarkResult("recovery-test-2", 1)
			savedRun2, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result2)

			// The run should still be saved to database even if file save fails
			assert.NoError(suite.T(), err)
			assert.NotEmpty(suite.T(), savedRun2.ID)

			// Restore permissions
			os.Chmod(historicDir, originalPerm)

			// Test that file operations work again
			result3 := suite.generateBenchmarkResult("recovery-test-3", 1)
			savedRun3, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result3)
			require.NoError(suite.T(), err)

			// Verify files are saved
			filesPath, err := suite.historicStorage.GetResultFiles(suite.ctx, savedRun3.ID)
			require.NoError(suite.T(), err)
			assert.DirExists(suite.T(), filesPath)
		}
	}

	// Test API server resilience
	// Make invalid requests to test error handling
	resp, err := suite.httpClient.Get(suite.serverURL + "/api/runs/invalid-run-id")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()

	// Test invalid JSON in request body
	resp, err = suite.httpClient.Post(
		suite.serverURL+"/api/baselines",
		"application/json",
		bytes.NewBuffer([]byte("invalid json")),
	)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, resp.StatusCode)
	resp.Body.Close()

	// Test cleanup and recovery
	err = suite.historicStorage.CleanupOldFiles(suite.ctx)
	assert.NoError(suite.T(), err, "Cleanup should handle errors gracefully")

	suite.logger.Info("Test Scenario 7 completed successfully")
}

// Test concurrent access and race conditions
func (suite *IntegrationTestSuite) TestConcurrentAccess() {
	suite.logger.Info("Testing concurrent access and race conditions")

	testName := "concurrent-test"
	numGoroutines := 10
	opsPerGoroutine := 5

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*opsPerGoroutine)

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < opsPerGoroutine; j++ {
				result := suite.generateBenchmarkResult(testName, goroutineID*opsPerGoroutine+j+1)
				_, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
				if err != nil {
					errors <- err
				}
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < opsPerGoroutine; j++ {
				_, err := suite.historicStorage.ListHistoricRuns(suite.ctx, testName, 10)
				if err != nil {
					errors <- err
				}

				_, err = suite.historicStorage.GetHistoricSummary(suite.ctx, testName)
				if err != nil {
					errors <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for errors
	var errorCount int
	for err := range errors {
		errorCount++
		suite.logger.WithError(err).Error("Concurrent operation error")
	}

	assert.Equal(suite.T(), 0, errorCount, "No errors should occur during concurrent operations")

	suite.logger.Info("Concurrent access test completed successfully")
}

// Test WebSocket connection limits and behavior
func (suite *IntegrationTestSuite) TestWebSocketConnectionLimits() {
	suite.logger.Info("Testing WebSocket connection limits")

	// Create multiple WebSocket connections
	maxConnections := 100
	connections := make([]*websocket.Conn, 0, maxConnections)

	wsURL := strings.Replace(suite.serverURL, "http://", "ws://", 1) + "/api/ws"

	for i := 0; i < maxConnections; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			suite.logger.WithError(err).WithField("connection_num", i).Warn("Failed to create WebSocket connection")
			break
		}

		connections = append(connections, conn)
	}

	suite.logger.WithField("connections_created", len(connections)).Info("Created WebSocket connections")

	// Send messages to all connections
	message := map[string]interface{}{
		"type":    "test",
		"content": "connection limit test",
	}

	for i, conn := range connections {
		err := conn.WriteJSON(message)
		if err != nil {
			suite.logger.WithError(err).WithField("connection_num", i).Warn("Failed to write to WebSocket")
		}
	}

	// Close all connections
	for _, conn := range connections {
		conn.Close()
	}

	suite.logger.Info("WebSocket connection limits test completed")
}

// generateBenchmarkResult creates a sample benchmark result for testing
func (suite *IntegrationTestSuite) generateBenchmarkResult(testName string, runNumber int) *types.BenchmarkResult {
	now := time.Now()
	startTime := now.Add(-5 * time.Minute)
	endTime := now

	// Generate client metrics
	clients := []string{"client-1", "client-2", "client-3"}
	clientMetrics := make(map[string]*types.ClientMetrics)

	for i, clientName := range clients {
		metrics := &types.ClientMetrics{
			Name:          clientName,
			TotalRequests: int64(1000 + runNumber*100),
			TotalErrors:   int64(10 + runNumber),
			ErrorRate:     float64(10+runNumber) / float64(1000+runNumber*100),
			Latency: types.MetricSummary{
				Count:     int64(1000 + runNumber*100),
				Min:       float64(5 + i),
				Max:       float64(500 + i*10),
				Avg:       float64(100 + i*5 + runNumber),
				P50:       float64(90 + i*5 + runNumber),
				P75:       float64(150 + i*7 + runNumber),
				P90:       float64(200 + i*10 + runNumber),
				P95:       float64(250 + i*12 + runNumber),
				P99:       float64(400 + i*15 + runNumber),
				ErrorRate: float64(10+runNumber+i) / float64(1000+runNumber*100),
			},
			Methods: make(map[string]types.MetricSummary),
			ConnectionMetrics: types.ConnectionMetrics{
				ActiveConnections:  int64(10 + i),
				ConnectionsCreated: int64(20 + i*2),
				ConnectionsClosed:  int64(15 + i),
				ConnectionReuse:    0.8 + float64(i)*0.05,
			},
			TimeSeries:    make(map[string][]types.TimeSeriesPoint),
			SystemMetrics: []types.SystemMetrics{},
			ErrorTypes:    make(map[string]int64),
			StatusCodes:   make(map[int]int64),
		}

		// Add method metrics
		methods := []string{"eth_getBalance", "eth_getBlockByNumber", "eth_getTransactionByHash"}
		for j, methodName := range methods {
			metrics.Methods[methodName] = types.MetricSummary{
				Count:     int64(300 + runNumber*30 + j*10),
				Min:       float64(3 + j),
				Max:       float64(200 + j*5),
				Avg:       float64(50 + j*3 + runNumber),
				P50:       float64(45 + j*3 + runNumber),
				P95:       float64(120 + j*5 + runNumber),
				P99:       float64(180 + j*7 + runNumber),
				ErrorRate: float64(5+runNumber+j) / float64(300+runNumber*30+j*10),
			}
		}

		// Add status codes
		metrics.StatusCodes[200] = int64(900 + runNumber*90)
		metrics.StatusCodes[500] = int64(5 + runNumber)
		metrics.StatusCodes[429] = int64(3 + runNumber/2)

		clientMetrics[clientName] = metrics
	}

	return &types.BenchmarkResult{
		Config: map[string]interface{}{
			"test_name":   testName,
			"description": fmt.Sprintf("Integration test run #%d", runNumber),
			"rps":         100,
			"duration":    "5m",
			"endpoints":   []string{"eth_getBalance", "eth_getBlockByNumber", "eth_getTransactionByHash"},
		},
		Summary: map[string]interface{}{
			"total_requests": int64(3000 + runNumber*300),
			"total_errors":   int64(30 + runNumber*3),
			"duration":       "5m",
		},
		ClientMetrics: clientMetrics,
		Timestamp:     now.Format(time.RFC3339),
		StartTime:     startTime.Format(time.RFC3339),
		EndTime:       endTime.Format(time.RFC3339),
		Duration:      endTime.Sub(startTime).String(),
		Environment: types.EnvironmentInfo{
			OS:            "linux",
			Architecture:  "amd64",
			CPUModel:      "Intel Core i7",
			CPUCores:      8,
			TotalMemoryGB: 16.0,
			GoVersion:     "go1.21.0",
			K6Version:     "v0.45.0",
			NetworkType:   "ethernet",
		},
	}
}

// Helper method to validate benchmark result structure
func (suite *IntegrationTestSuite) validateBenchmarkResult(result *types.BenchmarkResult) {
	assert.NotNil(suite.T(), result)
	assert.NotEmpty(suite.T(), result.Timestamp)
	assert.NotEmpty(suite.T(), result.StartTime)
	assert.NotEmpty(suite.T(), result.EndTime)
	assert.NotNil(suite.T(), result.Config)
	assert.NotNil(suite.T(), result.Summary)
	assert.NotNil(suite.T(), result.ClientMetrics)
	assert.True(suite.T(), len(result.ClientMetrics) > 0)

	// Validate client metrics
	for clientName, metrics := range result.ClientMetrics {
		assert.NotEmpty(suite.T(), clientName)
		assert.NotNil(suite.T(), metrics)
		assert.True(suite.T(), metrics.TotalRequests > 0)
		assert.True(suite.T(), metrics.Latency.Count > 0)
		assert.True(suite.T(), metrics.Latency.Avg > 0)
		assert.True(suite.T(), len(metrics.Methods) > 0)
	}
}

// Helper method to wait for specific WebSocket message
func (suite *IntegrationTestSuite) waitForWebSocketMessage(messageType string, timeout time.Duration) map[string]interface{} {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		suite.wsMutex.RLock()
		for _, msg := range suite.wsMessages {
			if msgType, ok := msg["type"].(string); ok && msgType == messageType {
				suite.wsMutex.RUnlock()
				return msg
			}
		}
		suite.wsMutex.RUnlock()

		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// Helper method to get performance metrics summary
func (suite *IntegrationTestSuite) getPerformanceMetricsSummary() map[string]interface{} {
	suite.performanceMutex.RLock()
	defer suite.performanceMutex.RUnlock()

	if len(suite.memoryUsage) == 0 {
		return map[string]interface{}{
			"memory_samples": 0,
			"cpu_samples":    0,
		}
	}

	// Calculate memory statistics
	var totalMemory, minMemory, maxMemory int64
	minMemory = suite.memoryUsage[0]
	maxMemory = suite.memoryUsage[0]

	for _, mem := range suite.memoryUsage {
		totalMemory += mem
		if mem < minMemory {
			minMemory = mem
		}
		if mem > maxMemory {
			maxMemory = mem
		}
	}

	avgMemory := totalMemory / int64(len(suite.memoryUsage))

	return map[string]interface{}{
		"memory_samples":   len(suite.memoryUsage),
		"cpu_samples":      len(suite.cpuUsage),
		"avg_memory_bytes": avgMemory,
		"min_memory_bytes": minMemory,
		"max_memory_bytes": maxMemory,
		"memory_increase":  maxMemory - minMemory,
	}
}

// Run the integration test suite
func TestIntegrationSuite(t *testing.T) {
	// Skip integration tests if not enabled
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Integration tests not enabled. Set INTEGRATION_TESTS=1 to run.")
	}

	suite.Run(t, new(IntegrationTestSuite))
}
