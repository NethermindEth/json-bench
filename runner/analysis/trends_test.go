package analysis

import (
	"context"
	"database/sql"
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

// TrendTestSuite contains all trend analysis tests
type TrendTestSuite struct {
	suite.Suite
	mockStorage *MockHistoricStorage
	mockDB      *sql.DB
	analyzer    TrendAnalyzer
	ctx         context.Context
}

func (suite *TrendTestSuite) SetupTest() {
	suite.mockStorage = new(MockHistoricStorage)
	suite.ctx = context.Background()

	// Create in-memory SQLite database for testing
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(suite.T(), err)
	suite.mockDB = db

	// Create trend analyzer
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise in tests
	suite.analyzer = NewTrendAnalyzer(suite.mockStorage, suite.mockDB, logger)

	// Start the analyzer
	err = suite.analyzer.Start(suite.ctx)
	require.NoError(suite.T(), err)
}

func (suite *TrendTestSuite) TearDownTest() {
	if suite.mockDB != nil {
		suite.mockDB.Close()
	}
	suite.analyzer.Stop()
	suite.mockStorage.AssertExpectations(suite.T())
}

// TestTrendCalculation tests basic trend calculation functionality
func (suite *TrendTestSuite) TestTrendCalculation() {
	// Create trending data - improving performance (decreasing latency)
	runs := suite.createTrendingRuns("trend-test", 10, func(i int) *types.HistoricRun {
		run := suite.createBasicHistoricRun(i, "trend-test")
		// Simulate improving performance over time
		run.AvgLatencyMs = 200.0 - float64(i)*5.0      // Decreasing from 200ms to 155ms
		run.P95LatencyMs = 400.0 - float64(i)*10.0     // Decreasing from 400ms to 310ms
		run.OverallErrorRate = 0.05 - float64(i)*0.002 // Decreasing from 5% to 3.2%
		return run
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "trend-test", mock.AnythingOfType("int")).Return(runs, nil)

	// Test trend calculation
	result, err := suite.analyzer.CalculateTrends(suite.ctx, "trend-test", 10)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), result)
	assert.Equal(suite.T(), "trend-test", result.TestName)
	assert.Equal(suite.T(), 10, result.Period)
	assert.NotEmpty(suite.T(), result.Metrics)
	assert.NotEmpty(suite.T(), result.Trends)

	// Check avg_latency trend (should be improving)
	avgLatencyTrend, exists := result.Trends["avg_latency"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "improving", avgLatencyTrend.Direction.Direction)
	assert.True(suite.T(), avgLatencyTrend.Direction.Slope < 0) // Negative slope = decreasing latency = improving

	// Check error_rate trend (should be improving)
	errorRateTrend, exists := result.Trends["error_rate"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "improving", errorRateTrend.Direction.Direction)

	// Verify insights and recommendations are generated
	assert.NotEmpty(suite.T(), result.Insights)
	assert.NotEmpty(suite.T(), result.Recommendations)
}

// TestDegradingTrendDetection tests detection of degrading performance trends
func (suite *TrendTestSuite) TestDegradingTrendDetection() {
	// Create degrading trend data
	runs := suite.createTrendingRuns("degrading-test", 8, func(i int) *types.HistoricRun {
		run := suite.createBasicHistoricRun(i, "degrading-test")
		// Simulate degrading performance over time
		run.AvgLatencyMs = 150.0 + float64(i)*10.0     // Increasing from 150ms to 220ms
		run.P95LatencyMs = 300.0 + float64(i)*20.0     // Increasing from 300ms to 440ms
		run.OverallErrorRate = 0.02 + float64(i)*0.005 // Increasing from 2% to 5.5%
		return run
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "degrading-test", mock.AnythingOfType("int")).Return(runs, nil)

	result, err := suite.analyzer.CalculateTrends(suite.ctx, "degrading-test", 10)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), result)

	// Check that degrading trends are detected
	avgLatencyTrend, exists := result.Trends["avg_latency"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "degrading", avgLatencyTrend.Direction.Direction)
	assert.True(suite.T(), avgLatencyTrend.Direction.Slope > 0) // Positive slope = increasing latency = degrading

	// Verify overall summary reflects degrading trend
	assert.Equal(suite.T(), "degrading", result.Summary.OverallDirection)
	assert.Contains(suite.T(), []string{"poor", "concerning"}, result.Summary.OverallHealth)
}

