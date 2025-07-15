package analysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/jsonrpc-bench/runner/types"
)

// MockBaselineManager is a mock implementation of BaselineManager
type MockBaselineManager struct {
	mock.Mock
}

func (m *MockBaselineManager) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockBaselineManager) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockBaselineManager) SetBaseline(ctx context.Context, runID, name, description string) (*Baseline, error) {
	args := m.Called(ctx, runID, name, description)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Baseline), args.Error(1)
}

func (m *MockBaselineManager) GetBaseline(ctx context.Context, name string) (*Baseline, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Baseline), args.Error(1)
}

func (m *MockBaselineManager) ListBaselines(ctx context.Context, testName string) ([]*Baseline, error) {
	args := m.Called(ctx, testName)
	return args.Get(0).([]*Baseline), args.Error(1)
}

func (m *MockBaselineManager) DeleteBaseline(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

func (m *MockBaselineManager) CompareToBaseline(ctx context.Context, runID, baselineName string) (*BaselineComparison, error) {
	args := m.Called(ctx, runID, baselineName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*BaselineComparison), args.Error(1)
}

func (m *MockBaselineManager) CompareToAllBaselines(ctx context.Context, runID string) ([]*BaselineComparison, error) {
	args := m.Called(ctx, runID)
	return args.Get(0).([]*BaselineComparison), args.Error(1)
}

func (m *MockBaselineManager) DetectRegressions(ctx context.Context, runID, baselineName string, thresholds RegressionThresholds) ([]*types.Regression, error) {
	args := m.Called(ctx, runID, baselineName, thresholds)
	return args.Get(0).([]*types.Regression), args.Error(1)
}

func (m *MockBaselineManager) GetBaselineHistory(ctx context.Context, baselineName string, days int) ([]*BaselineHistoryPoint, error) {
	args := m.Called(ctx, baselineName, days)
	return args.Get(0).([]*BaselineHistoryPoint), args.Error(1)
}

// RegressionTestSuite contains all regression detection tests
type RegressionTestSuite struct {
	suite.Suite
	mockStorage         *MockHistoricStorage
	mockBaselineManager *MockBaselineManager
	mockDB              *sql.DB
	detector            RegressionDetector
	ctx                 context.Context
}

func (suite *RegressionTestSuite) SetupTest() {
	suite.mockStorage = new(MockHistoricStorage)
	suite.mockBaselineManager = new(MockBaselineManager)
	suite.ctx = context.Background()

	// Create in-memory SQLite database for testing
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(suite.T(), err)
	suite.mockDB = db

	// Create regression detector
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise in tests
	suite.detector = NewRegressionDetector(suite.mockStorage, suite.mockBaselineManager, suite.mockDB, logger)

	// Start the detector
	err = suite.detector.Start(suite.ctx)
	require.NoError(suite.T(), err)
}

func (suite *RegressionTestSuite) TearDownTest() {
	if suite.mockDB != nil {
		suite.mockDB.Close()
	}
	suite.detector.Stop()
	suite.mockStorage.AssertExpectations(suite.T())
	suite.mockBaselineManager.AssertExpectations(suite.T())
}

// TestRegressionThresholds tests threshold management
func (suite *RegressionTestSuite) TestRegressionThresholds() {
	// Test setting custom thresholds
	customThreshold := RegressionThreshold{
		MinorThreshold:    3.0,
		MajorThreshold:    8.0,
		CriticalThreshold: 15.0,
		MinSampleSize:     5,
		SignificanceLevel: 0.01,
		IsPercentage:      true,
		Direction:         "increase",
	}

	err := suite.detector.SetThreshold("custom_latency", customThreshold)
	require.NoError(suite.T(), err)

	// Test getting thresholds
	thresholds := suite.detector.GetThresholds()
	assert.NotEmpty(suite.T(), thresholds)

	// Verify custom threshold was set
	retrievedThreshold, exists := thresholds["custom_latency"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "custom_latency", retrievedThreshold.MetricName)
	assert.Equal(suite.T(), 3.0, retrievedThreshold.MinorThreshold)
	assert.Equal(suite.T(), 8.0, retrievedThreshold.MajorThreshold)
	assert.Equal(suite.T(), 15.0, retrievedThreshold.CriticalThreshold)

	// Verify default thresholds exist
	defaultThreshold, exists := thresholds["default"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), 5.0, defaultThreshold.MinorThreshold)
	assert.Equal(suite.T(), 10.0, defaultThreshold.MajorThreshold)
	assert.Equal(suite.T(), 20.0, defaultThreshold.CriticalThreshold)
}

// TestSeverityClassification tests regression severity classification
func (suite *RegressionTestSuite) TestSeverityClassification() {
	testCases := []struct {
		metric        string
		percentChange float64
		expected      string
	}{
		{"latency", 25.0, "medium"},
		{"latency", 35.0, "high"},
		{"latency", 55.0, "critical"},
		{"latency", 3.0, "low"},
		{"error_rate", 2.0, "minor"}, // Absolute values for error rate
		{"error_rate", 7.0, "major"},
		{"error_rate", 12.0, "critical"},
		{"throughput", 15.0, "medium"},
		{"throughput", 25.0, "high"},
		{"throughput", 45.0, "critical"},
	}

	for _, tc := range testCases {
		suite.T().Run(fmt.Sprintf("%s_%.1f", tc.metric, tc.percentChange), func(t *testing.T) {
			severity := suite.detector.GetSeverity(tc.metric, tc.percentChange)
			assert.Equal(t, tc.expected, severity)
		})
	}
}

