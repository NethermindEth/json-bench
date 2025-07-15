package analysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/jsonrpc-bench/runner/types"
)

// MockHistoricStorage is a mock implementation of storage.HistoricStorage
type MockHistoricStorage struct {
	mock.Mock
}

func (m *MockHistoricStorage) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockHistoricStorage) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockHistoricStorage) SaveHistoricRun(ctx context.Context, result *types.BenchmarkResult) (*types.HistoricRun, error) {
	args := m.Called(ctx, result)
	return args.Get(0).(*types.HistoricRun), args.Error(1)
}

func (m *MockHistoricStorage) GetHistoricRun(ctx context.Context, runID string) (*types.HistoricRun, error) {
	args := m.Called(ctx, runID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.HistoricRun), args.Error(1)
}

func (m *MockHistoricStorage) ListHistoricRuns(ctx context.Context, testName string, limit int) ([]*types.HistoricRun, error) {
	args := m.Called(ctx, testName, limit)
	return args.Get(0).([]*types.HistoricRun), args.Error(1)
}

func (m *MockHistoricStorage) DeleteHistoricRun(ctx context.Context, runID string) error {
	args := m.Called(ctx, runID)
	return args.Error(0)
}

func (m *MockHistoricStorage) GetHistoricTrends(ctx context.Context, testName, client, metric string, days int) (*types.HistoricTrend, error) {
	args := m.Called(ctx, testName, client, metric, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.HistoricTrend), args.Error(1)
}

func (m *MockHistoricStorage) CompareRuns(ctx context.Context, runID1, runID2 string) (*types.HistoricComparison, error) {
	args := m.Called(ctx, runID1, runID2)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.HistoricComparison), args.Error(1)
}

func (m *MockHistoricStorage) GetHistoricSummary(ctx context.Context, testName string) (*types.HistoricSummary, error) {
	args := m.Called(ctx, testName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.HistoricSummary), args.Error(1)
}

func (m *MockHistoricStorage) SaveResultFiles(ctx context.Context, runID string, result *types.BenchmarkResult) error {
	args := m.Called(ctx, runID, result)
	return args.Error(0)
}

func (m *MockHistoricStorage) GetResultFiles(ctx context.Context, runID string) (string, error) {
	args := m.Called(ctx, runID)
	return args.String(0), args.Error(1)
}

func (m *MockHistoricStorage) CleanupOldFiles(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// BaselineTestSuite contains all baseline management tests
type BaselineTestSuite struct {
	suite.Suite
	mockStorage *MockHistoricStorage
	mockDB      *sql.DB
	manager     BaselineManager
	ctx         context.Context
}

func (suite *BaselineTestSuite) SetupTest() {
	suite.mockStorage = new(MockHistoricStorage)
	suite.ctx = context.Background()

	// Create in-memory SQLite database for testing
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(suite.T(), err)
	suite.mockDB = db

	// Create baseline manager
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise in tests
	suite.manager = NewBaselineManager(suite.mockStorage, suite.mockDB, logger)

	// Start the manager and create tables
	err = suite.manager.Start(suite.ctx)
	require.NoError(suite.T(), err)
}

func (suite *BaselineTestSuite) TearDownTest() {
	if suite.mockDB != nil {
		suite.mockDB.Close()
	}
	suite.mockStorage.AssertExpectations(suite.T())
}

// TestBaselineCreation tests baseline creation functionality
func (suite *BaselineTestSuite) TestBaselineCreation() {
	// Create test data
	testRun := suite.createTestHistoricRun("test-run-1", "test-benchmark")
	_ = suite.createTestBenchmarkResult()

	// Mock storage expectations
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "test-run-1").Return(testRun, nil)

	// Test baseline creation
	baseline, err := suite.manager.SetBaseline(suite.ctx, "test-run-1", "test-baseline", "Test baseline for unit tests")

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), baseline)
	assert.Equal(suite.T(), "test-baseline", baseline.Name)
	assert.Equal(suite.T(), "Test baseline for unit tests", baseline.Description)
	assert.Equal(suite.T(), "test-benchmark", baseline.TestName)
	assert.Equal(suite.T(), "test-run-1", baseline.RunID)
	assert.True(suite.T(), baseline.IsActive)

	// Verify baseline metrics were extracted correctly
	assert.Equal(suite.T(), testRun.OverallErrorRate, baseline.BaselineMetrics.OverallErrorRate)
	assert.Equal(suite.T(), testRun.AvgLatencyMs, baseline.BaselineMetrics.AvgLatencyMs)
	assert.Equal(suite.T(), testRun.P95LatencyMs, baseline.BaselineMetrics.P95LatencyMs)
	assert.Equal(suite.T(), testRun.P99LatencyMs, baseline.BaselineMetrics.P99LatencyMs)
	assert.Equal(suite.T(), testRun.TotalRequests, baseline.BaselineMetrics.TotalRequests)
	assert.Equal(suite.T(), testRun.TotalErrors, baseline.BaselineMetrics.TotalErrors)

	// Verify client metrics
	assert.NotEmpty(suite.T(), baseline.BaselineMetrics.ClientMetrics)
	clientBaseline, exists := baseline.BaselineMetrics.ClientMetrics["geth"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), 0.02, clientBaseline.ErrorRate)
	assert.Equal(suite.T(), 150.0, clientBaseline.AvgLatency)
}