// TestStableTrendDetection tests detection of stable performance trends
func (suite *TrendTestSuite) TestStableTrendDetection() {
	// Create stable trend data
	runs := suite.createTrendingRuns("stable-test", 10, func(i int) *types.HistoricRun {
		run := suite.createBasicHistoricRun(i, "stable-test")
		// Simulate stable performance with minor fluctuations
		baseLatency := 150.0
		fluctuation := math.Sin(float64(i)*0.5) * 2.0 // Small sine wave fluctuation
		run.AvgLatencyMs = baseLatency + fluctuation
		run.P95LatencyMs = 300.0 + fluctuation*2.0
		run.OverallErrorRate = 0.02 + fluctuation*0.001
		return run
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "stable-test", mock.AnythingOfType("int")).Return(runs, nil)

	result, err := suite.analyzer.CalculateTrends(suite.ctx, "stable-test", 10)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), result)

	// Check that stable trends are detected
	avgLatencyTrend, exists := result.Trends["avg_latency"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "stable", avgLatencyTrend.Direction.Direction)
	assert.True(suite.T(), math.Abs(avgLatencyTrend.Direction.Slope) < 1.0) // Very small slope

	// Verify overall summary reflects stable trend
	assert.Equal(suite.T(), "stable", result.Summary.OverallDirection)
	assert.Contains(suite.T(), []string{"good", "excellent"}, result.Summary.OverallHealth)
}

// TestMovingAverageCalculation tests moving average calculations
func (suite *TrendTestSuite) TestMovingAverageCalculation() {
	// Create test data with known values for mathematical verification
	runs := suite.createTrendingRuns("ma-test", 15, func(i int) *types.HistoricRun {
		run := suite.createBasicHistoricRun(i, "ma-test")
		run.AvgLatencyMs = float64(100 + i*10) // Linear increase: 100, 110, 120, ...
		return run
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "ma-test", mock.AnythingOfType("int")).Return(runs, nil)

	// Test moving average calculation with window size 5
	result, err := suite.analyzer.CalculateMovingAverage(suite.ctx, "ma-test", "avg_latency", 5, 15)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), result)
	assert.Equal(suite.T(), 5, result.WindowSize)
	assert.NotEmpty(suite.T(), result.Points)

	// Verify mathematical accuracy of moving average
	// For the 5th point (index 4), the moving average should be (100+110+120+130+140)/5 = 120
	if len(result.Points) >= 1 {
		expectedMA := (100.0 + 110.0 + 120.0 + 130.0 + 140.0) / 5.0
		assert.InDelta(suite.T(), expectedMA, result.Points[0].MovingAverage, 0.001)
	}

	// Verify trend clarity and smoothness calculations
	assert.True(suite.T(), result.TrendClarity >= 0.0 && result.TrendClarity <= 1.0)
	assert.True(suite.T(), result.Smoothness >= 0.0 && result.Smoothness <= 1.0)
}

// TestLinearRegressionAccuracy tests mathematical accuracy of linear regression
func (suite *TrendTestSuite) TestLinearRegressionAccuracy() {
	// Create perfect linear data for regression testing
	dataPoints := make([]TrendDataPoint, 10)
	for i := 0; i < 10; i++ {
		dataPoints[i] = TrendDataPoint{
			Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
			Value:     float64(i*2 + 5), // y = 2x + 5 (slope=2, intercept=5)
			RunID:     fmt.Sprintf("run-%d", i),
		}
	}

	// Test linear regression calculation
	analyzer := suite.analyzer.(*trendAnalyzer)
	regression := analyzer.calculateLinearRegression(dataPoints)

	// Verify mathematical accuracy
	assert.InDelta(suite.T(), 2.0, regression.Slope, 0.001)
	assert.InDelta(suite.T(), 5.0, regression.Intercept, 0.001)
	assert.True(suite.T(), regression.RSquared > 0.99) // Should be nearly perfect for linear data
	assert.True(suite.T(), regression.Significant)
	assert.Equal(suite.T(), "y = 2.0000x + 5.0000", regression.Equation)
}