// TestSequentialComparison tests sequential run comparison
func (suite *RegressionTestSuite) TestSequentialComparison() {
	// Create current run
	currentRun := suite.createTestRun("current-run", "seq-test")
	currentRun.AvgLatencyMs = 200.0 // Worse than previous
	currentRun.OverallErrorRate = 0.05

	// Create previous runs
	previousRuns := []*types.HistoricRun{
		suite.createTestRun("prev-run-1", "seq-test"), // Most recent
		suite.createTestRun("prev-run-2", "seq-test"),
		suite.createTestRun("prev-run-3", "seq-test"),
	}

	// Set timestamps to ensure proper ordering
	baseTime := time.Now()
	currentRun.Timestamp = baseTime
	for i, run := range previousRuns {
		run.Timestamp = baseTime.Add(time.Duration(-(i + 1)) * time.Hour)
		run.AvgLatencyMs = 150.0 // Better baseline performance
		run.OverallErrorRate = 0.02
	}

	// Mock storage expectations
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "current-run").Return(currentRun, nil)
	allRuns := append([]*types.HistoricRun{currentRun}, previousRuns...)
	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "seq-test", 4).Return(allRuns, nil)

	// Test sequential comparison
	options := DetectionOptions{
		ComparisonMode: "sequential",
		LookbackCount:  3,
	}

	regressions, err := suite.detector.CompareToSequential(suite.ctx, "current-run", 3)

	require.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), regressions)

	// Verify regression detection
	found := false
	for _, regression := range regressions {
		if regression.Metric == "avg_latency" {
			found = true
			assert.Equal(suite.T(), "current-run", regression.RunID)
			assert.Equal(suite.T(), "prev-run-1", regression.BaselineRunID) // Most recent previous
			assert.Equal(suite.T(), 150.0, regression.BaselineValue)
			assert.Equal(suite.T(), 200.0, regression.CurrentValue)
			expectedChange := ((200.0 - 150.0) / 150.0) * 100 // 33.33%
			assert.InDelta(suite.T(), expectedChange, regression.PercentChange, 0.1)
		}
	}
	assert.True(suite.T(), found, "Should detect latency regression")
}

// TestBaselineComparison tests baseline comparison mode
func (suite *RegressionTestSuite) TestBaselineComparison() {
	// Create test regressions that baseline manager should return
	expectedRegressions := []*types.Regression{
		{
			ID:             "reg-1",
			RunID:          "current-run",
			BaselineRunID:  "baseline-run",
			Client:         "geth",
			Metric:         "avg_latency",
			BaselineValue:  150.0,
			CurrentValue:   225.0,
			PercentChange:  50.0,
			AbsoluteChange: 75.0,
			Severity:       "critical",
			IsSignificant:  true,
			DetectedAt:     time.Now(),
		},
	}

	// Mock baseline manager expectations
	suite.mockBaselineManager.On("DetectRegressions", suite.ctx, "current-run", "test-baseline", mock.AnythingOfType("RegressionThresholds")).Return(expectedRegressions, nil)

	// Test baseline comparison
	regressions, err := suite.detector.CompareToBaseline(suite.ctx, "current-run", "test-baseline")

	require.NoError(suite.T(), err)
	assert.Len(suite.T(), regressions, 1)
	assert.Equal(suite.T(), expectedRegressions[0].ID, regressions[0].ID)
	assert.Equal(suite.T(), expectedRegressions[0].Severity, regressions[0].Severity)
}

// TestRollingAverageComparison tests rolling average comparison
func (suite *RegressionTestSuite) TestRollingAverageComparison() {
	// Create current run with worse performance
	currentRun := suite.createTestRun("current-run", "rolling-test")
	currentRun.AvgLatencyMs = 300.0
	currentRun.OverallErrorRate = 0.08

	// Create window of previous runs with better performance
	windowRuns := []*types.HistoricRun{currentRun}
	baseTime := time.Now()
	currentRun.Timestamp = baseTime

	for i := 1; i <= 5; i++ {
		run := suite.createTestRun(fmt.Sprintf("window-run-%d", i), "rolling-test")
		run.Timestamp = baseTime.Add(time.Duration(-i) * time.Hour)
		run.AvgLatencyMs = 150.0 + float64(i)*2.0 // Slight variation around 150ms
		run.OverallErrorRate = 0.02 + float64(i)*0.001
		windowRuns = append(windowRuns, run)
	}

	// Mock storage expectations
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "current-run").Return(currentRun, nil)
	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "rolling-test", 6).Return(windowRuns, nil)

	// Test rolling average comparison
	regressions, err := suite.detector.CompareToRollingAverage(suite.ctx, "current-run", 5)

	require.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), regressions)

	// Verify regression against rolling average
	foundLatencyRegression := false
	for _, regression := range regressions {
		if regression.Metric == "avg_latency" {
			foundLatencyRegression = true
			assert.Equal(suite.T(), "current-run", regression.RunID)
			assert.Equal(suite.T(), "rolling_average", regression.BaselineRunID)
			// Rolling average should be around 156ms ((150+152+154+156+158)/5)
			assert.InDelta(suite.T(), 154.0, regression.BaselineValue, 5.0)
			assert.Equal(suite.T(), 300.0, regression.CurrentValue)
			// Should be a significant regression
			assert.True(suite.T(), regression.PercentChange > 50.0)
		}
	}
	assert.True(suite.T(), foundLatencyRegression, "Should detect latency regression against rolling average")
}