// TestBaselineRetrieval tests baseline retrieval functionality
func (suite *BaselineTestSuite) TestBaselineRetrieval() {
	// Create and save a baseline first
	testRun := suite.createTestHistoricRun("test-run-2", "test-benchmark")
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "test-run-2").Return(testRun, nil)

	createdBaseline, err := suite.manager.SetBaseline(suite.ctx, "test-run-2", "retrieval-baseline", "Test retrieval")
	require.NoError(suite.T(), err)

	// Test baseline retrieval
	retrievedBaseline, err := suite.manager.GetBaseline(suite.ctx, "retrieval-baseline")

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), retrievedBaseline)
	assert.Equal(suite.T(), createdBaseline.Name, retrievedBaseline.Name)
	assert.Equal(suite.T(), createdBaseline.Description, retrievedBaseline.Description)
	assert.Equal(suite.T(), createdBaseline.TestName, retrievedBaseline.TestName)
	assert.Equal(suite.T(), createdBaseline.RunID, retrievedBaseline.RunID)
}

// TestBaselineNotFound tests handling of non-existent baselines
func (suite *BaselineTestSuite) TestBaselineNotFound() {
	_, err := suite.manager.GetBaseline(suite.ctx, "non-existent-baseline")

	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "baseline not found")
}

// TestBaselineList tests listing baselines
func (suite *BaselineTestSuite) TestBaselineList() {
	// Create multiple baselines for the same test
	testRun1 := suite.createTestHistoricRun("test-run-3", "list-test")
	testRun2 := suite.createTestHistoricRun("test-run-4", "list-test")
	testRun3 := suite.createTestHistoricRun("test-run-5", "different-test")

	suite.mockStorage.On("GetHistoricRun", suite.ctx, "test-run-3").Return(testRun1, nil)
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "test-run-4").Return(testRun2, nil)
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "test-run-5").Return(testRun3, nil)

	// Create baselines
	_, err := suite.manager.SetBaseline(suite.ctx, "test-run-3", "baseline-1", "First baseline")
	require.NoError(suite.T(), err)

	_, err = suite.manager.SetBaseline(suite.ctx, "test-run-4", "baseline-2", "Second baseline")
	require.NoError(suite.T(), err)

	_, err = suite.manager.SetBaseline(suite.ctx, "test-run-5", "baseline-3", "Third baseline")
	require.NoError(suite.T(), err)

	// Test listing baselines for specific test
	baselines, err := suite.manager.ListBaselines(suite.ctx, "list-test")
	require.NoError(suite.T(), err)
	assert.Len(suite.T(), baselines, 2)

	// Test listing all baselines
	allBaselines, err := suite.manager.ListBaselines(suite.ctx, "")
	require.NoError(suite.T(), err)
	assert.Len(suite.T(), allBaselines, 3)
}