// TestTrendDirectionDetection tests trend direction detection logic
func (suite *TrendTestSuite) TestTrendDirectionDetection() {
	// Create test data with known trend direction
	runs := suite.createTrendingRuns("direction-test", 10, func(i int) *types.HistoricRun {
		run := suite.createBasicHistoricRun(i, "direction-test")
		run.AvgLatencyMs = 200.0 - float64(i)*5.0 // Strong decreasing trend
		return run
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "direction-test", mock.AnythingOfType("int")).Return(runs, nil)

	// Test trend direction detection
	direction, err := suite.analyzer.DetectTrendDirection(suite.ctx, "direction-test", "avg_latency", 10)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), direction)
	assert.Equal(suite.T(), "improving", direction.Direction) // Decreasing latency is improving
	assert.Contains(suite.T(), []string{"moderate", "strong", "very_strong"}, direction.Strength)
	assert.True(suite.T(), direction.Confidence > 0.5)
	assert.True(suite.T(), direction.Slope < 0) // Negative slope for decreasing values
	assert.True(suite.T(), direction.TrendScore > 0)
}

// TestAnomalyDetection tests anomaly detection algorithms
func (suite *TrendTestSuite) TestAnomalyDetection() {
	// Create data with clear anomalies
	runs := suite.createTrendingRuns("anomaly-test", 20, func(i int) *types.HistoricRun {
		run := suite.createBasicHistoricRun(i, "anomaly-test")
		if i == 10 {
			// Insert anomaly at position 10
			run.AvgLatencyMs = 1000.0   // Spike
			run.OverallErrorRate = 0.50 // 50% error rate spike
		} else {
			// Normal values
			run.AvgLatencyMs = 150.0 + float64(i)*1.0
			run.OverallErrorRate = 0.02 + float64(i)*0.001
		}
		return run
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "anomaly-test", mock.AnythingOfType("int")).Return(runs, nil)

	// Test anomaly detection
	result, err := suite.analyzer.DetectAnomalies(suite.ctx, "anomaly-test", 20)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), result)
	assert.NotEmpty(suite.T(), result.Anomalies)

	// Verify anomalies are detected correctly
	found := false
	for _, anomaly := range result.Anomalies {
		if anomaly.RunID == "run-10" { // The anomalous run
			found = true
			assert.Contains(suite.T(), []string{"moderate", "severe"}, anomaly.Severity)
			assert.True(suite.T(), anomaly.DeviationScore > 2.0) // Significant deviation
		}
	}
	assert.True(suite.T(), found, "Anomaly should be detected in run-10")

	// Verify anomaly rate calculation
	assert.True(suite.T(), result.AnomalyRate > 0.0)
	assert.True(suite.T(), result.AnomalyRate < 1.0)
}

// TestForecastingAccuracy tests trend forecasting capabilities
func (suite *TrendTestSuite) TestForecastingAccuracy() {
	// Create predictable linear trend for forecasting
	runs := suite.createTrendingRuns("forecast-test", 15, func(i int) *types.HistoricRun {
		run := suite.createBasicHistoricRun(i, "forecast-test")
		run.AvgLatencyMs = 100.0 + float64(i)*5.0 // Linear increase: 100, 105, 110, ...
		return run
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "forecast-test", mock.AnythingOfType("int")).Return(runs, nil)

	// Test forecasting
	forecast, err := suite.analyzer.ForecastTrend(suite.ctx, "forecast-test", "avg_latency", 15, 5)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), forecast)
	assert.Equal(suite.T(), "linear_regression", forecast.Method)
	assert.Len(suite.T(), forecast.Predictions, 5)

	// Verify forecast accuracy for linear data
	// Next point should be approximately 100 + 15*5 = 175
	expectedNextValue := 175.0
	assert.InDelta(suite.T(), expectedNextValue, forecast.Predictions[0].PredictedValue, 5.0)

	// Verify confidence intervals
	for _, prediction := range forecast.Predictions {
		assert.True(suite.T(), prediction.UpperBound > prediction.PredictedValue)
		assert.True(suite.T(), prediction.LowerBound < prediction.PredictedValue)
		assert.True(suite.T(), prediction.Confidence >= 0.0 && prediction.Confidence <= 1.0)
	}

	// Verify validation metrics
	assert.True(suite.T(), forecast.Validation.MAE >= 0.0)
	assert.True(suite.T(), forecast.Validation.RMSE >= 0.0)
	assert.True(suite.T(), forecast.Validation.MAPE >= 0.0)
}