// TestComprehensiveRegressionDetection tests the main regression detection workflow
func (suite *RegressionTestSuite) TestComprehensiveRegressionDetection() {
	// Create test run with multiple regressions
	currentRun := suite.createTestRunWithRegressions("comprehensive-run", "comprehensive-test")

	// Mock storage expectations
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "comprehensive-run").Return(currentRun, nil)

	// Create previous runs for comparison
	previousRuns := []*types.HistoricRun{
		suite.createTestRun("prev-run", "comprehensive-test"),
	}
	previousRuns[0].Timestamp = time.Now().Add(-1 * time.Hour)

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "comprehensive-test", 2).Return(append([]*types.HistoricRun{currentRun}, previousRuns...), nil)

	// Test comprehensive regression detection
	options := DetectionOptions{
		ComparisonMode:     "sequential",
		LookbackCount:      1,
		EnableStatistical:  true,
		MinConfidence:      0.5,
		IgnoreImprovements: false,
	}

	report, err := suite.detector.DetectRegressions(suite.ctx, "comprehensive-run", options)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), report)
	assert.Equal(suite.T(), "comprehensive-run", report.RunID)
	assert.Equal(suite.T(), "comprehensive-test", report.TestName)
	assert.Equal(suite.T(), "sequential", report.ComparisonMode)

	// Verify summary statistics
	assert.True(suite.T(), report.Summary.TotalRegressions > 0)
	assert.True(suite.T(), report.Summary.OverallHealthScore < 100.0)
	assert.NotEmpty(suite.T(), report.Summary.RecommendedAction)

	// Verify client analysis
	assert.NotEmpty(suite.T(), report.ClientAnalysis)
	for clientName, analysis := range report.ClientAnalysis {
		assert.NotEmpty(suite.T(), clientName)
		assert.NotNil(suite.T(), analysis)
		assert.True(suite.T(), analysis.HealthScore >= 0.0 && analysis.HealthScore <= 100.0)
		assert.Contains(suite.T(), []string{"improved", "degraded", "stable"}, analysis.OverallStatus)
		assert.Contains(suite.T(), []string{"low", "medium", "high", "critical"}, analysis.RiskLevel)
	}

	// Verify risk assessment
	assert.NotNil(suite.T(), report.RiskAssessment)
	assert.Contains(suite.T(), []string{"low", "medium", "high", "critical"}, report.RiskAssessment.OverallRisk)
	assert.True(suite.T(), report.RiskAssessment.RiskScore >= 0.0 && report.RiskAssessment.RiskScore <= 100.0)
	assert.NotEmpty(suite.T(), report.RiskAssessment.ImpactAssessment)

	// Verify recommendations
	assert.NotEmpty(suite.T(), report.Recommendations)
}

// TestRunAnalysis tests comprehensive run analysis
func (suite *RegressionTestSuite) TestRunAnalysis() {
	// Create test run for analysis
	testRun := suite.createTestRunWithMetrics("analysis-run", "analysis-test")

	// Mock storage expectations
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "analysis-run").Return(testRun, nil)

	// Mock trend data (optional)
	trendData := &types.HistoricTrend{
		TestName:   "analysis-test",
		Client:     "overall",
		Metric:     "avg_latency",
		Points:     []types.TrendPoint{},
		Trend:      "stable",
		TrendSlope: 0.1,
		R2:         0.6,
	}
	suite.mockStorage.On("GetHistoricTrends", suite.ctx, "analysis-test", "overall", "avg_latency", 30).Return(trendData, nil)

	// Test run analysis
	analysis, err := suite.detector.AnalyzeRun(suite.ctx, "analysis-run")

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), analysis)
	assert.Equal(suite.T(), "analysis-run", analysis.RunID)
	assert.Equal(suite.T(), "analysis-test", analysis.TestName)

	// Verify health and performance scores
	assert.True(suite.T(), analysis.OverallHealthScore >= 0.0 && analysis.OverallHealthScore <= 100.0)
	assert.True(suite.T(), analysis.PerformanceScore >= 0.0 && analysis.PerformanceScore <= 100.0)

	// Verify client scores
	assert.NotEmpty(suite.T(), analysis.ClientScores)
	for client, score := range analysis.ClientScores {
		assert.NotEmpty(suite.T(), client)
		assert.True(suite.T(), score >= 0.0 && score <= 100.0)
	}

	// Verify quality metrics
	assert.NotNil(suite.T(), analysis.QualityMetrics)
	assert.True(suite.T(), analysis.QualityMetrics.OverallQuality >= 0.0 && analysis.QualityMetrics.OverallQuality <= 1.0)
	assert.True(suite.T(), analysis.QualityMetrics.DataCompleteness >= 0.0 && analysis.QualityMetrics.DataCompleteness <= 1.0)
	assert.True(suite.T(), analysis.QualityMetrics.ReliabilityScore >= 0.0 && analysis.QualityMetrics.ReliabilityScore <= 1.0)

	// Verify anomaly detection
	assert.NotNil(suite.T(), analysis.Anomalies)

	// Verify trend analysis
	assert.NotNil(suite.T(), analysis.Trends)

	// Verify recommendations
	assert.NotNil(suite.T(), analysis.Recommendations)
}