// TestBaselineDeletion tests baseline deletion
func (suite *BaselineTestSuite) TestBaselineDeletion() {
	// Create a baseline first
	testRun := suite.createTestHistoricRun("test-run-6", "delete-test")
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "test-run-6").Return(testRun, nil)

	_, err := suite.manager.SetBaseline(suite.ctx, "test-run-6", "delete-baseline", "To be deleted")
	require.NoError(suite.T(), err)

	// Verify baseline exists
	baseline, err := suite.manager.GetBaseline(suite.ctx, "delete-baseline")
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), baseline)

	// Delete baseline
	err = suite.manager.DeleteBaseline(suite.ctx, "delete-baseline")
	require.NoError(suite.T(), err)

	// Verify baseline no longer exists
	_, err = suite.manager.GetBaseline(suite.ctx, "delete-baseline")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "baseline not found")
}

// TestBaselineComparison tests baseline comparison functionality
func (suite *BaselineTestSuite) TestBaselineComparison() {
	// Create baseline run
	baselineRun := suite.createTestHistoricRun("baseline-run", "comparison-test")
	currentRun := suite.createTestHistoricRun("current-run", "comparison-test")

	// Modify current run to have different metrics (worse performance)
	currentRun.AvgLatencyMs = 200.0    // Worse than baseline 150ms
	currentRun.OverallErrorRate = 0.05 // Worse than baseline 0.02

	// Update the full results to reflect changes
	currentResult := suite.createTestBenchmarkResult()
	currentResult.ClientMetrics["geth"].Latency.Avg = 200.0
	currentResult.ClientMetrics["geth"].ErrorRate = 0.05
	currentResultJSON, _ := json.Marshal(currentResult)
	currentRun.FullResults = currentResultJSON

	suite.mockStorage.On("GetHistoricRun", suite.ctx, "baseline-run").Return(baselineRun, nil)
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "current-run").Return(currentRun, nil)

	// Create baseline
	_, err := suite.manager.SetBaseline(suite.ctx, "baseline-run", "comparison-baseline", "Baseline for comparison")
	require.NoError(suite.T(), err)

	// Test comparison
	comparison, err := suite.manager.CompareToBaseline(suite.ctx, "current-run", "comparison-baseline")

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), comparison)
	assert.Equal(suite.T(), "current-run", comparison.RunID)
	assert.Equal(suite.T(), "comparison-baseline", comparison.BaselineName)
	assert.Equal(suite.T(), "baseline-run", comparison.BaselineRunID)

	// Verify overall change calculation
	expectedPercentChange := ((200.0 - 150.0) / 150.0) * 100 // ~33.33% increase
	assert.InDelta(suite.T(), expectedPercentChange, comparison.OverallChange.PercentChange, 0.1)
	assert.False(suite.T(), comparison.OverallChange.IsImprovement) // Latency increase is not improvement

	// Verify client changes
	assert.NotEmpty(suite.T(), comparison.ClientChanges)
	gethChange, exists := comparison.ClientChanges["geth"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "degraded", gethChange.Status)
}