// TestStatisticalCalculations tests various statistical calculations
func (suite *TrendTestSuite) TestStatisticalCalculations() {
	// Create test data with known statistical properties
	runs := suite.createTrendingRuns("stats-test", 100, func(i int) *types.HistoricRun {
		run := suite.createBasicHistoricRun(i, "stats-test")
		// Create normal distribution around 150ms
		run.AvgLatencyMs = 150.0 + math.Sin(float64(i)*0.1)*10.0
		return run
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "stats-test", mock.AnythingOfType("int")).Return(runs, nil)

	// Test statistics calculation
	stats, err := suite.analyzer.CalculateStatistics(suite.ctx, "stats-test", 30)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), stats)
	assert.Equal(suite.T(), 100, stats.Count)

	// Verify basic statistics
	assert.True(suite.T(), stats.Mean > 0)
	assert.True(suite.T(), stats.Median > 0)
	assert.True(suite.T(), stats.StandardDev >= 0)
	assert.True(suite.T(), stats.Variance >= 0)
	assert.True(suite.T(), stats.Min <= stats.Max)

	// Verify percentiles are in order
	assert.True(suite.T(), stats.Percentiles["p10"] <= stats.Percentiles["p25"])
	assert.True(suite.T(), stats.Percentiles["p25"] <= stats.Percentiles["p50"])
	assert.True(suite.T(), stats.Percentiles["p50"] <= stats.Percentiles["p75"])
	assert.True(suite.T(), stats.Percentiles["p75"] <= stats.Percentiles["p90"])
	assert.True(suite.T(), stats.Percentiles["p90"] <= stats.Percentiles["p95"])
	assert.True(suite.T(), stats.Percentiles["p95"] <= stats.Percentiles["p99"])

	// Verify median equals p50
	assert.InDelta(suite.T(), stats.Median, stats.Percentiles["p50"], 0.001)

	// Verify autocorrelation calculation
	assert.NotEmpty(suite.T(), stats.Autocorrelation)
	assert.InDelta(suite.T(), 1.0, stats.Autocorrelation[0], 0.001) // First value should be 1.0
}

// TestMethodTrends tests method-specific trend analysis
func (suite *TrendTestSuite) TestMethodTrends() {
	// Create test data for method analysis
	runs := suite.createTrendingRuns("method-test", 10, func(i int) *types.HistoricRun {
		return suite.createBasicHistoricRun(i, "method-test")
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "method-test", mock.AnythingOfType("int")).Return(runs, nil)

	// Test method trend analysis
	result, err := suite.analyzer.GetMethodTrends(suite.ctx, "method-test", "eth_getBalance", 10)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), result)
	assert.Equal(suite.T(), "method-test", result.TestName)
	assert.Equal(suite.T(), "eth_getBalance", result.Method)
	assert.NotEmpty(suite.T(), result.Trends)

	// Verify method-specific trends are calculated
	assert.NotNil(suite.T(), result.Comparison)
	assert.NotNil(suite.T(), result.Ranking)
}

// TestClientTrends tests client-specific trend analysis
func (suite *TrendTestSuite) TestClientTrends() {
	// Create test data for client analysis
	runs := suite.createTrendingRuns("client-test", 10, func(i int) *types.HistoricRun {
		return suite.createBasicHistoricRun(i, "client-test")
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "client-test", mock.AnythingOfType("int")).Return(runs, nil)

	// Test client trend analysis
	result, err := suite.analyzer.GetClientTrends(suite.ctx, "client-test", "geth", 10)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), result)
	assert.Equal(suite.T(), "client-test", result.TestName)
	assert.Equal(suite.T(), "geth", result.Client)
	assert.NotEmpty(suite.T(), result.Trends)

	// Verify client-specific trends are calculated
	assert.NotNil(suite.T(), result.Comparison)
	assert.NotNil(suite.T(), result.Ranking)
}