// TestStatisticalSignificance tests statistical significance calculations
func (suite *RegressionTestSuite) TestStatisticalSignificance() {
	// Test with statistically significant changes
	significantCases := []struct {
		name          string
		baselineValue float64
		currentValue  float64
		sampleSize    int
		expectedSig   bool
	}{
		{
			name:          "large_change_large_sample",
			baselineValue: 100.0,
			currentValue:  150.0, // 50% increase
			sampleSize:    100,
			expectedSig:   true,
		},
		{
			name:          "small_change_large_sample",
			baselineValue: 100.0,
			currentValue:  102.0, // 2% increase
			sampleSize:    1000,
			expectedSig:   false, // Small change, even with large sample
		},
		{
			name:          "large_change_small_sample",
			baselineValue: 100.0,
			currentValue:  200.0, // 100% increase
			sampleSize:    5,
			expectedSig:   true, // Very large change
		},
	}

	for _, tc := range significantCases {
		suite.T().Run(tc.name, func(t *testing.T) {
			percentChange := ((tc.currentValue - tc.baselineValue) / tc.baselineValue) * 100

			// Simple significance test based on magnitude and threshold
			isSignificant := math.Abs(percentChange) > 10.0 // 10% threshold for significance

			if tc.expectedSig {
				assert.True(t, isSignificant || math.Abs(percentChange) > 25.0)
			}
		})
	}
}

// TestCustomThresholds tests custom threshold application
func (suite *RegressionTestSuite) TestCustomThresholds() {
	// Create test run
	currentRun := suite.createTestRun("custom-run", "custom-test")
	currentRun.AvgLatencyMs = 108.0 // 8% increase from baseline 100ms

	baselineRun := suite.createTestRun("baseline-run", "custom-test")
	baselineRun.AvgLatencyMs = 100.0
	baselineRun.Timestamp = time.Now().Add(-1 * time.Hour)

	// Mock storage expectations
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "custom-run").Return(currentRun, nil)
	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "custom-test", 2).Return([]*types.HistoricRun{currentRun, baselineRun}, nil)

	// Test with default thresholds (should NOT trigger regression for 8% change)
	defaultOptions := DetectionOptions{
		ComparisonMode: "sequential",
		LookbackCount:  1,
	}

	defaultRegressions, err := suite.detector.CompareToSequential(suite.ctx, "custom-run", 1)
	require.NoError(suite.T(), err)

	// With default 10% threshold, 8% change should not be detected
	foundRegression := false
	for _, reg := range defaultRegressions {
		if reg.Metric == "avg_latency" && reg.Severity != "low" {
			foundRegression = true
		}
	}
	assert.False(suite.T(), foundRegression, "8% change should not trigger regression with default 10% threshold")

	// Test with custom lower thresholds (should trigger regression for 8% change)
	customThresholds := map[string]RegressionThreshold{
		"avg_latency": {
			MinorThreshold:    5.0, // Lower threshold
			MajorThreshold:    8.0,
			CriticalThreshold: 15.0,
			IsPercentage:      true,
			Direction:         "increase",
		},
	}

	customOptions := DetectionOptions{
		ComparisonMode:   "sequential",
		LookbackCount:    1,
		CustomThresholds: customThresholds,
	}

	// Create fresh mocks for second call
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "custom-run").Return(currentRun, nil)
	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "custom-test", 2).Return([]*types.HistoricRun{currentRun, baselineRun}, nil)

	report, err := suite.detector.DetectRegressions(suite.ctx, "custom-run", customOptions)
	require.NoError(suite.T(), err)

	// With custom 5% threshold, 8% change should be detected
	foundMajorRegression := false
	for _, reg := range report.Regressions {
		if reg.Metric == "avg_latency" && reg.Severity == "major" {
			foundMajorRegression = true
		}
	}
	assert.True(suite.T(), foundMajorRegression, "8% change should trigger major regression with 5% threshold")
}

// TestClientMethodFiltering tests client and method filtering
func (suite *RegressionTestSuite) TestClientMethodFiltering() {
	// Create test run with multiple clients
	currentRun := suite.createTestRunWithMultipleClients("filter-run", "filter-test")
	previousRun := suite.createTestRunWithMultipleClients("prev-run", "filter-test")
	previousRun.Timestamp = time.Now().Add(-1 * time.Hour)

	// Make some metrics worse to trigger regressions
	currentResult := suite.parseFullResults(currentRun.FullResults)
	if gethMetrics, exists := currentResult.ClientMetrics["geth"]; exists {
		gethMetrics.Latency.Avg = 250.0 // Worse than baseline ~150ms
		gethMetrics.ErrorRate = 0.08    // Worse than baseline ~0.02
	}
	if nethermindMetrics, exists := currentResult.ClientMetrics["nethermind"]; exists {
		nethermindMetrics.Latency.Avg = 200.0 // Also worse
		nethermindMetrics.ErrorRate = 0.06
	}

	// Update full results
	updatedJSON, _ := json.Marshal(currentResult)
	currentRun.FullResults = updatedJSON

	// Mock storage expectations
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "filter-run").Return(currentRun, nil)
	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "filter-test", 2).Return([]*types.HistoricRun{currentRun, previousRun}, nil)

	// Test with client filtering (include only geth)
	options := DetectionOptions{
		ComparisonMode: "sequential",
		LookbackCount:  1,
		IncludeClients: []string{"geth"},
	}

	report, err := suite.detector.DetectRegressions(suite.ctx, "filter-run", options)
	require.NoError(suite.T(), err)

	// Verify only geth regressions are included
	for _, regression := range report.Regressions {
		assert.Equal(suite.T(), "geth", regression.Client)
	}

	// Verify client analysis only includes geth
	_, gethExists := report.ClientAnalysis["geth"]
	_, nethermindExists := report.ClientAnalysis["nethermind"]
	assert.True(suite.T(), gethExists)
	assert.False(suite.T(), nethermindExists)

	// Test with client exclusion
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "filter-run").Return(currentRun, nil)
	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "filter-test", 2).Return([]*types.HistoricRun{currentRun, previousRun}, nil)

	optionsExclude := DetectionOptions{
		ComparisonMode: "sequential",
		LookbackCount:  1,
		ExcludeClients: []string{"geth"},
	}

	reportExclude, err := suite.detector.DetectRegressions(suite.ctx, "filter-run", optionsExclude)
	require.NoError(suite.T(), err)

	// Verify geth regressions are excluded
	for _, regression := range reportExclude.Regressions {
		assert.NotEqual(suite.T(), "geth", regression.Client)
	}
}