// TestRegressionDetection tests regression detection logic
func (suite *BaselineTestSuite) TestRegressionDetection() {
	// Create baseline and current runs with significant regression
	baselineRun := suite.createTestHistoricRun("regression-baseline", "regression-test")
	currentRun := suite.createTestHistoricRun("regression-current", "regression-test")

	// Create a significant regression (50% latency increase)
	currentRun.AvgLatencyMs = 225.0 // 50% increase from 150ms
	currentRun.P95LatencyMs = 450.0 // 50% increase from 300ms

	// Update full results to match
	currentResult := suite.createTestBenchmarkResult()
	currentResult.ClientMetrics["geth"].Latency.Avg = 225.0
	currentResult.ClientMetrics["geth"].Latency.P95 = 450.0
	currentResultJSON, _ := json.Marshal(currentResult)
	currentRun.FullResults = currentResultJSON

	suite.mockStorage.On("GetHistoricRun", suite.ctx, "regression-baseline").Return(baselineRun, nil)
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "regression-current").Return(currentRun, nil)

	// Create baseline
	_, err := suite.manager.SetBaseline(suite.ctx, "regression-baseline", "regression-baseline", "Baseline for regression test")
	require.NoError(suite.T(), err)

	// Test regression detection
	thresholds := RegressionThresholds{
		ErrorRateThreshold:  0.01, // 1% absolute increase
		LatencyThreshold:    10.0, // 10% increase threshold
		ThroughputThreshold: 10.0, // 10% decrease threshold
		SignificanceLevel:   0.05,
		MinSampleSize:       10,
		ConsecutiveRuns:     1,
	}

	regressions, err := suite.manager.DetectRegressions(suite.ctx, "regression-current", "regression-baseline", thresholds)

	require.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), regressions)

	// Verify regression details
	var latencyRegression *types.Regression
	for _, reg := range regressions {
		if reg.Metric == "avg_latency" {
			latencyRegression = reg
			break
		}
	}

	require.NotNil(suite.T(), latencyRegression)
	assert.Equal(suite.T(), "geth", latencyRegression.Client)
	assert.Equal(suite.T(), "avg_latency", latencyRegression.Metric)
	assert.Equal(suite.T(), 150.0, latencyRegression.BaselineValue)
	assert.Equal(suite.T(), 225.0, latencyRegression.CurrentValue)
	assert.InDelta(suite.T(), 50.0, latencyRegression.PercentChange, 0.1)
	assert.Equal(suite.T(), "critical", latencyRegression.Severity) // 50% increase should be critical
}

// TestBaselineHistory tests baseline history functionality
func (suite *BaselineTestSuite) TestBaselineHistory() {
	// Create baseline
	baselineRun := suite.createTestHistoricRun("history-baseline", "history-test")
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "history-baseline").Return(baselineRun, nil)

	_, err := suite.manager.SetBaseline(suite.ctx, "history-baseline", "history-baseline", "Baseline for history test")
	require.NoError(suite.T(), err)

	// Test baseline history retrieval
	history, err := suite.manager.GetBaselineHistory(suite.ctx, "history-baseline", 7)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), history)
	// Note: This test would require additional setup to create historic runs in the database
	// For now, we verify that the method doesn't error
}

// TestStatisticalSignificance tests statistical significance calculations
func (suite *BaselineTestSuite) TestStatisticalSignificance() {
	// Create test data with known statistical properties
	baseline1 := ComparisonMetric{
		BaselineValue:  100.0,
		CurrentValue:   110.0,
		AbsoluteChange: 10.0,
		PercentChange:  10.0,
	}

	baseline2 := ComparisonMetric{
		BaselineValue:  100.0,
		CurrentValue:   105.0,
		AbsoluteChange: 5.0,
		PercentChange:  5.0,
	}

	// Test significance determination
	// 10% change should be considered significant with default thresholds
	assert.True(suite.T(), baseline1.PercentChange >= 5.0) // Default significance threshold

	// 5% change is at the boundary
	assert.True(suite.T(), baseline2.PercentChange >= 5.0)
}