// TestInsightGeneration tests automatic insight generation
func (suite *TrendTestSuite) TestInsightGeneration() {
	// Create data with specific patterns for insight generation
	runs := suite.createTrendingRuns("insight-test", 15, func(i int) *types.HistoricRun {
		run := suite.createBasicHistoricRun(i, "insight-test")
		if i >= 10 {
			// Introduce degradation in last 5 runs
			run.AvgLatencyMs = 150.0 + float64(i-9)*20.0
		} else {
			// Stable performance in first 10 runs
			run.AvgLatencyMs = 150.0 + float64(i)*1.0
		}
		return run
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "insight-test", mock.AnythingOfType("int")).Return(runs, nil)

	result, err := suite.analyzer.CalculateTrends(suite.ctx, "insight-test", 15)

	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), result)
	assert.NotEmpty(suite.T(), result.Insights)

	// Verify insights contain relevant information
	foundDegradationInsight := false
	for _, insight := range result.Insights {
		assert.NotEmpty(suite.T(), insight.Title)
		assert.NotEmpty(suite.T(), insight.Description)
		assert.Contains(suite.T(), []string{"low", "medium", "high", "critical"}, insight.Severity)
		assert.True(suite.T(), insight.Confidence >= 0.0 && insight.Confidence <= 1.0)

		if insight.Type == "performance_degradation" {
			foundDegradationInsight = true
		}
	}

	// Should detect degradation in the data
	assert.True(suite.T(), foundDegradationInsight, "Should detect performance degradation")
}

// TestVolatilityCalculation tests volatility calculations
func (suite *TrendTestSuite) TestVolatilityCalculation() {
	analyzer := suite.analyzer.(*trendAnalyzer)

	// Test with stable data (low volatility)
	stableData := []TrendDataPoint{
		{Value: 100.0}, {Value: 101.0}, {Value: 99.0}, {Value: 100.5}, {Value: 99.5},
	}
	stableVolatility := analyzer.calculateVolatility(stableData)

	// Test with volatile data (high volatility)
	volatileData := []TrendDataPoint{
		{Value: 100.0}, {Value: 150.0}, {Value: 50.0}, {Value: 200.0}, {Value: 25.0},
	}
	volatileVolatility := analyzer.calculateVolatility(volatileData)

	// Volatile data should have higher volatility
	assert.True(suite.T(), volatileVolatility > stableVolatility)
	assert.True(suite.T(), stableVolatility >= 0.0)
	assert.True(suite.T(), volatileVolatility >= 0.0)
}

// TestChangePointDetection tests change point detection
func (suite *TrendTestSuite) TestChangePointDetection() {
	analyzer := suite.analyzer.(*trendAnalyzer)

	// Create data with a clear change point
	dataPoints := make([]TrendDataPoint, 30)
	for i := 0; i < 30; i++ {
		if i < 15 {
			// First half: stable around 100
			dataPoints[i] = TrendDataPoint{
				Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
				Value:     100.0 + float64(i%3), // Small fluctuation
				RunID:     fmt.Sprintf("run-%d", i),
			}
		} else {
			// Second half: stable around 200 (level shift)
			dataPoints[i] = TrendDataPoint{
				Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
				Value:     200.0 + float64(i%3), // Small fluctuation around new level
				RunID:     fmt.Sprintf("run-%d", i),
			}
		}
	}

	changePoints := analyzer.detectChangePoints(dataPoints)

	// Should detect a change point around position 15
	assert.NotEmpty(suite.T(), changePoints)

	found := false
	for _, cp := range changePoints {
		if cp.Type == "level" && math.Abs(cp.Magnitude) > 50 {
			found = true
			assert.True(suite.T(), cp.Confidence > 0.5)
			assert.NotEmpty(suite.T(), cp.Description)
		}
	}
	assert.True(suite.T(), found, "Should detect significant level change")
}