// TestConcurrentRegressionDetection tests concurrent regression detection operations
func (suite *RegressionTestSuite) TestConcurrentRegressionDetection() {
	// Create test data
	testRun := suite.createTestRun("concurrent-run", "concurrent-test")
	previousRun := suite.createTestRun("prev-run", "concurrent-test")
	previousRun.Timestamp = time.Now().Add(-1 * time.Hour)

	suite.mockStorage.On("GetHistoricRun", suite.ctx, "concurrent-run").Return(testRun, nil).Maybe()
	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "concurrent-test", mock.AnythingOfType("int")).Return([]*types.HistoricRun{testRun, previousRun}, nil).Maybe()

	// Test concurrent detection operations
	done := make(chan bool, 5)
	errors := make(chan error, 5)

	for i := 0; i < 5; i++ {
		go func(id int) {
			defer func() { done <- true }()

			options := DetectionOptions{
				ComparisonMode: "sequential",
				LookbackCount:  1,
			}

			_, err := suite.detector.DetectRegressions(suite.ctx, "concurrent-run", options)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Wait for all operations to complete
	for i := 0; i < 5; i++ {
		<-done
	}

	close(errors)
	for err := range errors {
		suite.T().Errorf("Concurrent regression detection failed: %v", err)
	}
}

// TestRegressionPersistence tests saving and retrieving regressions
func (suite *RegressionTestSuite) TestRegressionPersistence() {
	// Create test regressions
	regressions := []*types.Regression{
		{
			ID:             "test-regression-1",
			RunID:          "test-run",
			BaselineRunID:  "baseline-run",
			Client:         "geth",
			Metric:         "avg_latency",
			Method:         "",
			BaselineValue:  150.0,
			CurrentValue:   225.0,
			PercentChange:  50.0,
			AbsoluteChange: 75.0,
			Severity:       "critical",
			IsSignificant:  true,
			PValue:         0.001,
			DetectedAt:     time.Now(),
			Notes:          "Test regression",
		},
		{
			ID:             "test-regression-2",
			RunID:          "test-run",
			BaselineRunID:  "baseline-run",
			Client:         "nethermind",
			Metric:         "error_rate",
			Method:         "eth_getBalance",
			BaselineValue:  0.02,
			CurrentValue:   0.08,
			PercentChange:  300.0,
			AbsoluteChange: 0.06,
			Severity:       "major",
			IsSignificant:  true,
			PValue:         0.005,
			DetectedAt:     time.Now(),
			Notes:          "Method-specific regression",
		},
	}

	// Test saving regressions
	err := suite.detector.SaveRegressions(suite.ctx, regressions)
	require.NoError(suite.T(), err)

	// Test retrieving regressions
	retrievedRegressions, err := suite.detector.GetRegressions(suite.ctx, "test-run")
	require.NoError(suite.T(), err)
	assert.Len(suite.T(), retrievedRegressions, 2)

	// Verify regression details
	for _, retrieved := range retrievedRegressions {
		var original *types.Regression
		for _, orig := range regressions {
			if orig.ID == retrieved.ID {
				original = orig
				break
			}
		}

		require.NotNil(suite.T(), original)
		assert.Equal(suite.T(), original.RunID, retrieved.RunID)
		assert.Equal(suite.T(), original.BaselineRunID, retrieved.BaselineRunID)
		assert.Equal(suite.T(), original.Client, retrieved.Client)
		assert.Equal(suite.T(), original.Metric, retrieved.Metric)
		assert.Equal(suite.T(), original.Method, retrieved.Method)
		assert.InDelta(suite.T(), original.BaselineValue, retrieved.BaselineValue, 0.001)
		assert.InDelta(suite.T(), original.CurrentValue, retrieved.CurrentValue, 0.001)
		assert.InDelta(suite.T(), original.PercentChange, retrieved.PercentChange, 0.001)
		assert.Equal(suite.T(), original.Severity, retrieved.Severity)
		assert.Equal(suite.T(), original.IsSignificant, retrieved.IsSignificant)
	}
}

// TestRegressionAcknowledgment tests regression acknowledgment functionality
func (suite *RegressionTestSuite) TestRegressionAcknowledgment() {
	// First save a regression
	regression := &types.Regression{
		ID:             "ack-test-regression",
		RunID:          "ack-test-run",
		BaselineRunID:  "ack-baseline-run",
		Client:         "geth",
		Metric:         "avg_latency",
		BaselineValue:  150.0,
		CurrentValue:   225.0,
		PercentChange:  50.0,
		AbsoluteChange: 75.0,
		Severity:       "critical",
		IsSignificant:  true,
		DetectedAt:     time.Now(),
	}

	err := suite.detector.SaveRegressions(suite.ctx, []*types.Regression{regression})
	require.NoError(suite.T(), err)

	// Test acknowledging the regression
	err = suite.detector.AcknowledgeRegression(suite.ctx, "ack-test-regression", "test-user")
	require.NoError(suite.T(), err)

	// Verify acknowledgment was saved
	retrievedRegressions, err := suite.detector.GetRegressions(suite.ctx, "ack-test-run")
	require.NoError(suite.T(), err)
	assert.Len(suite.T(), retrievedRegressions, 1)

	acknowledged := retrievedRegressions[0]
	assert.NotNil(suite.T(), acknowledged.AcknowledgedAt)
	assert.Equal(suite.T(), "test-user", acknowledged.AcknowledgedBy)

	// Test acknowledging non-existent regression
	err = suite.detector.AcknowledgeRegression(suite.ctx, "non-existent-regression", "test-user")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "regression not found")
}