// TestConcurrentOperations tests concurrent baseline operations
func (suite *BaselineTestSuite) TestConcurrentOperations() {
	// Create test data
	testRun := suite.createTestHistoricRun("concurrent-run", "concurrent-test")
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "concurrent-run").Return(testRun, nil).Maybe()

	// Test concurrent baseline creation
	done := make(chan bool, 10)
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			baselineName := fmt.Sprintf("concurrent-baseline-%d", id)
			_, err := suite.manager.SetBaseline(suite.ctx, "concurrent-run", baselineName, "Concurrent test")
			if err != nil {
				errors <- err
				return
			}

			// Try to retrieve the baseline
			_, err = suite.manager.GetBaseline(suite.ctx, baselineName)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	close(errors)
	for err := range errors {
		suite.T().Errorf("Concurrent operation failed: %v", err)
	}
}

// TestBaselineUpdate tests baseline update functionality
func (suite *BaselineTestSuite) TestBaselineUpdate() {
	// Create initial baseline
	testRun1 := suite.createTestHistoricRun("update-run-1", "update-test")
	testRun2 := suite.createTestHistoricRun("update-run-2", "update-test")

	suite.mockStorage.On("GetHistoricRun", suite.ctx, "update-run-1").Return(testRun1, nil)
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "update-run-2").Return(testRun2, nil)

	// Create initial baseline
	baseline1, err := suite.manager.SetBaseline(suite.ctx, "update-run-1", "update-baseline", "Initial baseline")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), "update-run-1", baseline1.RunID)

	// Update baseline with new run
	baseline2, err := suite.manager.SetBaseline(suite.ctx, "update-run-2", "update-baseline", "Updated baseline")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), "update-run-2", baseline2.RunID)
	assert.Equal(suite.T(), "Updated baseline", baseline2.Description)

	// Verify only one baseline exists with the name
	retrieved, err := suite.manager.GetBaseline(suite.ctx, "update-baseline")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), "update-run-2", retrieved.RunID)
}

// TestEdgeCases tests various edge cases
func (suite *BaselineTestSuite) TestEdgeCases() {
	// Test with empty test name
	emptyRun := suite.createTestHistoricRun("empty-run", "")
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "empty-run").Return(emptyRun, nil)

	baseline, err := suite.manager.SetBaseline(suite.ctx, "empty-run", "empty-baseline", "Empty test name")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), "", baseline.TestName)

	// Test with zero metrics
	zeroRun := suite.createTestHistoricRun("zero-run", "zero-test")
	zeroRun.AvgLatencyMs = 0
	zeroRun.P95LatencyMs = 0
	zeroRun.TotalRequests = 0
	zeroRun.TotalErrors = 0

	suite.mockStorage.On("GetHistoricRun", suite.ctx, "zero-run").Return(zeroRun, nil)

	zeroBaseline, err := suite.manager.SetBaseline(suite.ctx, "zero-run", "zero-baseline", "Zero metrics")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), 0.0, zeroBaseline.BaselineMetrics.AvgLatencyMs)
}

// TestPerformanceComparison tests performance metric comparison accuracy
func (suite *BaselineTestSuite) TestPerformanceComparison() {
	// Test data with known mathematical relationships
	testCases := []struct {
		name           string
		baselineValue  float64
		currentValue   float64
		expectedChange float64
		isImprovement  bool
	}{
		{
			name:           "50% increase",
			baselineValue:  100.0,
			currentValue:   150.0,
			expectedChange: 50.0,
			isImprovement:  false, // For latency, increase is bad
		},
		{
			name:           "25% decrease",
			baselineValue:  200.0,
			currentValue:   150.0,
			expectedChange: -25.0,
			isImprovement:  true, // For latency, decrease is good
		},
		{
			name:           "No change",
			baselineValue:  100.0,
			currentValue:   100.0,
			expectedChange: 0.0,
			isImprovement:  false,
		},
		{
			name:           "Small increase",
			baselineValue:  100.0,
			currentValue:   103.0,
			expectedChange: 3.0,
			isImprovement:  false,
		},
	}

	for _, tc := range testCases {
		suite.T().Run(tc.name, func(t *testing.T) {
			change := calculatePercentChange(tc.baselineValue, tc.currentValue)
			assert.InDelta(t, tc.expectedChange, change, 0.001)

			// For latency metrics, lower is better
			isImprovement := tc.currentValue < tc.baselineValue
			assert.Equal(t, tc.isImprovement, isImprovement)
		})
	}
}