// TestConcurrentAnalysis tests concurrent trend analysis operations
func (suite *TrendTestSuite) TestConcurrentAnalysis() {
	// Create test data
	runs := suite.createTrendingRuns("concurrent-test", 20, func(i int) *types.HistoricRun {
		return suite.createBasicHistoricRun(i, "concurrent-test")
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "concurrent-test", mock.AnythingOfType("int")).Return(runs, nil).Maybe()

	// Test concurrent analysis operations
	done := make(chan bool, 5)
	errors := make(chan error, 5)

	operations := []func(){
		func() {
			_, err := suite.analyzer.CalculateTrends(suite.ctx, "concurrent-test", 20)
			if err != nil {
				errors <- err
			}
		},
		func() {
			_, err := suite.analyzer.DetectTrendDirection(suite.ctx, "concurrent-test", "avg_latency", 20)
			if err != nil {
				errors <- err
			}
		},
		func() {
			_, err := suite.analyzer.CalculateMovingAverage(suite.ctx, "concurrent-test", "avg_latency", 5, 20)
			if err != nil {
				errors <- err
			}
		},
		func() {
			_, err := suite.analyzer.DetectAnomalies(suite.ctx, "concurrent-test", 20)
			if err != nil {
				errors <- err
			}
		},
		func() {
			_, err := suite.analyzer.CalculateStatistics(suite.ctx, "concurrent-test", 20)
			if err != nil {
				errors <- err
			}
		},
	}

	// Run operations concurrently
	for _, op := range operations {
		go func(operation func()) {
			defer func() { done <- true }()
			operation()
		}(op)
	}

	// Wait for all operations to complete
	for i := 0; i < len(operations); i++ {
		<-done
	}

	close(errors)
	for err := range errors {
		suite.T().Errorf("Concurrent operation failed: %v", err)
	}
}

// TestEdgeCases tests various edge cases
func (suite *TrendTestSuite) TestEdgeCases() {
	// Test with insufficient data points
	shortRuns := suite.createTrendingRuns("short-test", 3, func(i int) *types.HistoricRun {
		return suite.createBasicHistoricRun(i, "short-test")
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "short-test", mock.AnythingOfType("int")).Return(shortRuns, nil)

	_, err := suite.analyzer.CalculateTrends(suite.ctx, "short-test", 10)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "insufficient data points")

	// Test with empty data
	emptyRuns := []*types.HistoricRun{}
	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "empty-test", mock.AnythingOfType("int")).Return(emptyRuns, nil)

	_, err = suite.analyzer.CalculateTrends(suite.ctx, "empty-test", 10)
	assert.Error(suite.T(), err)

	// Test with single data point
	singleRun := suite.createTrendingRuns("single-test", 1, func(i int) *types.HistoricRun {
		return suite.createBasicHistoricRun(i, "single-test")
	})

	suite.mockStorage.On("ListHistoricRuns", suite.ctx, "single-test", mock.AnythingOfType("int")).Return(singleRun, nil)

	_, err = suite.analyzer.CalculateTrends(suite.ctx, "single-test", 10)
	assert.Error(suite.T(), err)
}

// Helper functions for creating test data

func (suite *TrendTestSuite) createTrendingRuns(testName string, count int, modifier func(int) *types.HistoricRun) []*types.HistoricRun {
	runs := make([]*types.HistoricRun, count)
	for i := 0; i < count; i++ {
		runs[i] = modifier(i)
	}
	return runs
}

func (suite *TrendTestSuite) createBasicHistoricRun(index int, testName string) *types.HistoricRun {
	return &types.HistoricRun{
		ID:               fmt.Sprintf("run-%d", index),
		TestName:         testName,
		Timestamp:        time.Now().Add(time.Duration(-index) * time.Hour), // Reverse chronological
		AvgLatencyMs:     150.0,
		P95LatencyMs:     300.0,
		P99LatencyMs:     500.0,
		OverallErrorRate: 0.02,
		TotalRequests:    1000,
		TotalErrors:      20,
		GitCommit:        fmt.Sprintf("commit-%d", index),
		Duration:         "10m",
	}
}

// Benchmark tests for performance validation