// TestEdgeCasesAndErrorHandling tests various edge cases
func (suite *RegressionTestSuite) TestEdgeCasesAndErrorHandling() {
	// Test with non-existent run
	options := DetectionOptions{
		ComparisonMode: "sequential",
		LookbackCount:  1,
	}

	suite.mockStorage.On("GetHistoricRun", suite.ctx, "non-existent-run").Return(nil, fmt.Errorf("run not found"))

	_, err := suite.detector.DetectRegressions(suite.ctx, "non-existent-run", options)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "failed to get historic run")

	// Test with invalid comparison mode
	invalidOptions := DetectionOptions{
		ComparisonMode: "invalid-mode",
	}

	testRun := suite.createTestRun("valid-run", "valid-test")
	suite.mockStorage.On("GetHistoricRun", suite.ctx, "valid-run").Return(testRun, nil)

	_, err = suite.detector.DetectRegressions(suite.ctx, "valid-run", invalidOptions)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "invalid comparison mode")

	// Test baseline mode without baseline name
	baselineOptions := DetectionOptions{
		ComparisonMode: "baseline",
		BaselineName:   "", // Missing baseline name
	}

	suite.mockStorage.On("GetHistoricRun", suite.ctx, "valid-run").Return(testRun, nil)

	_, err = suite.detector.DetectRegressions(suite.ctx, "valid-run", baselineOptions)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "baseline name required")

	// Test with insufficient historical data
	shortOptions := DetectionOptions{
		ComparisonMode: "sequential",
		LookbackCount:  5,
	}

	suite.mockStorage.On("GetHistoricRun", suite.ctx, "valid-run").Return(testRun, nil)
	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "valid-test", 6).Return([]*types.HistoricRun{testRun}, nil) // Only current run

	regressions, err := suite.detector.CompareToSequential(suite.ctx, "valid-run", 5)
	require.NoError(suite.T(), err)
	assert.Empty(suite.T(), regressions) // Should return empty list, not error
}

// Helper functions for creating test data

func (suite *RegressionTestSuite) createTestRun(runID, testName string) *types.HistoricRun {
	result := suite.createBasicBenchmarkResult()
	resultJSON, _ := json.Marshal(result)

	return &types.HistoricRun{
		ID:               runID,
		TestName:         testName,
		Timestamp:        time.Now(),
		AvgLatencyMs:     150.0,
		P95LatencyMs:     300.0,
		P99LatencyMs:     500.0,
		OverallErrorRate: 0.02,
		TotalRequests:    1000,
		TotalErrors:      20,
		FullResults:      resultJSON,
		Duration:         "10m",
	}
}

func (suite *RegressionTestSuite) createTestRunWithRegressions(runID, testName string) *types.HistoricRun {
	run := suite.createTestRun(runID, testName)

	// Modify to have worse performance
	run.AvgLatencyMs = 250.0 // Significant increase
	run.P95LatencyMs = 500.0
	run.OverallErrorRate = 0.08 // Much higher error rate

	// Update full results to match
	result := suite.parseFullResults(run.FullResults)
	for _, clientMetrics := range result.ClientMetrics {
		clientMetrics.Latency.Avg = 250.0
		clientMetrics.Latency.P95 = 500.0
		clientMetrics.ErrorRate = 0.08
	}
	updatedJSON, _ := json.Marshal(result)
	run.FullResults = updatedJSON

	return run
}

func (suite *RegressionTestSuite) createTestRunWithMetrics(runID, testName string) *types.HistoricRun {
	result := &types.BenchmarkResult{
		TestName:  testName,
		StartTime: time.Now().Add(-10 * time.Minute),
		EndTime:   time.Now(),
		Duration:  10 * time.Minute,
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
			},
			"nethermind": {
				Name:          "nethermind",
				TotalRequests: 500,
				TotalErrors:   15,
				ErrorRate:     0.03,
				Latency: types.LatencyMetrics{
					Avg:        160.0,
					P50:        130.0,
					P95:        320.0,
					P99:        520.0,
					Max:        1100.0,
					Throughput: 48.0,
				},
			},
		},
	}

	resultJSON, _ := json.Marshal(result)

	return &types.HistoricRun{
		ID:               runID,
		TestName:         testName,
		Timestamp:        time.Now(),
		AvgLatencyMs:     155.0, // Average of clients
		P95LatencyMs:     310.0,
		P99LatencyMs:     510.0,
		OverallErrorRate: 0.025,
		TotalRequests:    1000,
		TotalErrors:      25,
		FullResults:      resultJSON,
		Duration:         "10m",
	}
}

