package analysis

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// TrendAnalyzer provides comprehensive trend analysis capabilities for performance data
type TrendAnalyzer interface {
	Start(ctx context.Context) error
	Stop() error

	// Core trend analysis
	CalculateTrends(ctx context.Context, testName string, days int) (*TrendAnalysisResult, error)
	GetMethodTrends(ctx context.Context, testName, method string, days int) (*MethodTrendAnalysis, error)
	GetClientTrends(ctx context.Context, testName, client string, days int) (*ClientTrendAnalysis, error)

	// Moving average calculations
	CalculateMovingAverage(ctx context.Context, testName, metric string, windowSize, days int) (*MovingAverageResult, error)

	// Trend direction detection
	DetectTrendDirection(ctx context.Context, testName, metric string, days int) (*TrendDirection, error)

	// Forecasting capabilities
	ForecastTrend(ctx context.Context, testName, metric string, days, forecastDays int) (*TrendForecast, error)

	// Anomaly detection
	DetectAnomalies(ctx context.Context, testName string, days int) (*AnomalyDetectionResult, error)

	// Statistical analysis
	CalculateStatistics(ctx context.Context, testName string, days int) (*TrendStatistics, error)
}

// TrendData represents analyzed trend data with statistical properties
type TrendData struct {
	TestName     string               `json:"test_name"`
	Metric       string               `json:"metric"`
	Client       string               `json:"client,omitempty"`
	Method       string               `json:"method,omitempty"`
	DataPoints   []TrendDataPoint     `json:"data_points"`
	Statistics   TrendStatistics      `json:"statistics"`
	Direction    TrendDirection       `json:"direction"`
	MovingAvg    []MovingAveragePoint `json:"moving_average"`
	Forecast     *TrendForecast       `json:"forecast,omitempty"`
	Anomalies    []AnomalyPoint       `json:"anomalies"`
	Seasonality  *SeasonalityAnalysis `json:"seasonality,omitempty"`
	ChangePoints []ChangePoint        `json:"change_points"`
}

// TrendDataPoint represents a single data point in a trend analysis
type TrendDataPoint struct {
	Timestamp  time.Time              `json:"timestamp"`
	Value      float64                `json:"value"`
	RunID      string                 `json:"run_id"`
	GitCommit  string                 `json:"git_commit,omitempty"`
	IsAnomaly  bool                   `json:"is_anomaly"`
	Confidence float64                `json:"confidence"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// TrendStatistics contains statistical properties of trend data
type TrendStatistics struct {
	Count            int                `json:"count"`
	Mean             float64            `json:"mean"`
	Median           float64            `json:"median"`
	StandardDev      float64            `json:"standard_deviation"`
	Variance         float64            `json:"variance"`
	Min              float64            `json:"min"`
	Max              float64            `json:"max"`
	Percentiles      map[string]float64 `json:"percentiles"`
	LinearRegression LinearRegression   `json:"linear_regression"`
	Correlation      CorrelationMatrix  `json:"correlation"`
	Stationarity     StationarityTest   `json:"stationarity"`
	Autocorrelation  []float64          `json:"autocorrelation"`
}

// LinearRegression contains linear regression analysis results
type LinearRegression struct {
	Slope       float64 `json:"slope"`
	Intercept   float64 `json:"intercept"`
	RSquared    float64 `json:"r_squared"`
	PValue      float64 `json:"p_value"`
	Significant bool    `json:"significant"`
	Equation    string  `json:"equation"`
}

// CorrelationMatrix contains correlation analysis between different metrics
type CorrelationMatrix struct {
	Metrics      []string    `json:"metrics"`
	Matrix       [][]float64 `json:"matrix"`
	Significance [][]float64 `json:"significance"`
}

// StationarityTest contains results of stationarity testing
type StationarityTest struct {
	IsStationary bool    `json:"is_stationary"`
	PValue       float64 `json:"p_value"`
	TestType     string  `json:"test_type"`
	Critical     float64 `json:"critical_value"`
}

// TrendDirection represents the overall direction and strength of a trend
type TrendDirection struct {
	Direction    string  `json:"direction"`    // "improving", "degrading", "stable", "volatile"
	Strength     string  `json:"strength"`     // "weak", "moderate", "strong", "very_strong"
	Confidence   float64 `json:"confidence"`   // 0-1 confidence level
	Slope        float64 `json:"slope"`        // Rate of change
	Volatility   float64 `json:"volatility"`   // Measure of variability
	TrendScore   float64 `json:"trend_score"`  // Composite trend score
	Significance float64 `json:"significance"` // Statistical significance
}

// MovingAverageResult contains moving average calculations
type MovingAverageResult struct {
	WindowSize   int                  `json:"window_size"`
	Points       []MovingAveragePoint `json:"points"`
	Smoothness   float64              `json:"smoothness"`
	TrendClarity float64              `json:"trend_clarity"`
}

// MovingAveragePoint represents a single moving average calculation
type MovingAveragePoint struct {
	Timestamp     time.Time `json:"timestamp"`
	Value         float64   `json:"value"`
	MovingAverage float64   `json:"moving_average"`
	Deviation     float64   `json:"deviation"`
	UpperBound    float64   `json:"upper_bound"`
	LowerBound    float64   `json:"lower_bound"`
}

// TrendForecast contains forecasting results
type TrendForecast struct {
	Method      string             `json:"method"`
	Accuracy    float64            `json:"accuracy"`
	Confidence  float64            `json:"confidence"`
	Predictions []ForecastPoint    `json:"predictions"`
	Model       ForecastModel      `json:"model"`
	Validation  ForecastValidation `json:"validation"`
}

// ForecastPoint represents a single forecast prediction
type ForecastPoint struct {
	Timestamp      time.Time `json:"timestamp"`
	PredictedValue float64   `json:"predicted_value"`
	LowerBound     float64   `json:"lower_bound"`
	UpperBound     float64   `json:"upper_bound"`
	Confidence     float64   `json:"confidence"`
}

// ForecastModel contains model parameters
type ForecastModel struct {
	Type       string                 `json:"type"`
	Parameters map[string]interface{} `json:"parameters"`
	Equation   string                 `json:"equation,omitempty"`
}

// ForecastValidation contains validation metrics
type ForecastValidation struct {
	MAE  float64 `json:"mae"`  // Mean Absolute Error
	RMSE float64 `json:"rmse"` // Root Mean Square Error
	MAPE float64 `json:"mape"` // Mean Absolute Percentage Error
}

// AnomalyDetectionResult contains anomaly detection results
type AnomalyDetectionResult struct {
	Method      string            `json:"method"`
	Sensitivity float64           `json:"sensitivity"`
	Anomalies   []AnomalyPoint    `json:"anomalies"`
	AnomalyRate float64           `json:"anomaly_rate"`
	Thresholds  AnomalyThresholds `json:"thresholds"`
}

// AnomalyPoint represents a detected anomaly
type AnomalyPoint struct {
	Timestamp      time.Time              `json:"timestamp"`
	Value          float64                `json:"value"`
	ExpectedValue  float64                `json:"expected_value"`
	DeviationScore float64                `json:"deviation_score"`
	Severity       string                 `json:"severity"`
	RunID          string                 `json:"run_id"`
	Context        map[string]interface{} `json:"context,omitempty"`
}

// AnomalyThresholds contains thresholds for anomaly detection
type AnomalyThresholds struct {
	Mild     float64 `json:"mild"`
	Moderate float64 `json:"moderate"`
	Severe   float64 `json:"severe"`
}

// SeasonalityAnalysis contains seasonal pattern analysis
type SeasonalityAnalysis struct {
	HasSeasonality   bool              `json:"has_seasonality"`
	SeasonalStrength float64           `json:"seasonal_strength"`
	Period           int               `json:"period"`
	Patterns         []SeasonalPattern `json:"patterns"`
}

// SeasonalPattern represents a detected seasonal pattern
type SeasonalPattern struct {
	Period    int     `json:"period"`
	Strength  float64 `json:"strength"`
	Amplitude float64 `json:"amplitude"`
	Phase     float64 `json:"phase"`
}

// ChangePoint represents a significant change in trend
type ChangePoint struct {
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"` // "level", "trend", "variance"
	Magnitude   float64   `json:"magnitude"`
	Confidence  float64   `json:"confidence"`
	Description string    `json:"description"`
}