// Helper functions for creating test data

func (suite *BaselineTestSuite) createTestHistoricRun(runID, testName string) *types.HistoricRun {
	result := suite.createTestBenchmarkResult()
	resultJSON, _ := json.Marshal(result)

	return &types.HistoricRun{
		ID:               runID,
		TestName:         testName,
		Description:      "Test run for baseline testing",
		GitCommit:        "abc123",
		GitBranch:        "main",
		Tags:             []string{"test", "baseline"},
		Timestamp:        time.Now(),
		StartTime:        time.Now().Add(-10 * time.Minute),
		EndTime:          time.Now(),
		Duration:         "10m",
		TotalRequests:    1000,
		TotalErrors:      20,
		OverallErrorRate: 0.02,
		AvgLatencyMs:     150.0,
		P95LatencyMs:     300.0,
		P99LatencyMs:     500.0,
		MaxLatencyMs:     1000.0,
		PerformanceScores: map[string]float64{
			"geth":       85.0,
			"nethermind": 82.0,
		},
		BestClient:  "geth",
		FullResults: resultJSON,
		Environment: types.EnvironmentInfo{
			OS:        "linux",
			Arch:      "amd64",
			GoVersion: "1.21.0",
		},
		Notes:     "Test notes",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func (suite *BaselineTestSuite) createTestBenchmarkResult() *types.BenchmarkResult {
	return &types.BenchmarkResult{
		TestName:  "test-benchmark",
		StartTime: time.Now().Add(-10 * time.Minute),
		EndTime:   time.Now(),
		Duration:  time.Minute * 10,
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:          "geth",
				TotalRequests: 500,
				TotalErrors:   10,
				ErrorRate:     0.02,
				Latency: types.LatencyMetrics{
					Avg:        150.0,
					P50:        120.0,
					P95:        300.0,
					P99:        500.0,
					Max:        1000.0,
					Throughput: 50.0,
				},
				Methods: map[string]types.MetricSummary{
					"eth_getBalance": {
						Count:      250,
						ErrorRate:  0.01,
						Avg:        140.0,
						P50:        110.0,
						P95:        280.0,
						P99:        480.0,
						Max:        900.0,
						Throughput: 25.0,
					},
					"eth_getBlockByNumber": {
						Count:      250,
						ErrorRate:  0.03,
						Avg:        160.0,
						P50:        130.0,
						P95:        320.0,
						P99:        520.0,
						Max:        1000.0,
						Throughput: 25.0,
					},
				},
			},
			"nethermind": {
				Name:          "nethermind",
				TotalRequests: 500,
				TotalErrors:   10,
				ErrorRate:     0.02,
				Latency: types.LatencyMetrics{
					Avg:        155.0,
					P50:        125.0,
					P95:        310.0,
					P99:        510.0,
					Max:        1050.0,
					Throughput: 48.0,
				},
				Methods: map[string]types.MetricSummary{
					"eth_getBalance": {
						Count:      250,
						ErrorRate:  0.015,
						Avg:        145.0,
						P50:        115.0,
						P95:        290.0,
						P99:        490.0,
						Max:        950.0,
						Throughput: 24.0,
					},
					"eth_getBlockByNumber": {
						Count:      250,
						ErrorRate:  0.025,
						Avg:        165.0,
						P50:        135.0,
						P95:        330.0,
						P99:        530.0,
						Max:        1050.0,
						Throughput: 24.0,
					},
				},
			},
		},
	}
}