func (suite *RegressionTestSuite) createTestRunWithMultipleClients(runID, testName string) *types.HistoricRun {
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:          "geth",
				TotalRequests: 333,
				TotalErrors:   7,
				ErrorRate:     0.02,
				Latency: types.LatencyMetrics{
					Avg:        150.0,
					P95:        300.0,
					P99:        500.0,
					Throughput: 33.3,
				},
				Methods: map[string]types.MetricSummary{
					"eth_getBalance": {
						Count:      167,
						ErrorRate:  0.015,
						Avg:        140.0,
						P95:        280.0,
						Throughput: 16.7,
					},
					"eth_getBlockByNumber": {
						Count:      166,
						ErrorRate:  0.025,
						Avg:        160.0,
						P95:        320.0,
						Throughput: 16.6,
					},
				},
			},
			"nethermind": {
				Name:          "nethermind",
				TotalRequests: 333,
				TotalErrors:   10,
				ErrorRate:     0.03,
				Latency: types.LatencyMetrics{
					Avg:        155.0,
					P95:        310.0,
					P99:        510.0,
					Throughput: 33.3,
				},
				Methods: map[string]types.MetricSummary{
					"eth_getBalance": {
						Count:      167,
						ErrorRate:  0.025,
						Avg:        145.0,
						P95:        290.0,
						Throughput: 16.7,
					},
					"eth_getBlockByNumber": {
						Count:      166,
						ErrorRate:  0.035,
						Avg:        165.0,
						P95:        330.0,
						Throughput: 16.6,
					},
				},
			},
			"erigon": {
				Name:          "erigon",
				TotalRequests: 334,
				TotalErrors:   8,
				ErrorRate:     0.024,
				Latency: types.LatencyMetrics{
					Avg:        148.0,
					P95:        295.0,
					P99:        495.0,
					Throughput: 33.4,
				},
				Methods: map[string]types.MetricSummary{
					"eth_getBalance": {
						Count:      167,
						ErrorRate:  0.018,
						Avg:        138.0,
						P95:        275.0,
						Throughput: 16.7,
					},
					"eth_getBlockByNumber": {
						Count:      167,
						ErrorRate:  0.030,
						Avg:        158.0,
						P95:        315.0,
						Throughput: 16.7,
					},
				},
			},
		},
	}

	resultJSON, _ := json.Marshal(result)

	return &types.HistoricRun{
		ID:               runID,
		TestName:         testName,
		Timestamp:        time.Now(),
		AvgLatencyMs:     151.0, // Average across clients
		P95LatencyMs:     301.7,
		P99LatencyMs:     501.7,
		OverallErrorRate: 0.025,
		TotalRequests:    1000,
		TotalErrors:      25,
		FullResults:      resultJSON,
		Duration:         "10m",
	}
}

func (suite *RegressionTestSuite) createBasicBenchmarkResult() *types.BenchmarkResult {
	return &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:          "geth",
				TotalRequests: 500,
				TotalErrors:   10,
				ErrorRate:     0.02,
				Latency: types.LatencyMetrics{
					Avg:        150.0,
					P95:        300.0,
					P99:        500.0,
					Throughput: 50.0,
				},
			},
			"nethermind": {
				Name:          "nethermind",
				TotalRequests: 500,
				TotalErrors:   10,
				ErrorRate:     0.02,
				Latency: types.LatencyMetrics{
					Avg:        150.0,
					P95:        300.0,
					P99:        500.0,
					Throughput: 50.0,
				},
			},
		},
	}
}

func (suite *RegressionTestSuite) parseFullResults(fullResults json.RawMessage) *types.BenchmarkResult {
	var result types.BenchmarkResult
	json.Unmarshal(fullResults, &result)
	return &result
}

// Benchmark tests for performance validation