// TrendAnalysisResult contains comprehensive trend analysis
type TrendAnalysisResult struct {
	TestName        string               `json:"test_name"`
	AnalyzedAt      time.Time            `json:"analyzed_at"`
	Period          int                  `json:"period_days"`
	Metrics         []string             `json:"metrics"`
	Trends          map[string]TrendData `json:"trends"`
	Summary         TrendSummary         `json:"summary"`
	Insights        []TrendInsight       `json:"insights"`
	Recommendations []string             `json:"recommendations"`
}

// TrendSummary provides high-level trend summary
type TrendSummary struct {
	OverallDirection    string  `json:"overall_direction"`
	OverallHealth       string  `json:"overall_health"`
	Volatility          string  `json:"volatility"`
	Reliability         float64 `json:"reliability"`
	PredictabilityScore float64 `json:"predictability_score"`
}

// TrendInsight represents an automatically generated insight
type TrendInsight struct {
	Type        string                 `json:"type"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Severity    string                 `json:"severity"`
	Confidence  float64                `json:"confidence"`
	Timestamp   time.Time              `json:"timestamp"`
	Data        map[string]interface{} `json:"data,omitempty"`
}

// MethodTrendAnalysis contains method-specific trend analysis
type MethodTrendAnalysis struct {
	TestName   string                      `json:"test_name"`
	Method     string                      `json:"method"`
	Trends     map[string]TrendData        `json:"trends"`
	Comparison map[string]MethodComparison `json:"comparison"`
	Ranking    []MethodRanking             `json:"ranking"`
}

// MethodComparison compares method performance over time
type MethodComparison struct {
	Method      string             `json:"method"`
	Trend       string             `json:"trend"`
	Performance string             `json:"performance"`
	Metrics     map[string]float64 `json:"metrics"`
}

// MethodRanking ranks methods by performance
type MethodRanking struct {
	Method string  `json:"method"`
	Score  float64 `json:"score"`
	Rank   int     `json:"rank"`
}

// ClientTrendAnalysis contains client-specific trend analysis
type ClientTrendAnalysis struct {
	TestName   string                           `json:"test_name"`
	Client     string                           `json:"client"`
	Trends     map[string]TrendData             `json:"trends"`
	Comparison map[string]ClientTrendComparison `json:"comparison"`
	Ranking    []ClientRanking                  `json:"ranking"`
}

// ClientTrendComparison compares client performance over time
type ClientTrendComparison struct {
	Client      string             `json:"client"`
	Trend       string             `json:"trend"`
	Performance string             `json:"performance"`
	Metrics     map[string]float64 `json:"metrics"`
}

// ClientRanking ranks clients by performance
type ClientRanking struct {
	Client string  `json:"client"`
	Score  float64 `json:"score"`
	Rank   int     `json:"rank"`
}

// trendAnalyzer implements TrendAnalyzer interface
type trendAnalyzer struct {
	storage storage.HistoricStorage
	db      *sql.DB
	log     logrus.FieldLogger
	cache   sync.Map
	config  TrendAnalyzerConfig
}

// TrendAnalyzerConfig contains configuration for trend analysis
type TrendAnalyzerConfig struct {
	CacheEnabled       bool          `json:"cache_enabled"`
	CacheTTL           time.Duration `json:"cache_ttl"`
	MinDataPoints      int           `json:"min_data_points"`
	AnomalySensitivity float64       `json:"anomaly_sensitivity"`
	MovingAvgWindow    int           `json:"moving_avg_window"`
	SeasonalityWindow  int           `json:"seasonality_window"`
}

// NewTrendAnalyzer creates a new trend analyzer
func NewTrendAnalyzer(historicStorage storage.HistoricStorage, db *sql.DB, log logrus.FieldLogger) TrendAnalyzer {
	return &trendAnalyzer{
		storage: historicStorage,
		db:      db,
		log:     log.WithField("component", "trend-analyzer"),
		config: TrendAnalyzerConfig{
			CacheEnabled:       true,
			CacheTTL:           15 * time.Minute,
			MinDataPoints:      10,
			AnomalySensitivity: 2.0,
			MovingAvgWindow:    7,
			SeasonalityWindow:  30,
		},
	}
}

// Start initializes the trend analyzer
func (ta *trendAnalyzer) Start(ctx context.Context) error {
	ta.log.Info("Starting trend analyzer")

	// Initialize any required database tables or indexes
	if err := ta.initializeAnalysisTables(ctx); err != nil {
		return fmt.Errorf("failed to initialize analysis tables: %w", err)
	}

	ta.log.Info("Trend analyzer started successfully")
	return nil
}

// Stop shuts down the trend analyzer
func (ta *trendAnalyzer) Stop() error {
	ta.log.Info("Stopping trend analyzer")

	// Clear cache
	ta.cache.Range(func(key, value interface{}) bool {
		ta.cache.Delete(key)
		return true
	})

	return nil
}

// CalculateTrends performs comprehensive trend analysis for a test
func (ta *trendAnalyzer) CalculateTrends(ctx context.Context, testName string, days int) (*TrendAnalysisResult, error) {
	ta.log.WithFields(logrus.Fields{
		"test_name": testName,
		"days":      days,
	}).Info("Calculating comprehensive trends")

	// Check cache first
	cacheKey := fmt.Sprintf("trends_%s_%d", testName, days)
	if ta.config.CacheEnabled {
		if cached, ok := ta.cache.Load(cacheKey); ok {
			if result, ok := cached.(*TrendAnalysisResult); ok {
				if time.Since(result.AnalyzedAt) < ta.config.CacheTTL {
					ta.log.Debug("Returning cached trend analysis")
					return result, nil
				}
			}
		}
	}

	// Get historic data
	historicData, err := ta.getHistoricData(ctx, testName, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic data: %w", err)
	}

	if len(historicData) < ta.config.MinDataPoints {
		return nil, fmt.Errorf("insufficient data points: need at least %d, got %d",
			ta.config.MinDataPoints, len(historicData))
	}

	// Analyze trends for each metric
	metrics := []string{"avg_latency", "p95_latency", "p99_latency", "error_rate", "throughput"}
	trends := make(map[string]TrendData)

	for _, metric := range metrics {
		trendData, err := ta.analyzeTrendForMetric(ctx, historicData, metric)
		if err != nil {
			ta.log.WithError(err).WithField("metric", metric).Warn("Failed to analyze trend for metric")
			continue
		}
		trends[metric] = *trendData
	}

	// Generate insights and recommendations
	insights := ta.generateInsights(trends)
	recommendations := ta.generateRecommendations(trends, insights)

	// Create result
	result := &TrendAnalysisResult{
		TestName:        testName,
		AnalyzedAt:      time.Now(),
		Period:          days,
		Metrics:         metrics,
		Trends:          trends,
		Summary:         ta.generateTrendSummary(trends),
		Insights:        insights,
		Recommendations: recommendations,
	}

	// Cache result
	if ta.config.CacheEnabled {
		ta.cache.Store(cacheKey, result)
	}

	ta.log.WithFields(logrus.Fields{
		"test_name": testName,
		"metrics":   len(trends),
		"insights":  len(insights),
	}).Info("Trend analysis completed")

	return result, nil
}

// GetMethodTrends analyzes trends for a specific method
func (ta *trendAnalyzer) GetMethodTrends(ctx context.Context, testName, method string, days int) (*MethodTrendAnalysis, error) {
	ta.log.WithFields(logrus.Fields{
		"test_name": testName,
		"method":    method,
		"days":      days,
	}).Info("Analyzing method trends")

	// Get method-specific data
	methodData, err := ta.getMethodData(ctx, testName, method, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get method data: %w", err)
	}

	// Analyze trends for each metric
	metrics := []string{"latency", "error_rate", "throughput"}
	trends := make(map[string]TrendData)

	for _, metric := range metrics {
		trendData, err := ta.analyzeMethodTrendForMetric(ctx, methodData, metric, method)
		if err != nil {
			ta.log.WithError(err).WithFields(logrus.Fields{
				"method": method,
				"metric": metric,
			}).Warn("Failed to analyze method trend")
			continue
		}
		trends[metric] = *trendData
	}

	// Get comparison data
	comparison, err := ta.generateMethodComparison(ctx, testName, method, days)
	if err != nil {
		ta.log.WithError(err).Warn("Failed to generate method comparison")
		comparison = make(map[string]MethodComparison)
	}

	// Generate ranking
	ranking, err := ta.generateMethodRanking(ctx, testName, days)
	if err != nil {
		ta.log.WithError(err).Warn("Failed to generate method ranking")
		ranking = []MethodRanking{}
	}

	result := &MethodTrendAnalysis{
		TestName:   testName,
		Method:     method,
		Trends:     trends,
		Comparison: comparison,
		Ranking:    ranking,
	}

	ta.log.WithField("method", method).Info("Method trend analysis completed")
	return result, nil
}

// GetClientTrends analyzes trends for a specific client
func (ta *trendAnalyzer) GetClientTrends(ctx context.Context, testName, client string, days int) (*ClientTrendAnalysis, error) {
	ta.log.WithFields(logrus.Fields{
		"test_name": testName,
		"client":    client,
		"days":      days,
	}).Info("Analyzing client trends")

	// Get client-specific data
	clientData, err := ta.getClientData(ctx, testName, client, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get client data: %w", err)
	}

	// Analyze trends for each metric
	metrics := []string{"avg_latency", "p95_latency", "error_rate", "throughput"}
	trends := make(map[string]TrendData)

	for _, metric := range metrics {
		trendData, err := ta.analyzeClientTrendForMetric(ctx, clientData, metric, client)
		if err != nil {
			ta.log.WithError(err).WithFields(logrus.Fields{
				"client": client,
				"metric": metric,
			}).Warn("Failed to analyze client trend")
			continue
		}
		trends[metric] = *trendData
	}

	// Get comparison data
	comparison, err := ta.generateClientComparison(ctx, testName, client, days)
	if err != nil {
		ta.log.WithError(err).Warn("Failed to generate client comparison")
		comparison = make(map[string]ClientTrendComparison)
	}

	// Generate ranking
	ranking, err := ta.generateClientRanking(ctx, testName, days)
	if err != nil {
		ta.log.WithError(err).Warn("Failed to generate client ranking")
		ranking = []ClientRanking{}
	}

	result := &ClientTrendAnalysis{
		TestName:   testName,
		Client:     client,
		Trends:     trends,
		Comparison: comparison,
		Ranking:    ranking,
	}

	ta.log.WithField("client", client).Info("Client trend analysis completed")
	return result, nil
}

// CalculateMovingAverage calculates moving averages for a metric
func (ta *trendAnalyzer) CalculateMovingAverage(ctx context.Context, testName, metric string, windowSize, days int) (*MovingAverageResult, error) {
	ta.log.WithFields(logrus.Fields{
		"test_name":   testName,
		"metric":      metric,
		"window_size": windowSize,
		"days":        days,
	}).Info("Calculating moving average")

	// Get historic data
	historicData, err := ta.getHistoricData(ctx, testName, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic data: %w", err)
	}

	// Extract metric values
	dataPoints := ta.extractMetricValues(historicData, metric)
	if len(dataPoints) < windowSize {
		return nil, fmt.Errorf("insufficient data points for moving average: need at least %d, got %d",
			windowSize, len(dataPoints))
	}

	// Calculate moving averages
	points := make([]MovingAveragePoint, 0, len(dataPoints))
	values := make([]float64, 0, len(dataPoints))

	for i, point := range dataPoints {
		values = append(values, point.Value)

		if i >= windowSize-1 {
			// Calculate moving average
			sum := 0.0
			for j := i - windowSize + 1; j <= i; j++ {
				sum += dataPoints[j].Value
			}
			movingAvg := sum / float64(windowSize)

			// Calculate standard deviation for bounds
			variance := 0.0
			for j := i - windowSize + 1; j <= i; j++ {
				variance += math.Pow(dataPoints[j].Value-movingAvg, 2)
			}
			stdDev := math.Sqrt(variance / float64(windowSize))

			deviation := point.Value - movingAvg

			maPoint := MovingAveragePoint{
				Timestamp:     point.Timestamp,
				Value:         point.Value,
				MovingAverage: movingAvg,
				Deviation:     deviation,
				UpperBound:    movingAvg + 2*stdDev,
				LowerBound:    movingAvg - 2*stdDev,
			}
			points = append(points, maPoint)
		}
	}

	// Calculate smoothness and trend clarity
	smoothness := ta.calculateSmoothness(points)
	trendClarity := ta.calculateTrendClarity(points)

	result := &MovingAverageResult{
		WindowSize:   windowSize,
		Points:       points,
		Smoothness:   smoothness,
		TrendClarity: trendClarity,
	}

	ta.log.WithFields(logrus.Fields{
		"test_name":     testName,
		"metric":        metric,
		"points":        len(points),
		"smoothness":    smoothness,
		"trend_clarity": trendClarity,
	}).Info("Moving average calculation completed")

	return result, nil
}

// DetectTrendDirection detects the overall trend direction for a metric
func (ta *trendAnalyzer) DetectTrendDirection(ctx context.Context, testName, metric string, days int) (*TrendDirection, error) {
	ta.log.WithFields(logrus.Fields{
		"test_name": testName,
		"metric":    metric,
		"days":      days,
	}).Info("Detecting trend direction")

	// Get historic data
	historicData, err := ta.getHistoricData(ctx, testName, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic data: %w", err)
	}

	// Extract metric values
	dataPoints := ta.extractMetricValues(historicData, metric)
	if len(dataPoints) < ta.config.MinDataPoints {
		return nil, fmt.Errorf("insufficient data points for trend detection")
	}

	// Perform linear regression
	regression := ta.calculateLinearRegression(dataPoints)

	// Calculate volatility
	volatility := ta.calculateVolatility(dataPoints)

	// Determine direction
	direction := ta.determineTrendDirection(regression.Slope, metric)

	// Determine strength
	strength := ta.determineTrendStrength(regression.RSquared, volatility)

	// Calculate confidence
	confidence := ta.calculateTrendConfidence(regression, volatility, len(dataPoints))

	// Calculate trend score
	trendScore := ta.calculateTrendScore(regression, volatility)

	result := &TrendDirection{
		Direction:    direction,
		Strength:     strength,
		Confidence:   confidence,
		Slope:        regression.Slope,
		Volatility:   volatility,
		TrendScore:   trendScore,
		Significance: regression.PValue,
	}

	ta.log.WithFields(logrus.Fields{
		"test_name":  testName,
		"metric":     metric,
		"direction":  direction,
		"strength":   strength,
		"confidence": confidence,
	}).Info("Trend direction detection completed")

	return result, nil
}

// ForecastTrend generates forecasts for a metric
func (ta *trendAnalyzer) ForecastTrend(ctx context.Context, testName, metric string, days, forecastDays int) (*TrendForecast, error) {
	ta.log.WithFields(logrus.Fields{
		"test_name":     testName,
		"metric":        metric,
		"days":          days,
		"forecast_days": forecastDays,
	}).Info("Generating trend forecast")

	// Get historic data
	historicData, err := ta.getHistoricData(ctx, testName, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic data: %w", err)
	}

	// Extract metric values
	dataPoints := ta.extractMetricValues(historicData, metric)
	if len(dataPoints) < ta.config.MinDataPoints {
		return nil, fmt.Errorf("insufficient data points for forecasting")
	}

	// Use linear regression for forecasting (could be extended to more sophisticated methods)
	regression := ta.calculateLinearRegression(dataPoints)

	// Generate predictions
	predictions := make([]ForecastPoint, 0, forecastDays)
	lastTimestamp := dataPoints[len(dataPoints)-1].Timestamp

	// Calculate prediction interval
	residuals := ta.calculateResiduals(dataPoints, regression)
	residualStdDev := ta.calculateStandardDeviation(residuals)

	for i := 1; i <= forecastDays; i++ {
		futureTimestamp := lastTimestamp.AddDate(0, 0, i)
		x := float64(len(dataPoints) + i - 1)
		predictedValue := regression.Slope*x + regression.Intercept

		// Calculate prediction bounds (simple approach)
		marginOfError := 1.96 * residualStdDev * math.Sqrt(1+1/float64(len(dataPoints))+
			math.Pow(x-ta.calculateMean(ta.getXValues(dataPoints)), 2)/ta.calculateSumOfSquares(ta.getXValues(dataPoints)))

		prediction := ForecastPoint{
			Timestamp:      futureTimestamp,
			PredictedValue: predictedValue,
			LowerBound:     predictedValue - marginOfError,
			UpperBound:     predictedValue + marginOfError,
			Confidence:     regression.RSquared,
		}
		predictions = append(predictions, prediction)
	}

	// Calculate validation metrics
	validation := ta.calculateForecastValidation(dataPoints, regression)

	result := &TrendForecast{
		Method:      "linear_regression",
		Accuracy:    regression.RSquared,
		Confidence:  regression.RSquared,
		Predictions: predictions,
		Model: ForecastModel{
			Type: "linear",
			Parameters: map[string]interface{}{
				"slope":     regression.Slope,
				"intercept": regression.Intercept,
				"r_squared": regression.RSquared,
			},
			Equation: regression.Equation,
		},
		Validation: validation,
	}

	ta.log.WithFields(logrus.Fields{
		"test_name":   testName,
		"metric":      metric,
		"predictions": len(predictions),
		"accuracy":    result.Accuracy,
	}).Info("Trend forecast completed")

	return result, nil
}

// DetectAnomalies detects anomalous data points
func (ta *trendAnalyzer) DetectAnomalies(ctx context.Context, testName string, days int) (*AnomalyDetectionResult, error) {
	ta.log.WithFields(logrus.Fields{
		"test_name": testName,
		"days":      days,
	}).Info("Detecting anomalies")

	// Get historic data
	historicData, err := ta.getHistoricData(ctx, testName, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic data: %w", err)
	}

	var allAnomalies []AnomalyPoint
	metrics := []string{"avg_latency", "p95_latency", "error_rate"}

	for _, metric := range metrics {
		dataPoints := ta.extractMetricValues(historicData, metric)
		if len(dataPoints) < ta.config.MinDataPoints {
			continue
		}

		anomalies := ta.detectAnomaliesForMetric(dataPoints, metric)
		allAnomalies = append(allAnomalies, anomalies...)
	}

	// Calculate anomaly rate
	totalPoints := len(historicData) * len(metrics)
	anomalyRate := float64(len(allAnomalies)) / float64(totalPoints)

	// Define thresholds
	thresholds := AnomalyThresholds{
		Mild:     ta.config.AnomalySensitivity,
		Moderate: ta.config.AnomalySensitivity * 1.5,
		Severe:   ta.config.AnomalySensitivity * 2.0,
	}

	result := &AnomalyDetectionResult{
		Method:      "statistical_outlier",
		Sensitivity: ta.config.AnomalySensitivity,
		Anomalies:   allAnomalies,
		AnomalyRate: anomalyRate,
		Thresholds:  thresholds,
	}

	ta.log.WithFields(logrus.Fields{
		"test_name":    testName,
		"anomalies":    len(allAnomalies),
		"anomaly_rate": anomalyRate,
	}).Info("Anomaly detection completed")

	return result, nil
}

// CalculateStatistics calculates comprehensive statistics for trend data
func (ta *trendAnalyzer) CalculateStatistics(ctx context.Context, testName string, days int) (*TrendStatistics, error) {
	ta.log.WithFields(logrus.Fields{
		"test_name": testName,
		"days":      days,
	}).Info("Calculating trend statistics")

	// Get historic data
	historicData, err := ta.getHistoricData(ctx, testName, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic data: %w", err)
	}

	// For this example, we'll calculate statistics for avg_latency
	dataPoints := ta.extractMetricValues(historicData, "avg_latency")
	if len(dataPoints) < ta.config.MinDataPoints {
		return nil, fmt.Errorf("insufficient data points for statistics")
	}

	values := make([]float64, len(dataPoints))
	for i, point := range dataPoints {
		values[i] = point.Value
	}

	// Calculate basic statistics
	mean := ta.calculateMean(values)
	median := ta.calculateMedian(values)
	stdDev := ta.calculateStandardDeviation(values)
	variance := stdDev * stdDev
	min := ta.calculateMin(values)
	max := ta.calculateMax(values)

	// Calculate percentiles
	percentiles := map[string]float64{
		"p10": ta.calculatePercentile(values, 10),
		"p25": ta.calculatePercentile(values, 25),
		"p50": ta.calculatePercentile(values, 50),
		"p75": ta.calculatePercentile(values, 75),
		"p90": ta.calculatePercentile(values, 90),
		"p95": ta.calculatePercentile(values, 95),
		"p99": ta.calculatePercentile(values, 99),
	}

	// Calculate linear regression
	regression := ta.calculateLinearRegression(dataPoints)

	// Calculate autocorrelation
	autocorr := ta.calculateAutocorrelation(values, 10)

	result := &TrendStatistics{
		Count:            len(values),
		Mean:             mean,
		Median:           median,
		StandardDev:      stdDev,
		Variance:         variance,
		Min:              min,
		Max:              max,
		Percentiles:      percentiles,
		LinearRegression: regression,
		Autocorrelation:  autocorr,
	}

	ta.log.WithFields(logrus.Fields{
		"test_name": testName,
		"count":     result.Count,
		"mean":      result.Mean,
		"std_dev":   result.StandardDev,
	}).Info("Statistics calculation completed")

	return result, nil
}

// Helper methods for trend analysis implementation

func (ta *trendAnalyzer) initializeAnalysisTables(ctx context.Context) error {
	// Create any additional tables needed for trend analysis
	queries := []string{
		`CREATE INDEX IF NOT EXISTS idx_historic_runs_test_timestamp 
		 ON historic_runs(test_name, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_historic_runs_timestamp 
		 ON historic_runs(timestamp)`,
	}

	for _, query := range queries {
		if _, err := ta.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
	}

	return nil
}

func (ta *trendAnalyzer) getHistoricData(ctx context.Context, testName string, days int) ([]*types.HistoricRun, error) {
	filter := types.RunFilter{
		TestName: testName,
		Limit:    days * 10, // Get more than needed
	}
	runs, err := ta.storage.ListHistoricRuns(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Filter by date
	cutoff := time.Now().AddDate(0, 0, -days)
	var filtered []*types.HistoricRun
	for _, run := range runs {
		if run.Timestamp.After(cutoff) {
			filtered = append(filtered, run)
		}
	}

	// Sort by timestamp
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.Before(filtered[j].Timestamp)
	})

	return filtered, nil
}

func (ta *trendAnalyzer) extractMetricValues(runs []*types.HistoricRun, metric string) []TrendDataPoint {
	points := make([]TrendDataPoint, 0, len(runs))

	for _, run := range runs {
		var value float64
		switch metric {
		case "avg_latency":
			value = run.AvgLatencyMs
		case "p95_latency":
			value = run.P95LatencyMs
		case "p99_latency":
			value = run.P99LatencyMs
		case "error_rate":
			value = run.OverallErrorRate
		case "throughput":
			if run.Duration != "" {
				duration, err := time.ParseDuration(run.Duration)
				if err == nil && duration.Seconds() > 0 {
					value = float64(run.TotalRequests) / duration.Seconds()
				}
			}
		default:
			continue
		}

		point := TrendDataPoint{
			Timestamp:  run.Timestamp,
			Value:      value,
			RunID:      run.ID,
			GitCommit:  run.GitCommit,
			Confidence: 1.0,
		}
		points = append(points, point)
	}

	return points
}

func (ta *trendAnalyzer) analyzeTrendForMetric(ctx context.Context, runs []*types.HistoricRun, metric string) (*TrendData, error) {
	dataPoints := ta.extractMetricValues(runs, metric)
	if len(dataPoints) == 0 {
		return nil, fmt.Errorf("no data points for metric %s", metric)
	}

	// Calculate statistics
	statistics := ta.calculateDetailedStatistics(dataPoints)

	// Detect direction
	direction, err := ta.DetectTrendDirection(ctx, runs[0].TestName, metric, 30)
	if err != nil {
		// Use default direction if detection fails
		direction = &TrendDirection{
			Direction:  "stable",
			Strength:   "weak",
			Confidence: 0.0,
		}
	}

	// Calculate moving average
	movingAvg, err := ta.CalculateMovingAverage(ctx, runs[0].TestName, metric, ta.config.MovingAvgWindow, 30)
	if err != nil {
		movingAvg = &MovingAverageResult{Points: []MovingAveragePoint{}}
	}

	// Detect anomalies for this metric
	anomalies := ta.detectAnomaliesForMetric(dataPoints, metric)

	// Detect change points
	changePoints := ta.detectChangePoints(dataPoints)

	return &TrendData{
		TestName:     runs[0].TestName,
		Metric:       metric,
		DataPoints:   dataPoints,
		Statistics:   *statistics,
		Direction:    *direction,
		MovingAvg:    movingAvg.Points,
		Anomalies:    anomalies,
		ChangePoints: changePoints,
	}, nil
}

func (ta *trendAnalyzer) calculateDetailedStatistics(dataPoints []TrendDataPoint) *TrendStatistics {
	if len(dataPoints) == 0 {
		return &TrendStatistics{}
	}

	values := make([]float64, len(dataPoints))
	for i, point := range dataPoints {
		values[i] = point.Value
	}

	mean := ta.calculateMean(values)
	median := ta.calculateMedian(values)
	stdDev := ta.calculateStandardDeviation(values)
	variance := stdDev * stdDev
	min := ta.calculateMin(values)
	max := ta.calculateMax(values)

	percentiles := map[string]float64{
		"p10": ta.calculatePercentile(values, 10),
		"p25": ta.calculatePercentile(values, 25),
		"p50": ta.calculatePercentile(values, 50),
		"p75": ta.calculatePercentile(values, 75),
		"p90": ta.calculatePercentile(values, 90),
		"p95": ta.calculatePercentile(values, 95),
		"p99": ta.calculatePercentile(values, 99),
	}

	regression := ta.calculateLinearRegression(dataPoints)
	autocorr := ta.calculateAutocorrelation(values, 10)

	return &TrendStatistics{
		Count:            len(values),
		Mean:             mean,
		Median:           median,
		StandardDev:      stdDev,
		Variance:         variance,
		Min:              min,
		Max:              max,
		Percentiles:      percentiles,
		LinearRegression: regression,
		Autocorrelation:  autocorr,
	}
}

func (ta *trendAnalyzer) calculateLinearRegression(dataPoints []TrendDataPoint) LinearRegression {
	n := float64(len(dataPoints))
	if n < 2 {
		return LinearRegression{}
	}

	var sumX, sumY, sumXY, sumX2 float64
	for i, point := range dataPoints {
		x := float64(i)
		y := point.Value
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// Calculate slope and intercept
	denominator := n*sumX2 - sumX*sumX
	if denominator == 0 {
		return LinearRegression{}
	}

	slope := (n*sumXY - sumX*sumY) / denominator
	intercept := (sumY - slope*sumX) / n

	// Calculate R-squared
	meanY := sumY / n
	var ssRes, ssTot float64
	for i, point := range dataPoints {
		predicted := slope*float64(i) + intercept
		ssRes += math.Pow(point.Value-predicted, 2)
		ssTot += math.Pow(point.Value-meanY, 2)
	}

	var rSquared float64
	if ssTot > 0 {
		rSquared = 1 - ssRes/ssTot
	}

	// Simple significance test (for demonstration)
	significant := rSquared > 0.5

	equation := fmt.Sprintf("y = %.4fx + %.4f", slope, intercept)

	return LinearRegression{
		Slope:       slope,
		Intercept:   intercept,
		RSquared:    rSquared,
		PValue:      1 - rSquared, // Simplified p-value approximation
		Significant: significant,
		Equation:    equation,
	}
}

func (ta *trendAnalyzer) calculateVolatility(dataPoints []TrendDataPoint) float64 {
	if len(dataPoints) < 2 {
		return 0
	}

	values := make([]float64, len(dataPoints))
	for i, point := range dataPoints {
		values[i] = point.Value
	}

	mean := ta.calculateMean(values)
	var sumSquares float64
	for _, value := range values {
		sumSquares += math.Pow(value-mean, 2)
	}

	variance := sumSquares / float64(len(values)-1)
	return math.Sqrt(variance) / mean // Coefficient of variation
}

func (ta *trendAnalyzer) determineTrendDirection(slope float64, metric string) string {
	threshold := 0.01 // Configurable threshold

	// For latency and error rate, higher values are worse
	// For throughput, higher values are better
	isMetricBetter := func(slope float64, metric string) bool {
		switch metric {
		case "avg_latency", "p95_latency", "p99_latency", "error_rate":
			return slope < 0 // Decreasing is better
		case "throughput":
			return slope > 0 // Increasing is better
		default:
			return slope > 0
		}
	}

	if math.Abs(slope) < threshold {
		return "stable"
	} else if isMetricBetter(slope, metric) {
		return "improving"
	} else {
		return "degrading"
	}
}

func (ta *trendAnalyzer) determineTrendStrength(rSquared, volatility float64) string {
	// Combine R-squared and volatility to determine strength
	score := rSquared * (1 - volatility)

	if score > 0.8 {
		return "very_strong"
	} else if score > 0.6 {
		return "strong"
	} else if score > 0.4 {
		return "moderate"
	} else {
		return "weak"
	}
}

func (ta *trendAnalyzer) calculateTrendConfidence(regression LinearRegression, volatility float64, dataPoints int) float64 {
	// Combine multiple factors for confidence
	dataQuality := math.Min(float64(dataPoints)/100.0, 1.0)
	fitQuality := regression.RSquared
	stabilityQuality := 1 - volatility

	return (dataQuality + fitQuality + stabilityQuality) / 3.0
}

func (ta *trendAnalyzer) calculateTrendScore(regression LinearRegression, volatility float64) float64 {
	// Composite score combining trend strength and reliability
	return regression.RSquared * (1 - volatility) * 100
}

func (ta *trendAnalyzer) detectAnomaliesForMetric(dataPoints []TrendDataPoint, metric string) []AnomalyPoint {
	if len(dataPoints) < ta.config.MinDataPoints {
		return []AnomalyPoint{}
	}

	values := make([]float64, len(dataPoints))
	for i, point := range dataPoints {
		values[i] = point.Value
	}

	mean := ta.calculateMean(values)
	stdDev := ta.calculateStandardDeviation(values)
	threshold := ta.config.AnomalySensitivity * stdDev

	var anomalies []AnomalyPoint
	for _, point := range dataPoints {
		deviation := math.Abs(point.Value - mean)
		if deviation > threshold {
			severity := "mild"
			if deviation > 2*threshold {
				severity = "severe"
			} else if deviation > 1.5*threshold {
				severity = "moderate"
			}

			anomaly := AnomalyPoint{
				Timestamp:      point.Timestamp,
				Value:          point.Value,
				ExpectedValue:  mean,
				DeviationScore: deviation / stdDev,
				Severity:       severity,
				RunID:          point.RunID,
			}
			anomalies = append(anomalies, anomaly)
		}
	}

	return anomalies
}

func (ta *trendAnalyzer) detectChangePoints(dataPoints []TrendDataPoint) []ChangePoint {
	// Simple change point detection using sliding window
	if len(dataPoints) < 20 {
		return []ChangePoint{}
	}

	var changePoints []ChangePoint
	windowSize := 10

	for i := windowSize; i < len(dataPoints)-windowSize; i++ {
		// Calculate statistics for before and after windows
		beforeValues := make([]float64, windowSize)
		afterValues := make([]float64, windowSize)

		for j := 0; j < windowSize; j++ {
			beforeValues[j] = dataPoints[i-windowSize+j].Value
			afterValues[j] = dataPoints[i+j].Value
		}

		beforeMean := ta.calculateMean(beforeValues)
		afterMean := ta.calculateMean(afterValues)
		beforeStd := ta.calculateStandardDeviation(beforeValues)
		afterStd := ta.calculateStandardDeviation(afterValues)

		// Detect level change
		levelChange := math.Abs(afterMean - beforeMean)
		pooledStd := math.Sqrt((beforeStd*beforeStd + afterStd*afterStd) / 2)

		if levelChange > 2*pooledStd {
			magnitude := (afterMean - beforeMean) / beforeMean * 100
			changePoint := ChangePoint{
				Timestamp:   dataPoints[i].Timestamp,
				Type:        "level",
				Magnitude:   magnitude,
				Confidence:  math.Min(levelChange/pooledStd/4, 1.0),
				Description: fmt.Sprintf("Level change detected: %.2f%% change", magnitude),
			}
			changePoints = append(changePoints, changePoint)
		}
	}

	return changePoints
}

func (ta *trendAnalyzer) generateInsights(trends map[string]TrendData) []TrendInsight {
	var insights []TrendInsight

	for metric, trend := range trends {
		// Generate insights based on trend analysis
		if trend.Direction.Direction == "degrading" && trend.Direction.Strength != "weak" {
			insight := TrendInsight{
				Type:  "performance_degradation",
				Title: fmt.Sprintf("%s Performance Degrading", metric),
				Description: fmt.Sprintf("The %s metric shows a %s degrading trend with %.1f%% confidence",
					metric, trend.Direction.Strength, trend.Direction.Confidence*100),
				Severity:   ta.mapStrengthToSeverity(trend.Direction.Strength),
				Confidence: trend.Direction.Confidence,
				Timestamp:  time.Now(),
			}
			insights = append(insights, insight)
		}

		if len(trend.Anomalies) > 0 {
			severeAnomalies := 0
			for _, anomaly := range trend.Anomalies {
				if anomaly.Severity == "severe" {
					severeAnomalies++
				}
			}

			if severeAnomalies > 0 {
				insight := TrendInsight{
					Type:  "anomaly_detection",
					Title: fmt.Sprintf("Anomalies Detected in %s", metric),
					Description: fmt.Sprintf("Detected %d severe anomalies in %s out of %d total anomalies",
						severeAnomalies, metric, len(trend.Anomalies)),
					Severity:   "high",
					Confidence: 0.9,
					Timestamp:  time.Now(),
				}
				insights = append(insights, insight)
			}
		}

		if len(trend.ChangePoints) > 0 {
			recentChangePoints := 0
			for _, cp := range trend.ChangePoints {
				if time.Since(cp.Timestamp) < 7*24*time.Hour {
					recentChangePoints++
				}
			}

			if recentChangePoints > 0 {
				insight := TrendInsight{
					Type:  "change_point",
					Title: fmt.Sprintf("Recent Changes in %s", metric),
					Description: fmt.Sprintf("Detected %d significant changes in %s within the last week",
						recentChangePoints, metric),
					Severity:   "medium",
					Confidence: 0.8,
					Timestamp:  time.Now(),
				}
				insights = append(insights, insight)
			}
		}
	}

	return insights
}

func (ta *trendAnalyzer) generateRecommendations(trends map[string]TrendData, insights []TrendInsight) []string {
	var recommendations []string

	// Analyze insights to generate recommendations
	highSeverityCount := 0
	for _, insight := range insights {
		if insight.Severity == "high" || insight.Severity == "critical" {
			highSeverityCount++
		}
	}

	if highSeverityCount > 0 {
		recommendations = append(recommendations,
			"Immediate investigation recommended due to critical performance issues")
	}

	// Check for degrading trends
	degradingMetrics := []string{}
	for metric, trend := range trends {
		if trend.Direction.Direction == "degrading" {
			degradingMetrics = append(degradingMetrics, metric)
		}
	}

	if len(degradingMetrics) > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Monitor degrading trends in: %v", degradingMetrics))
	}

	// Check for high volatility
	volatileMetrics := []string{}
	for metric, trend := range trends {
		if trend.Direction.Volatility > 0.3 {
			volatileMetrics = append(volatileMetrics, metric)
		}
	}

	if len(volatileMetrics) > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("High volatility detected in: %v - consider investigating system stability", volatileMetrics))
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "Performance trends appear stable - continue monitoring")
	}

	return recommendations
}

func (ta *trendAnalyzer) generateTrendSummary(trends map[string]TrendData) TrendSummary {
	if len(trends) == 0 {
		return TrendSummary{
			OverallDirection: "unknown",
			OverallHealth:    "unknown",
			Volatility:       "unknown",
		}
	}

	improving := 0
	degrading := 0
	stable := 0
	totalVolatility := 0.0
	totalReliability := 0.0

	for _, trend := range trends {
		switch trend.Direction.Direction {
		case "improving":
			improving++
		case "degrading":
			degrading++
		case "stable":
			stable++
		}
		totalVolatility += trend.Direction.Volatility
		totalReliability += trend.Direction.Confidence
	}

	count := float64(len(trends))
	avgVolatility := totalVolatility / count
	avgReliability := totalReliability / count

	// Determine overall direction
	overallDirection := "stable"
	if degrading > improving && degrading > stable {
		overallDirection = "degrading"
	} else if improving > degrading && improving > stable {
		overallDirection = "improving"
	}

	// Determine overall health
	overallHealth := "good"
	if degrading > len(trends)/2 {
		overallHealth = "poor"
	} else if degrading > 0 && improving == 0 {
		overallHealth = "concerning"
	} else if improving > degrading {
		overallHealth = "excellent"
	}

	// Determine volatility level
	volatilityLevel := "low"
	if avgVolatility > 0.4 {
		volatilityLevel = "high"
	} else if avgVolatility > 0.2 {
		volatilityLevel = "medium"
	}

	return TrendSummary{
		OverallDirection:    overallDirection,
		OverallHealth:       overallHealth,
		Volatility:          volatilityLevel,
		Reliability:         avgReliability,
		PredictabilityScore: (1 - avgVolatility) * avgReliability * 100,
	}
}

// Additional helper methods for statistical calculations

func (ta *trendAnalyzer) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (ta *trendAnalyzer) calculateMedian(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

func (ta *trendAnalyzer) calculateStandardDeviation(values []float64) float64 {
	if len(values) <= 1 {
		return 0
	}

	mean := ta.calculateMean(values)
	sumSquares := 0.0
	for _, v := range values {
		sumSquares += math.Pow(v-mean, 2)
	}
	variance := sumSquares / float64(len(values)-1)
	return math.Sqrt(variance)
}

func (ta *trendAnalyzer) calculateMin(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

func (ta *trendAnalyzer) calculateMax(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}

func (ta *trendAnalyzer) calculatePercentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	index := percentile / 100.0 * float64(len(sorted)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))

	if lower == upper {
		return sorted[lower]
	}

	weight := index - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

func (ta *trendAnalyzer) calculateAutocorrelation(values []float64, maxLag int) []float64 {
	if len(values) < maxLag*2 {
		return []float64{}
	}

	mean := ta.calculateMean(values)
	n := len(values)

	autocorr := make([]float64, maxLag+1)

	// Calculate variance (lag 0)
	var c0 float64
	for _, v := range values {
		c0 += math.Pow(v-mean, 2)
	}
	c0 /= float64(n)

	// Calculate autocorrelations
	for lag := 0; lag <= maxLag; lag++ {
		var c float64
		for i := 0; i < n-lag; i++ {
			c += (values[i] - mean) * (values[i+lag] - mean)
		}
		c /= float64(n - lag)
		autocorr[lag] = c / c0
	}

	return autocorr
}

func (ta *trendAnalyzer) calculateSmoothness(points []MovingAveragePoint) float64 {
	if len(points) < 2 {
		return 0
	}

	totalVariation := 0.0
	for i := 1; i < len(points); i++ {
		diff := math.Abs(points[i].MovingAverage - points[i-1].MovingAverage)
		totalVariation += diff
	}

	avgValue := 0.0
	for _, point := range points {
		avgValue += point.MovingAverage
	}
	avgValue /= float64(len(points))

	if avgValue == 0 {
		return 0
	}

	return 1.0 - (totalVariation/float64(len(points)-1))/avgValue
}

func (ta *trendAnalyzer) calculateTrendClarity(points []MovingAveragePoint) float64 {
	if len(points) < 2 {
		return 0
	}

	// Calculate the consistency of trend direction
	consistentDirections := 0
	totalDirections := 0

	for i := 1; i < len(points); i++ {
		if i == 1 {
			continue // Need at least 2 intervals to determine consistency
		}

		currentTrend := points[i].MovingAverage - points[i-1].MovingAverage
		previousTrend := points[i-1].MovingAverage - points[i-2].MovingAverage

		if (currentTrend > 0 && previousTrend > 0) || (currentTrend < 0 && previousTrend < 0) {
			consistentDirections++
		}
		totalDirections++
	}

	if totalDirections == 0 {
		return 0
	}

	return float64(consistentDirections) / float64(totalDirections)
}

func (ta *trendAnalyzer) calculateResiduals(dataPoints []TrendDataPoint, regression LinearRegression) []float64 {
	residuals := make([]float64, len(dataPoints))
	for i, point := range dataPoints {
		predicted := regression.Slope*float64(i) + regression.Intercept
		residuals[i] = point.Value - predicted
	}
	return residuals
}

func (ta *trendAnalyzer) calculateForecastValidation(dataPoints []TrendDataPoint, regression LinearRegression) ForecastValidation {
	residuals := ta.calculateResiduals(dataPoints, regression)

	// Calculate MAE
	mae := 0.0
	for _, residual := range residuals {
		mae += math.Abs(residual)
	}
	mae /= float64(len(residuals))

	// Calculate RMSE
	rmse := 0.0
	for _, residual := range residuals {
		rmse += residual * residual
	}
	rmse = math.Sqrt(rmse / float64(len(residuals)))

	// Calculate MAPE
	mape := 0.0
	for i, residual := range residuals {
		if dataPoints[i].Value != 0 {
			mape += math.Abs(residual / dataPoints[i].Value)
		}
	}
	mape = (mape / float64(len(residuals))) * 100

	return ForecastValidation{
		MAE:  mae,
		RMSE: rmse,
		MAPE: mape,
	}
}

func (ta *trendAnalyzer) getXValues(dataPoints []TrendDataPoint) []float64 {
	values := make([]float64, len(dataPoints))
	for i := range dataPoints {
		values[i] = float64(i)
	}
	return values
}

func (ta *trendAnalyzer) calculateSumOfSquares(values []float64) float64 {
	mean := ta.calculateMean(values)
	sumSquares := 0.0
	for _, v := range values {
		sumSquares += math.Pow(v-mean, 2)
	}
	return sumSquares
}

func (ta *trendAnalyzer) mapStrengthToSeverity(strength string) string {
	switch strength {
	case "very_strong":
		return "critical"
	case "strong":
		return "high"
	case "moderate":
		return "medium"
	default:
		return "low"
	}
}

// Placeholder implementations for method and client specific analysis
func (ta *trendAnalyzer) getMethodData(ctx context.Context, testName, method string, days int) ([]*types.HistoricRun, error) {
	// This would query for method-specific data
	// For now, return regular historic data
	return ta.getHistoricData(ctx, testName, days)
}

func (ta *trendAnalyzer) getClientData(ctx context.Context, testName, client string, days int) ([]*types.HistoricRun, error) {
	// This would query for client-specific data
	// For now, return regular historic data
	return ta.getHistoricData(ctx, testName, days)
}

func (ta *trendAnalyzer) analyzeMethodTrendForMetric(ctx context.Context, runs []*types.HistoricRun, metric, method string) (*TrendData, error) {
	// This would analyze method-specific metrics
	// For now, use regular trend analysis
	return ta.analyzeTrendForMetric(ctx, runs, metric)
}

func (ta *trendAnalyzer) analyzeClientTrendForMetric(ctx context.Context, runs []*types.HistoricRun, metric, client string) (*TrendData, error) {
	// This would analyze client-specific metrics
	// For now, use regular trend analysis
	return ta.analyzeTrendForMetric(ctx, runs, metric)
}

func (ta *trendAnalyzer) generateMethodComparison(ctx context.Context, testName, method string, days int) (map[string]MethodComparison, error) {
	// Placeholder implementation
	return make(map[string]MethodComparison), nil
}

func (ta *trendAnalyzer) generateMethodRanking(ctx context.Context, testName string, days int) ([]MethodRanking, error) {
	// Placeholder implementation
	return []MethodRanking{}, nil
}

func (ta *trendAnalyzer) generateClientComparison(ctx context.Context, testName, client string, days int) (map[string]ClientTrendComparison, error) {
	// Placeholder implementation
	return make(map[string]ClientTrendComparison), nil
}

func (ta *trendAnalyzer) generateClientRanking(ctx context.Context, testName string, days int) ([]ClientRanking, error) {
	// Placeholder implementation
	return []ClientRanking{}, nil
}