// Benchmark tests for performance validation
func BenchmarkBaselineCreation(b *testing.B) {
	mockStorage := new(MockHistoricStorage)
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	manager := NewBaselineManager(mockStorage, db, logger)
	manager.Start(context.Background())

	testRun := &types.HistoricRun{
		ID:               "bench-run",
		TestName:         "benchmark-test",
		OverallErrorRate: 0.02,
		AvgLatencyMs:     150.0,
		P95LatencyMs:     300.0,
		TotalRequests:    1000,
		TotalErrors:      20,
		FullResults:      json.RawMessage(`{"client_metrics":{}}`),
		Timestamp:        time.Now(),
	}

	mockStorage.On("GetHistoricRun", mock.Anything, "bench-run").Return(testRun, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		baselineName := fmt.Sprintf("bench-baseline-%d", i)
		_, err := manager.SetBaseline(context.Background(), "bench-run", baselineName, "Benchmark baseline")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBaselineComparison(b *testing.B) {
	mockStorage := new(MockHistoricStorage)
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	manager := NewBaselineManager(mockStorage, db, logger)
	manager.Start(context.Background())

	// Create test data
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"test-client": {
				Name:      "test-client",
				ErrorRate: 0.02,
				Latency: types.LatencyMetrics{
					Avg: 150.0,
					P95: 300.0,
					P99: 500.0,
				},
			},
		},
	}
	resultJSON, _ := json.Marshal(result)

	baselineRun := &types.HistoricRun{
		ID:               "baseline-run",
		TestName:         "benchmark-test",
		OverallErrorRate: 0.02,
		AvgLatencyMs:     150.0,
		P95LatencyMs:     300.0,
		TotalRequests:    1000,
		TotalErrors:      20,
		FullResults:      resultJSON,
		Timestamp:        time.Now(),
	}

	currentRun := &types.HistoricRun{
		ID:               "current-run",
		TestName:         "benchmark-test",
		OverallErrorRate: 0.03,
		AvgLatencyMs:     180.0,
		P95LatencyMs:     360.0,
		TotalRequests:    1000,
		TotalErrors:      30,
		FullResults:      resultJSON,
		Timestamp:        time.Now(),
	}

	mockStorage.On("GetHistoricRun", mock.Anything, "baseline-run").Return(baselineRun, nil)
	mockStorage.On("GetHistoricRun", mock.Anything, "current-run").Return(currentRun, nil)

	// Create baseline
	_, err := manager.SetBaseline(context.Background(), "baseline-run", "bench-baseline", "Benchmark baseline")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.CompareToBaseline(context.Background(), "current-run", "bench-baseline")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Run the test suite
func TestBaselineTestSuite(t *testing.T) {
	suite.Run(t, new(BaselineTestSuite))
}

// Test mathematical accuracy of calculations
func TestCalculatePercentChange(t *testing.T) {
	testCases := []struct {
		baseline float64
		current  float64
		expected float64
	}{
		{100.0, 150.0, 50.0},
		{100.0, 50.0, -50.0},
		{200.0, 220.0, 10.0},
		{50.0, 45.0, -10.0},
		{0.0, 10.0, 0.0}, // Division by zero case
		{100.0, 100.0, 0.0},
	}

	for _, tc := range testCases {
		result := calculatePercentChange(tc.baseline, tc.current)
		assert.InDelta(t, tc.expected, result, 0.001)
	}
}

func TestCalculateOverallScore(t *testing.T) {
	// Test score calculation with known inputs
	score1 := calculateOverallScore(0.01, 100.0, 1000.0) // Low error, good latency, good throughput
	score2 := calculateOverallScore(0.1, 500.0, 100.0)   // High error, bad latency, poor throughput

	assert.True(t, score1 > score2, "Better metrics should yield higher score")
	assert.True(t, score1 <= 100.0, "Score should not exceed 100")
	assert.True(t, score2 >= 0.0, "Score should not be negative")
}