func BenchmarkRegressionDetection(b *testing.B) {
	mockStorage := new(MockHistoricStorage)
	mockBaselineManager := new(MockBaselineManager)
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	detector := NewRegressionDetector(mockStorage, mockBaselineManager, db, logger)
	detector.Start(context.Background())

	// Create test data
	currentRun := &types.HistoricRun{
		ID:               "bench-current",
		TestName:         "benchmark-test",
		Timestamp:        time.Now(),
		AvgLatencyMs:     200.0,
		OverallErrorRate: 0.05,
		FullResults:      json.RawMessage(`{"client_metrics":{"test":{"latency":{"avg":200},"error_rate":0.05}}}`),
	}

	previousRun := &types.HistoricRun{
		ID:               "bench-previous",
		TestName:         "benchmark-test",
		Timestamp:        time.Now().Add(-1 * time.Hour),
		AvgLatencyMs:     150.0,
		OverallErrorRate: 0.02,
		FullResults:      json.RawMessage(`{"client_metrics":{"test":{"latency":{"avg":150},"error_rate":0.02}}}`),
	}

	mockStorage.On("GetHistoricRun", mock.Anything, "bench-current").Return(currentRun, nil)
	mockStorage.On("ListHistoricRuns", mock.Anything, "benchmark-test", 2).Return([]*types.HistoricRun{currentRun, previousRun}, nil)

	options := DetectionOptions{
		ComparisonMode: "sequential",
		LookbackCount:  1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := detector.DetectRegressions(context.Background(), "bench-current", options)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunAnalysis(b *testing.B) {
	mockStorage := new(MockHistoricStorage)
	mockBaselineManager := new(MockBaselineManager)
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	detector := NewRegressionDetector(mockStorage, mockBaselineManager, db, logger)
	detector.Start(context.Background())

	// Create complex test run
	complexResult := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{},
	}

	// Add multiple clients for comprehensive analysis
	for i := 0; i < 5; i++ {
		clientName := fmt.Sprintf("client-%d", i)
		complexResult.ClientMetrics[clientName] = &types.ClientMetrics{
			Name:          clientName,
			TotalRequests: 1000,
			TotalErrors:   20,
			ErrorRate:     0.02,
			Latency: types.LatencyMetrics{
				Avg:        150.0 + float64(i)*10,
				P95:        300.0 + float64(i)*20,
				P99:        500.0 + float64(i)*30,
				Throughput: 100.0 - float64(i)*5,
			},
		}
	}

	resultJSON, _ := json.Marshal(complexResult)
	testRun := &types.HistoricRun{
		ID:               "complex-run",
		TestName:         "complex-test",
		Timestamp:        time.Now(),
		AvgLatencyMs:     170.0,
		OverallErrorRate: 0.02,
		FullResults:      resultJSON,
	}

	mockStorage.On("GetHistoricRun", mock.Anything, "complex-run").Return(testRun, nil)
	mockStorage.On("GetHistoricTrends", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, fmt.Errorf("no trends"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := detector.AnalyzeRun(context.Background(), "complex-run")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Run the test suite
func TestRegressionTestSuite(t *testing.T) {
	suite.Run(t, new(RegressionTestSuite))
}

// Test mathematical accuracy of regression calculations
func TestRegressionCalculationAccuracy(t *testing.T) {
	testCases := []struct {
		name        string
		baseline    float64
		current     float64
		expectedPct float64
		expectedAbs float64
	}{
		{"50% increase", 100.0, 150.0, 50.0, 50.0},
		{"25% decrease", 200.0, 150.0, -25.0, -50.0},
		{"100% increase", 50.0, 100.0, 100.0, 50.0},
		{"no change", 100.0, 100.0, 0.0, 0.0},
		{"small increase", 1000.0, 1005.0, 0.5, 5.0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			absoluteChange := tc.current - tc.baseline
			percentChange := 0.0
			if tc.baseline != 0 {
				percentChange = (absoluteChange / tc.baseline) * 100
			}

			assert.InDelta(t, tc.expectedPct, percentChange, 0.001)
			assert.InDelta(t, tc.expectedAbs, absoluteChange, 0.001)
		})
	}
}

func TestSeverityThresholds(t *testing.T) {
	// Test default threshold values
	detector := &regressionDetector{
		thresholds: make(map[string]RegressionThreshold),
	}
	detector.initializeDefaultThresholds()

	// Test latency thresholds
	latencyThreshold := detector.thresholds["latency"]
	assert.Equal(t, 5.0, latencyThreshold.MinorThreshold)
	assert.Equal(t, 15.0, latencyThreshold.MajorThreshold)
	assert.Equal(t, 30.0, latencyThreshold.CriticalThreshold)
	assert.Equal(t, "increase", latencyThreshold.Direction)

	// Test error rate thresholds
	errorThreshold := detector.thresholds["error_rate"]
	assert.Equal(t, 1.0, errorThreshold.MinorThreshold)
	assert.Equal(t, 5.0, errorThreshold.MajorThreshold)
	assert.Equal(t, 10.0, errorThreshold.CriticalThreshold)
	assert.Equal(t, "increase", errorThreshold.Direction)
	assert.False(t, errorThreshold.IsPercentage) // Absolute values

	// Test throughput thresholds
	throughputThreshold := detector.thresholds["throughput"]
	assert.Equal(t, 5.0, throughputThreshold.MinorThreshold)
	assert.Equal(t, 15.0, throughputThreshold.MajorThreshold)
	assert.Equal(t, 30.0, throughputThreshold.CriticalThreshold)
	assert.Equal(t, "decrease", throughputThreshold.Direction)
}

func TestRiskScoreCalculation(t *testing.T) {
	// Test risk score calculation logic
	testCases := []struct {
		criticalCount int
		majorCount    int
		minorCount    int
		expectedRisk  string
	}{
		{2, 0, 0, "critical"}, // 2 * 40 = 80 points
		{1, 1, 0, "high"},     // 40 + 20 = 60 points
		{0, 2, 1, "medium"},   // 20 + 20 + 5 = 45 points
		{0, 1, 2, "medium"},   // 20 + 5 + 5 = 30 points
		{0, 0, 3, "low"},      // 5 + 5 + 5 = 15 points
		{0, 0, 0, "low"},      // 0 points
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("c%d_m%d_mi%d", tc.criticalCount, tc.majorCount, tc.minorCount), func(t *testing.T) {
			score := float64(tc.criticalCount)*40.0 + float64(tc.majorCount)*20.0 + float64(tc.minorCount)*5.0

			var expectedRisk string
			if score >= 80 {
				expectedRisk = "critical"
			} else if score >= 60 {
				expectedRisk = "high"
			} else if score >= 30 {
				expectedRisk = "medium"
			} else {
				expectedRisk = "low"
			}

			assert.Equal(t, tc.expectedRisk, expectedRisk)
		})
	}
}

func TestConfidenceCalculation(t *testing.T) {
	// Test confidence calculation factors
	testCases := []struct {
		dataPoints    int
		rSquared      float64
		volatility    float64
		minConfidence float64
	}{
		{100, 0.9, 0.1, 0.8}, // High data quality, good fit, low volatility
		{50, 0.7, 0.2, 0.6},  // Medium quality
		{20, 0.5, 0.4, 0.4},  // Lower quality
		{10, 0.3, 0.6, 0.2},  // Poor quality
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("dp%d_r2%.1f_vol%.1f", tc.dataPoints, tc.rSquared, tc.volatility), func(t *testing.T) {
			dataQuality := math.Min(float64(tc.dataPoints)/100.0, 1.0)
			fitQuality := tc.rSquared
			stabilityQuality := 1 - tc.volatility

			confidence := (dataQuality + fitQuality + stabilityQuality) / 3.0

			assert.True(t, confidence >= tc.minConfidence)
			assert.True(t, confidence >= 0.0 && confidence <= 1.0)
		})
	}
}