func BenchmarkTrendCalculation(b *testing.B) {
	mockStorage := new(MockHistoricStorage)
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	analyzer := NewTrendAnalyzer(mockStorage, db, logger)
	analyzer.Start(context.Background())

	// Create large dataset for benchmarking
	runs := make([]*types.HistoricRun, 1000)
	for i := 0; i < 1000; i++ {
		runs[i] = &types.HistoricRun{
			ID:               fmt.Sprintf("bench-run-%d", i),
			TestName:         "benchmark-test",
			Timestamp:        time.Now().Add(time.Duration(-i) * time.Hour),
			AvgLatencyMs:     150.0 + float64(i)*0.1,
			P95LatencyMs:     300.0 + float64(i)*0.2,
			OverallErrorRate: 0.02 + float64(i)*0.0001,
			TotalRequests:    1000,
		}
	}

	mockStorage.On("ListHistoricRuns", mock.Anything, "benchmark-test", mock.AnythingOfType("int")).Return(runs, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := analyzer.CalculateTrends(context.Background(), "benchmark-test", 30)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLinearRegression(b *testing.B) {
	// Create test data
	dataPoints := make([]TrendDataPoint, 100)
	for i := 0; i < 100; i++ {
		dataPoints[i] = TrendDataPoint{
			Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
			Value:     float64(i)*2.5 + 10.0,
			RunID:     fmt.Sprintf("run-%d", i),
		}
	}

	analyzer := &trendAnalyzer{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = analyzer.calculateLinearRegression(dataPoints)
	}
}

func BenchmarkMovingAverage(b *testing.B) {
	mockStorage := new(MockHistoricStorage)
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	analyzer := NewTrendAnalyzer(mockStorage, db, logger)
	analyzer.Start(context.Background())

	runs := make([]*types.HistoricRun, 200)
	for i := 0; i < 200; i++ {
		runs[i] = &types.HistoricRun{
			ID:           fmt.Sprintf("ma-run-%d", i),
			TestName:     "ma-benchmark",
			Timestamp:    time.Now().Add(time.Duration(-i) * time.Hour),
			AvgLatencyMs: 150.0 + float64(i)*0.5,
		}
	}

	mockStorage.On("ListHistoricRuns", mock.Anything, "ma-benchmark", mock.AnythingOfType("int")).Return(runs, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := analyzer.CalculateMovingAverage(context.Background(), "ma-benchmark", "avg_latency", 10, 100)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Run the test suite
func TestTrendTestSuite(t *testing.T) {
	suite.Run(t, new(TrendTestSuite))
}

// Test mathematical accuracy of statistical functions
func TestStatisticalAccuracy(t *testing.T) {
	// Test mean calculation
	values := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	analyzer := &trendAnalyzer{}

	mean := analyzer.calculateMean(values)
	assert.InDelta(t, 3.0, mean, 0.001)

	// Test median calculation
	median := analyzer.calculateMedian(values)
	assert.InDelta(t, 3.0, median, 0.001)

	// Test standard deviation
	stdDev := analyzer.calculateStandardDeviation(values)
	expectedStdDev := math.Sqrt(2.5) // Known value for this dataset
	assert.InDelta(t, expectedStdDev, stdDev, 0.001)

	// Test percentile calculation
	p50 := analyzer.calculatePercentile(values, 50)
	assert.InDelta(t, 3.0, p50, 0.001)

	p25 := analyzer.calculatePercentile(values, 25)
	assert.InDelta(t, 2.0, p25, 0.001)

	p75 := analyzer.calculatePercentile(values, 75)
	assert.InDelta(t, 4.0, p75, 0.001)
}

func TestAutocorrelation(t *testing.T) {
	// Test autocorrelation with known pattern
	analyzer := &trendAnalyzer{}

	// Create a simple pattern
	values := []float64{1, 2, 3, 4, 5, 1, 2, 3, 4, 5, 1, 2, 3, 4, 5}

	autocorr := analyzer.calculateAutocorrelation(values, 5)

	// First value should be 1.0 (perfect correlation with itself)
	assert.InDelta(t, 1.0, autocorr[0], 0.001)

	// Values should be between -1 and 1
	for _, val := range autocorr {
		assert.True(t, val >= -1.0 && val <= 1.0)
	}
}

func TestTrendStrengthClassification(t *testing.T) {
	analyzer := &trendAnalyzer{}

	// Test trend strength determination
	testCases := []struct {
		rSquared   float64
		volatility float64
		expected   string
	}{
		{0.9, 0.1, "very_strong"},
		{0.7, 0.2, "strong"},
		{0.5, 0.3, "moderate"},
		{0.3, 0.5, "weak"},
		{0.1, 0.8, "weak"},
	}

	for _, tc := range testCases {
		strength := analyzer.determineTrendStrength(tc.rSquared, tc.volatility)
		assert.Equal(t, tc.expected, strength)
	}
}

func TestTrendDirectionLogic(t *testing.T) {
	analyzer := &trendAnalyzer{}

	// Test direction determination for different metrics
	testCases := []struct {
		slope    float64
		metric   string
		expected string
	}{
		{-5.0, "avg_latency", "improving"}, // Decreasing latency is improving
		{5.0, "avg_latency", "degrading"},  // Increasing latency is degrading
		{-0.02, "error_rate", "improving"}, // Decreasing error rate is improving
		{0.02, "error_rate", "degrading"},  // Increasing error rate is degrading
		{5.0, "throughput", "improving"},   // Increasing throughput is improving
		{-5.0, "throughput", "degrading"},  // Decreasing throughput is degrading
		{0.005, "avg_latency", "stable"},   // Small slope is stable
	}

	for _, tc := range testCases {
		direction := analyzer.determineTrendDirection(tc.slope, tc.metric)
		assert.Equal(t, tc.expected, direction)
	}
}
