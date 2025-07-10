package analysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// RegressionDetector provides comprehensive regression detection capabilities
type RegressionDetector interface {
	Start(ctx context.Context) error
	Stop() error

	// Threshold management
	SetThreshold(metric string, threshold RegressionThreshold) error
	GetThresholds() map[string]RegressionThreshold

	// Regression detection
	DetectRegressions(ctx context.Context, runID string, options DetectionOptions) (*RegressionReport, error)
	AnalyzeRun(ctx context.Context, runID string) (*RunAnalysis, error)

	// Severity classification
	GetSeverity(metric string, percentChange float64) string
	ClassifyRegression(regression *types.Regression) string

	// Comparison modes
	CompareToSequential(ctx context.Context, runID string, lookback int) ([]*types.Regression, error)
	CompareToBaseline(ctx context.Context, runID, baselineName string) ([]*types.Regression, error)
	CompareToRollingAverage(ctx context.Context, runID string, windowSize int) ([]*types.Regression, error)

	// Regression management
	SaveRegressions(ctx context.Context, regressions []*types.Regression) error
	GetRegressions(ctx context.Context, runID string) ([]*types.Regression, error)
	AcknowledgeRegression(ctx context.Context, regressionID, acknowledgedBy string) error
}

// RegressionThreshold defines detection thresholds for different metrics
type RegressionThreshold struct {
	MetricName        string  `json:"metric_name"`
	MinorThreshold    float64 `json:"minor_threshold"`    // Minor regression threshold (default: 5%)
	MajorThreshold    float64 `json:"major_threshold"`    // Major regression threshold (default: 10%)
	CriticalThreshold float64 `json:"critical_threshold"` // Critical regression threshold (default: 20%)
	MinSampleSize     int     `json:"min_sample_size"`    // Minimum sample size for statistical tests
	SignificanceLevel float64 `json:"significance_level"` // Statistical significance level (default: 0.05)
	IsPercentage      bool    `json:"is_percentage"`      // Whether threshold values are percentages
	Direction         string  `json:"direction"`          // "increase", "decrease", or "both"
}

// DetectionOptions configures regression detection behavior
type DetectionOptions struct {
	ComparisonMode     string                         `json:"comparison_mode"`     // "sequential", "baseline", "rolling_average"
	BaselineName       string                         `json:"baseline_name"`       // For baseline comparison
	LookbackCount      int                            `json:"lookback_count"`      // Number of previous runs to compare
	WindowSize         int                            `json:"window_size"`         // For rolling average comparison
	EnableStatistical  bool                           `json:"enable_statistical"`  // Enable statistical significance testing
	CustomThresholds   map[string]RegressionThreshold `json:"custom_thresholds"`   // Override default thresholds
	IncludeClients     []string                       `json:"include_clients"`     // Only analyze specific clients
	ExcludeClients     []string                       `json:"exclude_clients"`     // Exclude specific clients
	IncludeMethods     []string                       `json:"include_methods"`     // Only analyze specific methods
	ExcludeMethods     []string                       `json:"exclude_methods"`     // Exclude specific methods
	MinConfidence      float64                        `json:"min_confidence"`      // Minimum confidence level for reporting
	IgnoreImprovements bool                           `json:"ignore_improvements"` // Don't report improvements as negative regressions
}

// RegressionReport provides comprehensive regression analysis results
type RegressionReport struct {
	RunID           string                     `json:"run_id"`
	TestName        string                     `json:"test_name"`
	GeneratedAt     time.Time                  `json:"generated_at"`
	ComparisonMode  string                     `json:"comparison_mode"`
	BaselineInfo    *BaselineInfo              `json:"baseline_info,omitempty"`
	Options         DetectionOptions           `json:"options"`
	Summary         RegressionSummary          `json:"summary"`
	Regressions     []*types.Regression        `json:"regressions"`
	Improvements    []*types.Improvement       `json:"improvements"`
	ClientAnalysis  map[string]*ClientAnalysis `json:"client_analysis"`
	MethodAnalysis  map[string]*MethodAnalysis `json:"method_analysis,omitempty"`
	Recommendations []string                   `json:"recommendations"`
	RiskAssessment  RiskAssessment             `json:"risk_assessment"`
	StatisticalInfo *StatisticalInfo           `json:"statistical_info,omitempty"`
}

// BaselineInfo contains information about the baseline used for comparison
type BaselineInfo struct {
	Name        string    `json:"name"`
	RunID       string    `json:"run_id"`
	CreatedAt   time.Time `json:"created_at"`
	Description string    `json:"description"`
}

// RegressionSummary provides high-level regression analysis results
type RegressionSummary struct {
	TotalRegressions     int     `json:"total_regressions"`
	CriticalRegressions  int     `json:"critical_regressions"`
	MajorRegressions     int     `json:"major_regressions"`
	MinorRegressions     int     `json:"minor_regressions"`
	TotalImprovements    int     `json:"total_improvements"`
	OverallHealthScore   float64 `json:"overall_health_score"`  // 0-100 scale
	PerformanceScore     float64 `json:"performance_score"`     // Weighted performance score
	RegressionScore      float64 `json:"regression_score"`      // Score based on regression severity
	ComparisonConfidence float64 `json:"comparison_confidence"` // Statistical confidence
	ClientsAffected      int     `json:"clients_affected"`
	MethodsAffected      int     `json:"methods_affected"`
	MostAffectedClient   string  `json:"most_affected_client"`
	MostAffectedMethod   string  `json:"most_affected_method"`
	RecommendedAction    string  `json:"recommended_action"` // "investigate", "monitor", "none"
}

// ClientAnalysis provides detailed analysis for a specific client
type ClientAnalysis struct {
	ClientName       string                      `json:"client_name"`
	OverallStatus    string                      `json:"overall_status"` // "improved", "degraded", "stable"
	RegressionCount  int                         `json:"regression_count"`
	ImprovementCount int                         `json:"improvement_count"`
	HealthScore      float64                     `json:"health_score"` // 0-100
	MetricChanges    map[string]*MetricChange    `json:"metric_changes"`
	Regressions      []*types.Regression         `json:"regressions"`
	Improvements     []*types.Improvement        `json:"improvements"`
	StatisticalTests map[string]*StatisticalTest `json:"statistical_tests,omitempty"`
	Recommendations  []string                    `json:"recommendations"`
	RiskLevel        string                      `json:"risk_level"` // "low", "medium", "high", "critical"
}

// MethodAnalysis provides detailed analysis for a specific method
type MethodAnalysis struct {
	MethodName       string                   `json:"method_name"`
	ClientResults    map[string]*MetricChange `json:"client_results"` // Results per client
	OverallTrend     string                   `json:"overall_trend"`  // "improving", "degrading", "stable"
	RegressionCount  int                      `json:"regression_count"`
	ImprovementCount int                      `json:"improvement_count"`
	HealthScore      float64                  `json:"health_score"`
	Recommendations  []string                 `json:"recommendations"`
}

// MetricChange represents a detailed change in a specific metric
type MetricChange struct {
	MetricName        string               `json:"metric_name"`
	BaselineValue     float64              `json:"baseline_value"`
	CurrentValue      float64              `json:"current_value"`
	AbsoluteChange    float64              `json:"absolute_change"`
	PercentChange     float64              `json:"percent_change"`
	IsRegression      bool                 `json:"is_regression"`
	IsImprovement     bool                 `json:"is_improvement"`
	IsSignificant     bool                 `json:"is_significant"`
	Severity          string               `json:"severity"`
	Confidence        float64              `json:"confidence"`
	StatisticalTest   *StatisticalTest     `json:"statistical_test,omitempty"`
	ThresholdBreached *RegressionThreshold `json:"threshold_breached,omitempty"`
	HistoricalContext *HistoricalContext   `json:"historical_context,omitempty"`
}

// StatisticalTest represents results of statistical significance testing
type StatisticalTest struct {
	TestType           string     `json:"test_type"` // "t_test", "mann_whitney_u", "kolmogorov_smirnov"
	PValue             float64    `json:"p_value"`
	EffectSize         float64    `json:"effect_size"` // Cohen's d or similar
	ConfidenceInterval [2]float64 `json:"confidence_interval"`
	SampleSize1        int        `json:"sample_size_1"`
	SampleSize2        int        `json:"sample_size_2"`
	IsSignificant      bool       `json:"is_significant"`
	PowerAnalysis      float64    `json:"power_analysis"` // Statistical power
	Interpretation     string     `json:"interpretation"`
}

// HistoricalContext provides context about historical performance
type HistoricalContext struct {
	RecentTrend       string     `json:"recent_trend"`       // "improving", "degrading", "stable", "volatile"
	TrendStrength     float64    `json:"trend_strength"`     // 0-1, strength of trend
	Volatility        float64    `json:"volatility"`         // Standard deviation of recent values
	HistoricalRange   [2]float64 `json:"historical_range"`   // Min/max values from history
	PercentileRanking float64    `json:"percentile_ranking"` // Where current value ranks (0-100)
	IsOutlier         bool       `json:"is_outlier"`         // Whether current value is an outlier
	OutlierThreshold  float64    `json:"outlier_threshold"`  // Z-score threshold used
	RecentValues      []float64  `json:"recent_values"`      // Last N values for context
}

// RiskAssessment provides overall risk analysis
type RiskAssessment struct {
	OverallRisk           string             `json:"overall_risk"` // "low", "medium", "high", "critical"
	RiskFactors           []string           `json:"risk_factors"`
	RiskScore             float64            `json:"risk_score"` // 0-100
	ClientRiskScores      map[string]float64 `json:"client_risk_scores"`
	MethodRiskScores      map[string]float64 `json:"method_risk_scores,omitempty"`
	ImpactAssessment      string             `json:"impact_assessment"` // Description of potential impact
	MitigationSuggestions []string           `json:"mitigation_suggestions"`
	MonitoringPriority    string             `json:"monitoring_priority"` // "immediate", "high", "normal", "low"
}

// StatisticalInfo provides statistical analysis details
type StatisticalInfo struct {
	ComparisonMethod    string             `json:"comparison_method"`
	SampleSizes         map[string]int     `json:"sample_sizes"`
	ConfidenceLevels    map[string]float64 `json:"confidence_levels"`
	EffectSizes         map[string]float64 `json:"effect_sizes"`
	PowerAnalysis       map[string]float64 `json:"power_analysis"`
	MultipleComparisons bool               `json:"multiple_comparisons"`
	CorrectionMethod    string             `json:"correction_method,omitempty"`
	OverallSignificance float64            `json:"overall_significance"`
}

// RunAnalysis provides comprehensive analysis of a single run
type RunAnalysis struct {
	RunID              string                    `json:"run_id"`
	TestName           string                    `json:"test_name"`
	Timestamp          time.Time                 `json:"timestamp"`
	OverallHealthScore float64                   `json:"overall_health_score"`
	PerformanceScore   float64                   `json:"performance_score"`
	ClientScores       map[string]float64        `json:"client_scores"`
	Anomalies          []*PerformanceAnomaly     `json:"anomalies"`
	Trends             map[string]*TrendAnalysis `json:"trends"`
	Recommendations    []string                  `json:"recommendations"`
	ComparisonHistory  []*HistoricalComparison   `json:"comparison_history"`
	QualityMetrics     *QualityMetrics           `json:"quality_metrics"`
}

// PerformanceAnomaly represents an anomaly detected in performance data
type PerformanceAnomaly struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"` // "spike", "drop", "outlier", "pattern"
	Client        string    `json:"client"`
	Method        string    `json:"method,omitempty"`
	Metric        string    `json:"metric"`
	Value         float64   `json:"value"`
	ExpectedValue float64   `json:"expected_value"`
	Deviation     float64   `json:"deviation"`
	Severity      string    `json:"severity"`
	Confidence    float64   `json:"confidence"`
	DetectedAt    time.Time `json:"detected_at"`
	Description   string    `json:"description"`
}

// TrendAnalysis provides trend analysis for metrics over time
type TrendAnalysis struct {
	MetricName     string       `json:"metric_name"`
	Client         string       `json:"client"`
	Method         string       `json:"method,omitempty"`
	TrendDirection string       `json:"trend_direction"` // "improving", "degrading", "stable"
	TrendStrength  float64      `json:"trend_strength"`  // 0-1
	Slope          float64      `json:"slope"`
	RSquared       float64      `json:"r_squared"`
	DataPoints     int          `json:"data_points"`
	TimeRange      [2]time.Time `json:"time_range"`
	Forecast       *Forecast    `json:"forecast,omitempty"`
}

// Forecast provides performance forecasting
type Forecast struct {
	PredictedValue     float64       `json:"predicted_value"`
	ConfidenceInterval [2]float64    `json:"confidence_interval"`
	ForecastHorizon    time.Duration `json:"forecast_horizon"`
	Model              string        `json:"model"`
	Accuracy           float64       `json:"accuracy"`
}

// HistoricalComparison represents comparison with historical runs
type HistoricalComparison struct {
	ComparedRunID  string        `json:"compared_run_id"`
	ComparedAt     time.Time     `json:"compared_at"`
	TimeDifference time.Duration `json:"time_difference"`
	OverallChange  float64       `json:"overall_change"`
	Significance   string        `json:"significance"`
	Summary        string        `json:"summary"`
}

// QualityMetrics provides quality assessment of the benchmark run
type QualityMetrics struct {
	DataCompleteness     float64  `json:"data_completeness"`     // 0-1
	MeasurementStability float64  `json:"measurement_stability"` // 0-1
	NoiseLevel           float64  `json:"noise_level"`           // 0-1
	OutlierRate          float64  `json:"outlier_rate"`          // 0-1
	ConsistencyScore     float64  `json:"consistency_score"`     // 0-1
	ReliabilityScore     float64  `json:"reliability_score"`     // 0-1
	OverallQuality       float64  `json:"overall_quality"`       // 0-1
	QualityIssues        []string `json:"quality_issues"`
}

// regressionDetector implements RegressionDetector interface
type regressionDetector struct {
	storage         storage.HistoricStorage
	baselineManager BaselineManager
	db              *sql.DB
	log             logrus.FieldLogger
	thresholds      map[string]RegressionThreshold
}

// NewRegressionDetector creates a new regression detector
func NewRegressionDetector(historicStorage storage.HistoricStorage, baselineManager BaselineManager, db *sql.DB, log logrus.FieldLogger) RegressionDetector {
	rd := &regressionDetector{
		storage:         historicStorage,
		baselineManager: baselineManager,
		db:              db,
		log:             log.WithField("component", "regression-detector"),
		thresholds:      make(map[string]RegressionThreshold),
	}

	// Initialize default thresholds
	rd.initializeDefaultThresholds()

	return rd
}

// Start initializes the regression detector
func (rd *regressionDetector) Start(ctx context.Context) error {
	rd.log.Info("Starting regression detector")

	// Create regressions table if it doesn't exist
	if err := rd.createRegressionsTable(ctx); err != nil {
		return fmt.Errorf("failed to create regressions table: %w", err)
	}

	rd.log.Info("Regression detector started successfully")
	return nil
}

// Stop shuts down the regression detector
func (rd *regressionDetector) Stop() error {
	rd.log.Info("Stopping regression detector")
	return nil
}

// SetThreshold sets a custom threshold for a specific metric
func (rd *regressionDetector) SetThreshold(metric string, threshold RegressionThreshold) error {
	rd.log.WithFields(logrus.Fields{
		"metric":             metric,
		"minor_threshold":    threshold.MinorThreshold,
		"major_threshold":    threshold.MajorThreshold,
		"critical_threshold": threshold.CriticalThreshold,
	}).Info("Setting regression threshold")

	threshold.MetricName = metric
	rd.thresholds[metric] = threshold
	return nil
}

// GetThresholds returns all configured thresholds
func (rd *regressionDetector) GetThresholds() map[string]RegressionThreshold {
	return rd.thresholds
}

// DetectRegressions performs comprehensive regression detection on a run
func (rd *regressionDetector) DetectRegressions(ctx context.Context, runID string, options DetectionOptions) (*RegressionReport, error) {
	rd.log.WithFields(logrus.Fields{
		"run_id":          runID,
		"comparison_mode": options.ComparisonMode,
	}).Info("Detecting regressions")

	// Get the run
	run, err := rd.storage.GetHistoricRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic run: %w", err)
	}

	// Initialize report
	report := &RegressionReport{
		RunID:          runID,
		TestName:       run.TestName,
		GeneratedAt:    time.Now(),
		ComparisonMode: options.ComparisonMode,
		Options:        options,
		ClientAnalysis: make(map[string]*ClientAnalysis),
		MethodAnalysis: make(map[string]*MethodAnalysis),
	}

	// Detect regressions based on comparison mode
	var regressions []*types.Regression
	var improvements []*types.Improvement

	switch options.ComparisonMode {
	case "sequential":
		regressions, err = rd.CompareToSequential(ctx, runID, options.LookbackCount)
	case "baseline":
		if options.BaselineName == "" {
			return nil, fmt.Errorf("baseline name required for baseline comparison mode")
		}
		regressions, err = rd.CompareToBaseline(ctx, runID, options.BaselineName)
		// Get baseline info
		baseline, baselineErr := rd.baselineManager.GetBaseline(ctx, options.BaselineName)
		if baselineErr == nil {
			report.BaselineInfo = &BaselineInfo{
				Name:        baseline.Name,
				RunID:       baseline.RunID,
				CreatedAt:   baseline.CreatedAt,
				Description: baseline.Description,
			}
		}
	case "rolling_average":
		regressions, err = rd.CompareToRollingAverage(ctx, runID, options.WindowSize)
	default:
		return nil, fmt.Errorf("invalid comparison mode: %s", options.ComparisonMode)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to detect regressions: %w", err)
	}

	// Apply custom thresholds if specified
	if options.CustomThresholds != nil {
		regressions = rd.applyCustomThresholds(regressions, options.CustomThresholds)
	}

	// Filter by client/method includes/excludes
	regressions = rd.filterRegressions(regressions, options)

	// Extract improvements from regressions (positive changes)
	improvements = rd.extractImprovements(regressions, options)

	// Remove improvements from regressions if requested
	if options.IgnoreImprovements {
		regressions = rd.removeImprovements(regressions)
	}

	// Perform statistical analysis if enabled
	if options.EnableStatistical {
		rd.performStatisticalAnalysis(ctx, regressions, run, options)
	}

	// Build detailed analysis
	rd.buildClientAnalysis(ctx, report, run, regressions, improvements, options)
	rd.buildMethodAnalysis(ctx, report, run, regressions, improvements, options)

	// Generate summary
	report.Summary = rd.generateSummary(regressions, improvements, report.ClientAnalysis)
	report.Regressions = regressions
	report.Improvements = improvements

	// Risk assessment
	report.RiskAssessment = rd.assessRisk(regressions, improvements, report.ClientAnalysis)

	// Generate recommendations
	report.Recommendations = rd.generateRecommendations(report)

	// Statistical info
	if options.EnableStatistical {
		report.StatisticalInfo = rd.generateStatisticalInfo(regressions, options)
	}

	// Save regressions to database
	if len(regressions) > 0 {
		if err := rd.SaveRegressions(ctx, regressions); err != nil {
			rd.log.WithError(err).Warn("Failed to save regressions to database")
		}
	}

	rd.log.WithFields(logrus.Fields{
		"run_id":            runID,
		"regressions_found": len(regressions),
		"improvements":      len(improvements),
		"overall_health":    report.Summary.OverallHealthScore,
		"risk_level":        report.RiskAssessment.OverallRisk,
	}).Info("Regression detection completed")

	return report, nil
}

// AnalyzeRun performs comprehensive analysis of a single run
func (rd *regressionDetector) AnalyzeRun(ctx context.Context, runID string) (*RunAnalysis, error) {
	rd.log.WithField("run_id", runID).Info("Analyzing run")

	// Get the run
	run, err := rd.storage.GetHistoricRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic run: %w", err)
	}

	// Parse full results
	var fullResult types.BenchmarkResult
	if err := json.Unmarshal(run.FullResults, &fullResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal full results: %w", err)
	}

	analysis := &RunAnalysis{
		RunID:        runID,
		TestName:     run.TestName,
		Timestamp:    run.Timestamp,
		ClientScores: make(map[string]float64),
		Trends:       make(map[string]*TrendAnalysis),
	}

	// Calculate overall health and performance scores
	analysis.OverallHealthScore = rd.calculateHealthScore(&fullResult)
	analysis.PerformanceScore = rd.calculatePerformanceScore(&fullResult)

	// Calculate client scores
	for clientName, metrics := range fullResult.ClientMetrics {
		analysis.ClientScores[clientName] = rd.calculateClientScore(metrics)
	}

	// Detect anomalies
	analysis.Anomalies = rd.detectAnomalies(ctx, &fullResult, run)

	// Analyze trends
	rd.analyzeTrends(ctx, analysis, run)

	// Quality assessment
	analysis.QualityMetrics = rd.assessQuality(&fullResult)

	// Generate recommendations
	analysis.Recommendations = rd.generateRunRecommendations(analysis)

	// Get comparison history
	analysis.ComparisonHistory = rd.getComparisonHistory(ctx, runID, 5)

	rd.log.WithFields(logrus.Fields{
		"run_id":        runID,
		"health_score":  analysis.OverallHealthScore,
		"perf_score":    analysis.PerformanceScore,
		"anomalies":     len(analysis.Anomalies),
		"quality_score": analysis.QualityMetrics.OverallQuality,
	}).Info("Run analysis completed")

	return analysis, nil
}

// GetSeverity determines the severity level for a metric change
func (rd *regressionDetector) GetSeverity(metric string, percentChange float64) string {
	threshold, exists := rd.thresholds[metric]
	if !exists {
		threshold = rd.thresholds["default"]
	}

	absChange := math.Abs(percentChange)

	if absChange >= threshold.CriticalThreshold {
		return "critical"
	} else if absChange >= threshold.MajorThreshold {
		return "major"
	} else if absChange >= threshold.MinorThreshold {
		return "minor"
	}
	return "low"
}

// ClassifyRegression classifies a regression based on multiple factors
func (rd *regressionDetector) ClassifyRegression(regression *types.Regression) string {
	severity := rd.GetSeverity(regression.Metric, regression.PercentChange)

	// Enhance classification based on additional factors
	if regression.IsSignificant && severity == "major" {
		return "critical"
	}
	if !regression.IsSignificant && severity == "critical" {
		return "major"
	}

	return severity
}

// CompareToSequential compares against previous sequential runs
func (rd *regressionDetector) CompareToSequential(ctx context.Context, runID string, lookback int) ([]*types.Regression, error) {
	rd.log.WithFields(logrus.Fields{
		"run_id":   runID,
		"lookback": lookback,
	}).Info("Comparing to sequential runs")

	// Get current run
	currentRun, err := rd.storage.GetHistoricRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	// Get previous runs
	filter := types.RunFilter{
		TestName: currentRun.TestName,
		Limit:    lookback + 1,
	}
	previousRuns, err := rd.storage.ListHistoricRuns(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Filter out current run and get previous runs
	var prevRuns []*types.HistoricRun
	for _, run := range previousRuns {
		if run.ID != runID && run.Timestamp.Before(currentRun.Timestamp) {
			prevRuns = append(prevRuns, run)
		}
	}

	if len(prevRuns) == 0 {
		rd.log.Info("No previous runs found for comparison")
		return []*types.Regression{}, nil
	}

	// Sort by timestamp descending and take the most recent
	sort.Slice(prevRuns, func(i, j int) bool {
		return prevRuns[i].Timestamp.After(prevRuns[j].Timestamp)
	})

	// Take the specified number of previous runs
	if len(prevRuns) > lookback {
		prevRuns = prevRuns[:lookback]
	}

	// Compare against the most recent previous run primarily
	baseline := prevRuns[0]

	return rd.compareRuns(ctx, currentRun, baseline, "sequential")
}

// CompareToBaseline compares against a specific baseline
func (rd *regressionDetector) CompareToBaseline(ctx context.Context, runID, baselineName string) ([]*types.Regression, error) {
	rd.log.WithFields(logrus.Fields{
		"run_id":        runID,
		"baseline_name": baselineName,
	}).Info("Comparing to baseline")

	// Use baseline manager to detect regressions
	regressions, err := rd.baselineManager.DetectRegressions(ctx, runID, baselineName, RegressionThresholds{
		ErrorRateThreshold:  0.01, // 1% absolute increase
		LatencyThreshold:    5.0,  // 5% increase
		ThroughputThreshold: 5.0,  // 5% decrease
		SignificanceLevel:   0.05,
		MinSampleSize:       10,
		ConsecutiveRuns:     1,
	})

	return regressions, err
}

// CompareToRollingAverage compares against rolling average of previous runs
func (rd *regressionDetector) CompareToRollingAverage(ctx context.Context, runID string, windowSize int) ([]*types.Regression, error) {
	rd.log.WithFields(logrus.Fields{
		"run_id":      runID,
		"window_size": windowSize,
	}).Info("Comparing to rolling average")

	// Get current run
	currentRun, err := rd.storage.GetHistoricRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	// Get previous runs for rolling average
	filter := types.RunFilter{
		TestName: currentRun.TestName,
		Limit:    windowSize + 1,
	}
	previousRuns, err := rd.storage.ListHistoricRuns(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Filter out current run and get previous runs
	var prevRuns []*types.HistoricRun
	for _, run := range previousRuns {
		if run.ID != runID && run.Timestamp.Before(currentRun.Timestamp) {
			prevRuns = append(prevRuns, run)
		}
	}

	if len(prevRuns) < windowSize {
		rd.log.WithFields(logrus.Fields{
			"available_runs": len(prevRuns),
			"required_runs":  windowSize,
		}).Warn("Insufficient previous runs for rolling average")

		if len(prevRuns) == 0 {
			return []*types.Regression{}, nil
		}
		windowSize = len(prevRuns)
	}

	// Sort by timestamp descending and take the window
	sort.Slice(prevRuns, func(i, j int) bool {
		return prevRuns[i].Timestamp.After(prevRuns[j].Timestamp)
	})
	prevRuns = prevRuns[:windowSize]

	// Calculate rolling averages
	avgRun := rd.calculateRollingAverage(prevRuns)

	return rd.compareRuns(ctx, currentRun, avgRun, "rolling_average")
}

// SaveRegressions saves regressions to the database
func (rd *regressionDetector) SaveRegressions(ctx context.Context, regressions []*types.Regression) error {
	if len(regressions) == 0 {
		return nil
	}

	rd.log.WithField("count", len(regressions)).Info("Saving regressions to database")

	tx, err := rd.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO regressions (
			id, run_id, baseline_run_id, client, metric, method,
			baseline_value, current_value, percent_change, absolute_change,
			severity, is_significant, p_value, detected_at, notes
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (id) DO UPDATE SET
			severity = EXCLUDED.severity,
			is_significant = EXCLUDED.is_significant,
			p_value = EXCLUDED.p_value`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, regression := range regressions {
		_, err = stmt.ExecContext(ctx,
			regression.ID, regression.RunID, regression.BaselineRunID,
			regression.Client, regression.Metric, regression.Method,
			regression.BaselineValue, regression.CurrentValue,
			regression.PercentChange, regression.AbsoluteChange,
			regression.Severity, regression.IsSignificant, regression.PValue,
			regression.DetectedAt, regression.Notes)
		if err != nil {
			return fmt.Errorf("failed to insert regression: %w", err)
		}
	}

	return tx.Commit()
}

// GetRegressions retrieves regressions for a run
func (rd *regressionDetector) GetRegressions(ctx context.Context, runID string) ([]*types.Regression, error) {
	query := `
		SELECT id, run_id, baseline_run_id, client, metric, method,
			   baseline_value, current_value, percent_change, absolute_change,
			   severity, is_significant, p_value, detected_at, acknowledged_at,
			   acknowledged_by, notes
		FROM regressions
		WHERE run_id = $1
		ORDER BY severity DESC, percent_change DESC`

	rows, err := rd.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query regressions: %w", err)
	}
	defer rows.Close()

	var regressions []*types.Regression
	for rows.Next() {
		regression := &types.Regression{}
		err := rows.Scan(
			&regression.ID, &regression.RunID, &regression.BaselineRunID,
			&regression.Client, &regression.Metric, &regression.Method,
			&regression.BaselineValue, &regression.CurrentValue,
			&regression.PercentChange, &regression.AbsoluteChange,
			&regression.Severity, &regression.IsSignificant, &regression.PValue,
			&regression.DetectedAt, &regression.AcknowledgedAt,
			&regression.AcknowledgedBy, &regression.Notes)
		if err != nil {
			return nil, fmt.Errorf("failed to scan regression: %w", err)
		}
		regressions = append(regressions, regression)
	}

	return regressions, rows.Err()
}

// AcknowledgeRegression marks a regression as acknowledged
func (rd *regressionDetector) AcknowledgeRegression(ctx context.Context, regressionID, acknowledgedBy string) error {
	rd.log.WithFields(logrus.Fields{
		"regression_id":   regressionID,
		"acknowledged_by": acknowledgedBy,
	}).Info("Acknowledging regression")

	query := `
		UPDATE regressions 
		SET acknowledged_at = CURRENT_TIMESTAMP, acknowledged_by = $2
		WHERE id = $1`

	result, err := rd.db.ExecContext(ctx, query, regressionID, acknowledgedBy)
	if err != nil {
		return fmt.Errorf("failed to acknowledge regression: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("regression not found: %s", regressionID)
	}

	return nil
}

// Helper methods

func (rd *regressionDetector) initializeDefaultThresholds() {
	defaultThresholds := map[string]RegressionThreshold{
		"default": {
			MinorThreshold:    5.0,
			MajorThreshold:    10.0,
			CriticalThreshold: 20.0,
			MinSampleSize:     10,
			SignificanceLevel: 0.05,
			IsPercentage:      true,
			Direction:         "both",
		},
		"error_rate": {
			MinorThreshold:    1.0,  // 1% absolute increase
			MajorThreshold:    5.0,  // 5% absolute increase
			CriticalThreshold: 10.0, // 10% absolute increase
			MinSampleSize:     10,
			SignificanceLevel: 0.05,
			IsPercentage:      false,
			Direction:         "increase",
		},
		"latency": {
			MinorThreshold:    5.0,  // 5% increase
			MajorThreshold:    15.0, // 15% increase
			CriticalThreshold: 30.0, // 30% increase
			MinSampleSize:     10,
			SignificanceLevel: 0.05,
			IsPercentage:      true,
			Direction:         "increase",
		},
		"throughput": {
			MinorThreshold:    5.0,  // 5% decrease
			MajorThreshold:    15.0, // 15% decrease
			CriticalThreshold: 30.0, // 30% decrease
			MinSampleSize:     10,
			SignificanceLevel: 0.05,
			IsPercentage:      true,
			Direction:         "decrease",
		},
	}

	for name, threshold := range defaultThresholds {
		threshold.MetricName = name
		rd.thresholds[name] = threshold
	}
}

func (rd *regressionDetector) createRegressionsTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS regressions (
			id VARCHAR(255) PRIMARY KEY,
			run_id VARCHAR(255) NOT NULL,
			baseline_run_id VARCHAR(255),
			client VARCHAR(255) NOT NULL,
			metric VARCHAR(255) NOT NULL,
			method VARCHAR(255),
			baseline_value DOUBLE PRECISION NOT NULL,
			current_value DOUBLE PRECISION NOT NULL,
			percent_change DOUBLE PRECISION NOT NULL,
			absolute_change DOUBLE PRECISION NOT NULL,
			severity VARCHAR(50) NOT NULL,
			is_significant BOOLEAN NOT NULL DEFAULT false,
			p_value DOUBLE PRECISION,
			detected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			acknowledged_at TIMESTAMP,
			acknowledged_by VARCHAR(255),
			notes TEXT,
			
			FOREIGN KEY (run_id) REFERENCES historic_runs(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_regressions_run_id ON regressions(run_id);
		CREATE INDEX IF NOT EXISTS idx_regressions_severity ON regressions(severity);
		CREATE INDEX IF NOT EXISTS idx_regressions_client ON regressions(client);
		CREATE INDEX IF NOT EXISTS idx_regressions_metric ON regressions(metric);
		CREATE INDEX IF NOT EXISTS idx_regressions_detected_at ON regressions(detected_at);`

	_, err := rd.db.ExecContext(ctx, query)
	return err
}

func (rd *regressionDetector) compareRuns(ctx context.Context, current, baseline *types.HistoricRun, comparisonMode string) ([]*types.Regression, error) {
	var regressions []*types.Regression

	// Parse full results for detailed comparison
	var currentResult, baselineResult types.BenchmarkResult
	if err := json.Unmarshal(current.FullResults, &currentResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal current results: %w", err)
	}
	if err := json.Unmarshal(baseline.FullResults, &baselineResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal baseline results: %w", err)
	}

	// Compare client-level metrics
	for clientName, currentMetrics := range currentResult.ClientMetrics {
		baselineMetrics, exists := baselineResult.ClientMetrics[clientName]
		if !exists {
			continue
		}

		// Compare error rate
		if regression := rd.checkMetricRegression(current.ID, baseline.ID, clientName, "",
			"error_rate", baselineMetrics.ErrorRate, currentMetrics.ErrorRate); regression != nil {
			regressions = append(regressions, regression)
		}

		// Compare average latency
		if regression := rd.checkMetricRegression(current.ID, baseline.ID, clientName, "",
			"avg_latency", baselineMetrics.Latency.Avg, currentMetrics.Latency.Avg); regression != nil {
			regressions = append(regressions, regression)
		}

		// Compare P95 latency
		if regression := rd.checkMetricRegression(current.ID, baseline.ID, clientName, "",
			"p95_latency", baselineMetrics.Latency.P95, currentMetrics.Latency.P95); regression != nil {
			regressions = append(regressions, regression)
		}

		// Compare P99 latency
		if regression := rd.checkMetricRegression(current.ID, baseline.ID, clientName, "",
			"p99_latency", baselineMetrics.Latency.P99, currentMetrics.Latency.P99); regression != nil {
			regressions = append(regressions, regression)
		}

		// Compare throughput
		if regression := rd.checkMetricRegression(current.ID, baseline.ID, clientName, "",
			"throughput", baselineMetrics.Latency.Throughput, currentMetrics.Latency.Throughput); regression != nil {
			regressions = append(regressions, regression)
		}

		// Compare method-level metrics
		for methodName, currentMethodMetrics := range currentMetrics.Methods {
			if baselineMethodMetrics, exists := baselineMetrics.Methods[methodName]; exists {
				if regression := rd.checkMetricRegression(current.ID, baseline.ID, clientName, methodName,
					"method_avg_latency", baselineMethodMetrics.Avg, currentMethodMetrics.Avg); regression != nil {
					regressions = append(regressions, regression)
				}

				if regression := rd.checkMetricRegression(current.ID, baseline.ID, clientName, methodName,
					"method_p95_latency", baselineMethodMetrics.P95, currentMethodMetrics.P95); regression != nil {
					regressions = append(regressions, regression)
				}

				if regression := rd.checkMetricRegression(current.ID, baseline.ID, clientName, methodName,
					"method_error_rate", baselineMethodMetrics.ErrorRate, currentMethodMetrics.ErrorRate); regression != nil {
					regressions = append(regressions, regression)
				}
			}
		}
	}

	return regressions, nil
}

func (rd *regressionDetector) checkMetricRegression(runID, baselineRunID, client, method, metric string, baselineValue, currentValue float64) *types.Regression {
	// Skip if baseline value is 0 to avoid division by zero
	if baselineValue == 0 {
		return nil
	}

	// Calculate change
	absoluteChange := currentValue - baselineValue
	percentChange := (absoluteChange / baselineValue) * 100

	// Get threshold for this metric
	threshold, exists := rd.thresholds[metric]
	if !exists {
		// Try to find by metric category
		if strings.Contains(metric, "latency") {
			threshold = rd.thresholds["latency"]
		} else if strings.Contains(metric, "error") {
			threshold = rd.thresholds["error_rate"]
		} else if strings.Contains(metric, "throughput") {
			threshold = rd.thresholds["throughput"]
		} else {
			threshold = rd.thresholds["default"]
		}
	}

	// Check if this is a regression based on direction and threshold
	isRegression := false
	changeValue := percentChange

	switch threshold.Direction {
	case "increase":
		isRegression = percentChange > 0 && math.Abs(percentChange) >= threshold.MinorThreshold
	case "decrease":
		isRegression = percentChange < 0 && math.Abs(percentChange) >= threshold.MinorThreshold
		changeValue = math.Abs(percentChange)
	case "both":
		isRegression = math.Abs(percentChange) >= threshold.MinorThreshold
		changeValue = math.Abs(percentChange)
	}

	// For error rate, use absolute change if threshold is not percentage
	if metric == "error_rate" && !threshold.IsPercentage {
		isRegression = absoluteChange > threshold.MinorThreshold
		changeValue = absoluteChange
	}

	if !isRegression {
		return nil
	}

	// Determine severity
	severity := rd.GetSeverity(metric, changeValue)

	// Create regression
	regression := &types.Regression{
		ID:             uuid.New().String(),
		RunID:          runID,
		BaselineRunID:  baselineRunID,
		Client:         client,
		Metric:         metric,
		Method:         method,
		BaselineValue:  baselineValue,
		CurrentValue:   currentValue,
		PercentChange:  percentChange,
		AbsoluteChange: absoluteChange,
		Severity:       severity,
		IsSignificant:  math.Abs(percentChange) >= threshold.MajorThreshold,
		DetectedAt:     time.Now(),
	}

	return regression
}

func (rd *regressionDetector) calculateRollingAverage(runs []*types.HistoricRun) *types.HistoricRun {
	if len(runs) == 0 {
		return nil
	}

	// Initialize averages
	avgRun := &types.HistoricRun{
		ID:                "rolling_average",
		TestName:          runs[0].TestName,
		Timestamp:         time.Now(),
		PerformanceScores: make(map[string]float64),
	}

	// Calculate averages for summary metrics
	var totalErrorRate, totalAvgLatency, totalP95Latency, totalP99Latency, totalMaxLatency float64
	var totalRequests, totalErrors int64

	for _, run := range runs {
		totalErrorRate += run.OverallErrorRate
		totalAvgLatency += run.AvgLatencyMs
		totalP95Latency += run.P95LatencyMs
		totalP99Latency += run.P99LatencyMs
		totalMaxLatency += run.MaxLatencyMs
		totalRequests += run.TotalRequests
		totalErrors += run.TotalErrors
	}

	count := float64(len(runs))
	avgRun.OverallErrorRate = totalErrorRate / count
	avgRun.AvgLatencyMs = totalAvgLatency / count
	avgRun.P95LatencyMs = totalP95Latency / count
	avgRun.P99LatencyMs = totalP99Latency / count
	avgRun.MaxLatencyMs = totalMaxLatency / count
	avgRun.TotalRequests = totalRequests / int64(len(runs))
	avgRun.TotalErrors = totalErrors / int64(len(runs))

	// Calculate performance scores average
	for _, run := range runs {
		for client, score := range run.PerformanceScores {
			avgRun.PerformanceScores[client] += score
		}
	}
	for client, totalScore := range avgRun.PerformanceScores {
		avgRun.PerformanceScores[client] = totalScore / count
	}

	// Build synthetic full results for detailed comparison
	avgRun.FullResults = rd.buildSyntheticFullResults(runs)

	return avgRun
}

func (rd *regressionDetector) buildSyntheticFullResults(runs []*types.HistoricRun) json.RawMessage {
	// Create a synthetic BenchmarkResult by averaging all runs
	syntheticResult := types.BenchmarkResult{
		ClientMetrics: make(map[string]*types.ClientMetrics),
	}

	// Aggregate all client metrics
	clientMetricsSum := make(map[string]*types.ClientMetrics)
	clientCounts := make(map[string]int)

	for _, run := range runs {
		var result types.BenchmarkResult
		if err := json.Unmarshal(run.FullResults, &result); err != nil {
			continue
		}

		for clientName, metrics := range result.ClientMetrics {
			if clientMetricsSum[clientName] == nil {
				clientMetricsSum[clientName] = &types.ClientMetrics{
					Name:    clientName,
					Methods: make(map[string]types.MetricSummary),
				}
			}

			// Sum metrics
			clientMetricsSum[clientName].TotalRequests += metrics.TotalRequests
			clientMetricsSum[clientName].TotalErrors += metrics.TotalErrors
			clientMetricsSum[clientName].ErrorRate += metrics.ErrorRate
			clientMetricsSum[clientName].Latency.Avg += metrics.Latency.Avg
			clientMetricsSum[clientName].Latency.P50 += metrics.Latency.P50
			clientMetricsSum[clientName].Latency.P95 += metrics.Latency.P95
			clientMetricsSum[clientName].Latency.P99 += metrics.Latency.P99
			clientMetricsSum[clientName].Latency.Max += metrics.Latency.Max
			clientMetricsSum[clientName].Latency.Throughput += metrics.Latency.Throughput

			// Sum method metrics
			for methodName, methodMetrics := range metrics.Methods {
				if existing, exists := clientMetricsSum[clientName].Methods[methodName]; exists {
					existing.Count += methodMetrics.Count
					existing.ErrorRate += methodMetrics.ErrorRate
					existing.Avg += methodMetrics.Avg
					existing.P50 += methodMetrics.P50
					existing.P95 += methodMetrics.P95
					existing.P99 += methodMetrics.P99
					existing.Max += methodMetrics.Max
					existing.Throughput += methodMetrics.Throughput
				} else {
					clientMetricsSum[clientName].Methods[methodName] = methodMetrics
				}
			}

			clientCounts[clientName]++
		}
	}

	// Calculate averages
	for clientName, sumMetrics := range clientMetricsSum {
		count := float64(clientCounts[clientName])
		avgMetrics := &types.ClientMetrics{
			Name:          clientName,
			TotalRequests: sumMetrics.TotalRequests / int64(count),
			TotalErrors:   sumMetrics.TotalErrors / int64(count),
			ErrorRate:     sumMetrics.ErrorRate / count,
			Methods:       make(map[string]types.MetricSummary),
		}

		avgMetrics.Latency.Avg = sumMetrics.Latency.Avg / count
		avgMetrics.Latency.P50 = sumMetrics.Latency.P50 / count
		avgMetrics.Latency.P95 = sumMetrics.Latency.P95 / count
		avgMetrics.Latency.P99 = sumMetrics.Latency.P99 / count
		avgMetrics.Latency.Max = sumMetrics.Latency.Max / count
		avgMetrics.Latency.Throughput = sumMetrics.Latency.Throughput / count

		// Average method metrics
		for methodName, sumMethodMetrics := range sumMetrics.Methods {
			avgMethodMetrics := sumMethodMetrics
			avgMethodMetrics.Count = sumMethodMetrics.Count / int64(count)
			avgMethodMetrics.ErrorRate = sumMethodMetrics.ErrorRate / count
			avgMethodMetrics.Avg = sumMethodMetrics.Avg / count
			avgMethodMetrics.P50 = sumMethodMetrics.P50 / count
			avgMethodMetrics.P95 = sumMethodMetrics.P95 / count
			avgMethodMetrics.P99 = sumMethodMetrics.P99 / count
			avgMethodMetrics.Max = sumMethodMetrics.Max / count
			avgMethodMetrics.Throughput = sumMethodMetrics.Throughput / count

			avgMetrics.Methods[methodName] = avgMethodMetrics
		}

		syntheticResult.ClientMetrics[clientName] = avgMetrics
	}

	// Marshal to JSON
	data, err := json.Marshal(syntheticResult)
	if err != nil {
		return json.RawMessage("{}")
	}

	return data
}

// Additional helper methods for comprehensive analysis

func (rd *regressionDetector) applyCustomThresholds(regressions []*types.Regression, customThresholds map[string]RegressionThreshold) []*types.Regression {
	// Re-evaluate regressions with custom thresholds
	filtered := make([]*types.Regression, 0, len(regressions))

	for _, regression := range regressions {
		threshold, exists := customThresholds[regression.Metric]
		if !exists {
			// Use default if custom threshold not specified
			filtered = append(filtered, regression)
			continue
		}

		// Re-evaluate severity with custom threshold
		severity := rd.GetSeverityWithThreshold(regression.Metric, regression.PercentChange, threshold)
		if severity != "low" {
			regression.Severity = severity
			filtered = append(filtered, regression)
		}
	}

	return filtered
}

func (rd *regressionDetector) GetSeverityWithThreshold(metric string, percentChange float64, threshold RegressionThreshold) string {
	absChange := math.Abs(percentChange)

	if absChange >= threshold.CriticalThreshold {
		return "critical"
	} else if absChange >= threshold.MajorThreshold {
		return "major"
	} else if absChange >= threshold.MinorThreshold {
		return "minor"
	}
	return "low"
}

func (rd *regressionDetector) filterRegressions(regressions []*types.Regression, options DetectionOptions) []*types.Regression {
	filtered := make([]*types.Regression, 0, len(regressions))

	for _, regression := range regressions {
		// Check client includes/excludes
		if len(options.IncludeClients) > 0 {
			found := false
			for _, client := range options.IncludeClients {
				if regression.Client == client {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		if len(options.ExcludeClients) > 0 {
			exclude := false
			for _, client := range options.ExcludeClients {
				if regression.Client == client {
					exclude = true
					break
				}
			}
			if exclude {
				continue
			}
		}

		// Check method includes/excludes
		if len(options.IncludeMethods) > 0 && regression.Method != "" {
			found := false
			for _, method := range options.IncludeMethods {
				if regression.Method == method {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		if len(options.ExcludeMethods) > 0 && regression.Method != "" {
			exclude := false
			for _, method := range options.ExcludeMethods {
				if regression.Method == method {
					exclude = true
					break
				}
			}
			if exclude {
				continue
			}
		}

		filtered = append(filtered, regression)
	}

	return filtered
}

func (rd *regressionDetector) extractImprovements(regressions []*types.Regression, options DetectionOptions) []*types.Improvement {
	var improvements []*types.Improvement

	for _, regression := range regressions {
		// Check if this is actually an improvement (negative change for latency, positive for throughput)
		isImprovement := false

		switch {
		case strings.Contains(regression.Metric, "latency") && regression.PercentChange < 0:
			isImprovement = true
		case strings.Contains(regression.Metric, "error") && regression.PercentChange < 0:
			isImprovement = true
		case strings.Contains(regression.Metric, "throughput") && regression.PercentChange > 0:
			isImprovement = true
		}

		if isImprovement {
			improvement := &types.Improvement{
				Client:         regression.Client,
				Metric:         regression.Metric,
				Method:         regression.Method,
				BaselineValue:  regression.BaselineValue,
				CurrentValue:   regression.CurrentValue,
				PercentChange:  math.Abs(regression.PercentChange),
				AbsoluteChange: math.Abs(regression.AbsoluteChange),
			}
			improvements = append(improvements, improvement)
		}
	}

	return improvements
}

func (rd *regressionDetector) removeImprovements(regressions []*types.Regression) []*types.Regression {
	filtered := make([]*types.Regression, 0, len(regressions))

	for _, regression := range regressions {
		// Keep only actual regressions (not improvements)
		isActualRegression := true

		switch {
		case strings.Contains(regression.Metric, "latency") && regression.PercentChange < 0:
			isActualRegression = false
		case strings.Contains(regression.Metric, "error") && regression.PercentChange < 0:
			isActualRegression = false
		case strings.Contains(regression.Metric, "throughput") && regression.PercentChange > 0:
			isActualRegression = false
		}

		if isActualRegression {
			filtered = append(filtered, regression)
		}
	}

	return filtered
}

func (rd *regressionDetector) performStatisticalAnalysis(ctx context.Context, regressions []*types.Regression, run *types.HistoricRun, options DetectionOptions) {
	// For now, this is a placeholder for statistical analysis implementation
	// In a full implementation, you would:
	// 1. Perform statistical tests (t-test, Mann-Whitney U, etc.)
	// 2. Calculate effect sizes
	// 3. Perform power analysis
	// 4. Apply multiple comparison corrections

	rd.log.Info("Statistical analysis would be performed here")
}

func (rd *regressionDetector) buildClientAnalysis(ctx context.Context, report *RegressionReport, run *types.HistoricRun, regressions []*types.Regression, improvements []*types.Improvement, options DetectionOptions) {
	// Parse full results
	var fullResult types.BenchmarkResult
	if err := json.Unmarshal(run.FullResults, &fullResult); err != nil {
		rd.log.WithError(err).Warn("Failed to parse full results for client analysis")
		return
	}

	// Build analysis for each client
	for clientName := range fullResult.ClientMetrics {
		analysis := &ClientAnalysis{
			ClientName:    clientName,
			MetricChanges: make(map[string]*MetricChange),
			Regressions:   make([]*types.Regression, 0),
			Improvements:  make([]*types.Improvement, 0),
		}

		// Collect regressions for this client
		for _, regression := range regressions {
			if regression.Client == clientName {
				analysis.Regressions = append(analysis.Regressions, regression)
				analysis.RegressionCount++
			}
		}

		// Collect improvements for this client
		for _, improvement := range improvements {
			if improvement.Client == clientName {
				analysis.Improvements = append(analysis.Improvements, improvement)
				analysis.ImprovementCount++
			}
		}

		// Calculate health score
		analysis.HealthScore = rd.calculateClientHealthScore(analysis)

		// Determine overall status
		analysis.OverallStatus = rd.determineClientStatus(analysis)

		// Determine risk level
		analysis.RiskLevel = rd.determineClientRiskLevel(analysis)

		// Generate recommendations
		analysis.Recommendations = rd.generateClientRecommendations(analysis)

		report.ClientAnalysis[clientName] = analysis
	}
}

func (rd *regressionDetector) buildMethodAnalysis(ctx context.Context, report *RegressionReport, run *types.HistoricRun, regressions []*types.Regression, improvements []*types.Improvement, options DetectionOptions) {
	// Build method-level analysis if needed
	methodMap := make(map[string]*MethodAnalysis)

	for _, regression := range regressions {
		if regression.Method == "" {
			continue
		}

		if methodMap[regression.Method] == nil {
			methodMap[regression.Method] = &MethodAnalysis{
				MethodName:    regression.Method,
				ClientResults: make(map[string]*MetricChange),
			}
		}

		methodMap[regression.Method].RegressionCount++
	}

	for _, improvement := range improvements {
		if improvement.Method == "" {
			continue
		}

		if methodMap[improvement.Method] == nil {
			methodMap[improvement.Method] = &MethodAnalysis{
				MethodName:    improvement.Method,
				ClientResults: make(map[string]*MetricChange),
			}
		}

		methodMap[improvement.Method].ImprovementCount++
	}

	// Calculate health scores and trends
	for _, analysis := range methodMap {
		analysis.HealthScore = rd.calculateMethodHealthScore(analysis)
		analysis.OverallTrend = rd.determineMethodTrend(analysis)
		analysis.Recommendations = rd.generateMethodRecommendations(analysis)
	}

	report.MethodAnalysis = methodMap
}

func (rd *regressionDetector) generateSummary(regressions []*types.Regression, improvements []*types.Improvement, clientAnalysis map[string]*ClientAnalysis) RegressionSummary {
	summary := RegressionSummary{
		TotalRegressions:  len(regressions),
		TotalImprovements: len(improvements),
	}

	// Count by severity
	for _, regression := range regressions {
		switch regression.Severity {
		case "critical":
			summary.CriticalRegressions++
		case "major":
			summary.MajorRegressions++
		case "minor":
			summary.MinorRegressions++
		}
	}

	// Calculate overall health score
	summary.OverallHealthScore = rd.calculateOverallHealthScore(regressions, improvements, clientAnalysis)

	// Calculate performance and regression scores
	summary.PerformanceScore = rd.calculatePerformanceScoreFromSummary(clientAnalysis)
	summary.RegressionScore = rd.calculateRegressionScore(regressions)

	// Count affected clients and methods
	clientsSet := make(map[string]bool)
	methodsSet := make(map[string]bool)
	for _, regression := range regressions {
		clientsSet[regression.Client] = true
		if regression.Method != "" {
			methodsSet[regression.Method] = true
		}
	}
	summary.ClientsAffected = len(clientsSet)
	summary.MethodsAffected = len(methodsSet)

	// Find most affected client and method
	summary.MostAffectedClient = rd.findMostAffectedClient(regressions)
	summary.MostAffectedMethod = rd.findMostAffectedMethod(regressions)

	// Determine recommended action
	summary.RecommendedAction = rd.determineRecommendedAction(summary)

	return summary
}

func (rd *regressionDetector) assessRisk(regressions []*types.Regression, improvements []*types.Improvement, clientAnalysis map[string]*ClientAnalysis) RiskAssessment {
	assessment := RiskAssessment{
		ClientRiskScores: make(map[string]float64),
		MethodRiskScores: make(map[string]float64),
	}

	// Calculate overall risk score
	riskScore := 0.0
	if len(regressions) > 0 {
		criticalWeight := 40.0
		majorWeight := 20.0
		minorWeight := 5.0

		for _, regression := range regressions {
			switch regression.Severity {
			case "critical":
				riskScore += criticalWeight
			case "major":
				riskScore += majorWeight
			case "minor":
				riskScore += minorWeight
			}
		}
	}

	// Cap at 100
	if riskScore > 100 {
		riskScore = 100
	}

	assessment.RiskScore = riskScore

	// Determine overall risk level
	if riskScore >= 80 {
		assessment.OverallRisk = "critical"
		assessment.MonitoringPriority = "immediate"
	} else if riskScore >= 60 {
		assessment.OverallRisk = "high"
		assessment.MonitoringPriority = "high"
	} else if riskScore >= 30 {
		assessment.OverallRisk = "medium"
		assessment.MonitoringPriority = "normal"
	} else {
		assessment.OverallRisk = "low"
		assessment.MonitoringPriority = "low"
	}

	// Generate risk factors
	assessment.RiskFactors = rd.identifyRiskFactors(regressions, clientAnalysis)

	// Generate impact assessment
	assessment.ImpactAssessment = rd.generateImpactAssessment(regressions, assessment.OverallRisk)

	// Generate mitigation suggestions
	assessment.MitigationSuggestions = rd.generateMitigationSuggestions(regressions, assessment.OverallRisk)

	// Calculate client risk scores
	for clientName, analysis := range clientAnalysis {
		assessment.ClientRiskScores[clientName] = rd.calculateClientRiskScore(analysis)
	}

	return assessment
}

func (rd *regressionDetector) generateRecommendations(report *RegressionReport) []string {
	var recommendations []string

	// Based on risk level
	switch report.RiskAssessment.OverallRisk {
	case "critical":
		recommendations = append(recommendations, "IMMEDIATE ACTION REQUIRED: Critical performance regressions detected")
		recommendations = append(recommendations, "Consider rolling back recent changes or implementing hotfixes")
	case "high":
		recommendations = append(recommendations, "Investigate performance regressions urgently")
		recommendations = append(recommendations, "Review recent code changes and deployments")
	case "medium":
		recommendations = append(recommendations, "Monitor performance trends closely")
		recommendations = append(recommendations, "Schedule performance investigation")
	}

	// Based on regression count
	if report.Summary.TotalRegressions > 10 {
		recommendations = append(recommendations, "Multiple regressions detected - consider systematic performance review")
	}

	// Based on affected clients
	if report.Summary.ClientsAffected > 1 {
		recommendations = append(recommendations, "Multiple clients affected - investigate common factors")
	}

	// Based on most affected client
	if report.Summary.MostAffectedClient != "" {
		recommendations = append(recommendations, fmt.Sprintf("Focus investigation on %s client", report.Summary.MostAffectedClient))
	}

	return recommendations
}

func (rd *regressionDetector) generateStatisticalInfo(regressions []*types.Regression, options DetectionOptions) *StatisticalInfo {
	info := &StatisticalInfo{
		ComparisonMethod:    options.ComparisonMode,
		SampleSizes:         make(map[string]int),
		ConfidenceLevels:    make(map[string]float64),
		EffectSizes:         make(map[string]float64),
		PowerAnalysis:       make(map[string]float64),
		MultipleComparisons: len(regressions) > 1,
		OverallSignificance: 0.05, // Default significance level
	}

	if info.MultipleComparisons {
		info.CorrectionMethod = "bonferroni"
	}

	return info
}

// Helper functions for analysis calculations

func (rd *regressionDetector) calculateHealthScore(result *types.BenchmarkResult) float64 {
	score := 100.0

	// Penalize for high error rates
	for _, metrics := range result.ClientMetrics {
		if metrics.ErrorRate > 0.1 { // 10%
			score -= 30
		} else if metrics.ErrorRate > 0.05 { // 5%
			score -= 15
		} else if metrics.ErrorRate > 0.01 { // 1%
			score -= 5
		}
	}

	// Ensure score doesn't go below 0
	if score < 0 {
		score = 0
	}

	return score
}

func (rd *regressionDetector) calculatePerformanceScore(result *types.BenchmarkResult) float64 {
	// Simplified performance score calculation
	totalScore := 0.0
	clientCount := 0

	for _, metrics := range result.ClientMetrics {
		// Score based on latency and error rate
		latencyScore := math.Max(0, 100-metrics.Latency.P95/10)          // Assume 1000ms is worst case
		errorScore := math.Max(0, 100-metrics.ErrorRate*1000)            // Error rate penalty
		throughputScore := math.Min(metrics.Latency.Throughput/100, 100) // Normalize throughput

		clientScore := (latencyScore*0.4 + errorScore*0.4 + throughputScore*0.2)
		totalScore += clientScore
		clientCount++
	}

	if clientCount == 0 {
		return 0
	}

	return totalScore / float64(clientCount)
}

func (rd *regressionDetector) calculateClientScore(metrics *types.ClientMetrics) float64 {
	// Similar to performance score but for individual client
	latencyScore := math.Max(0, 100-metrics.Latency.P95/10)
	errorScore := math.Max(0, 100-metrics.ErrorRate*1000)
	throughputScore := math.Min(metrics.Latency.Throughput/100, 100)

	return latencyScore*0.4 + errorScore*0.4 + throughputScore*0.2
}

func (rd *regressionDetector) detectAnomalies(ctx context.Context, result *types.BenchmarkResult, run *types.HistoricRun) []*PerformanceAnomaly {
	var anomalies []*PerformanceAnomaly

	// Simple anomaly detection based on extreme values
	for clientName, metrics := range result.ClientMetrics {
		// Check for error rate spikes
		if metrics.ErrorRate > 0.2 { // 20% error rate is anomalous
			anomaly := &PerformanceAnomaly{
				ID:            uuid.New().String(),
				Type:          "spike",
				Client:        clientName,
				Metric:        "error_rate",
				Value:         metrics.ErrorRate,
				ExpectedValue: 0.05, // Expected 5% or less
				Deviation:     metrics.ErrorRate - 0.05,
				Severity:      "high",
				Confidence:    0.95,
				DetectedAt:    time.Now(),
				Description:   fmt.Sprintf("Error rate spike in %s: %.2f%%", clientName, metrics.ErrorRate*100),
			}
			anomalies = append(anomalies, anomaly)
		}

		// Check for extreme latency
		if metrics.Latency.P95 > 5000 { // 5 second P95 is likely anomalous
			anomaly := &PerformanceAnomaly{
				ID:            uuid.New().String(),
				Type:          "spike",
				Client:        clientName,
				Metric:        "p95_latency",
				Value:         metrics.Latency.P95,
				ExpectedValue: 1000, // Expected 1 second or less
				Deviation:     metrics.Latency.P95 - 1000,
				Severity:      "medium",
				Confidence:    0.90,
				DetectedAt:    time.Now(),
				Description:   fmt.Sprintf("Latency spike in %s: %.0fms", clientName, metrics.Latency.P95),
			}
			anomalies = append(anomalies, anomaly)
		}
	}

	return anomalies
}

func (rd *regressionDetector) analyzeTrends(ctx context.Context, analysis *RunAnalysis, run *types.HistoricRun) {
	// Get historical data for trend analysis
	filter := types.TrendFilter{
		Since: time.Now().AddDate(0, 0, -30),
	}
	trends, err := rd.storage.GetHistoricTrends(ctx, filter)
	if err != nil {
		rd.log.WithError(err).Warn("Failed to get trend data")
		return
	}

	if len(trends) > 0 {
		// For now, just take the first trend data
		trend := trends[0]
		trendAnalysis := &TrendAnalysis{
			MetricName:     "avg_latency",
			Client:         "overall",
			TrendDirection: trend.Direction,
			Slope:          0.0, // Would need to calculate from trend points
			RSquared:       0.0, // Would need to calculate
			DataPoints:     len(trend.TrendPoints),
		}

		// Determine trend strength based on trend direction
		switch trend.Direction {
		case "improving":
			trendAnalysis.TrendStrength = 0.8
		case "degrading":
			trendAnalysis.TrendStrength = 0.8
		case "stable":
			trendAnalysis.TrendStrength = 0.3
		default:
			trendAnalysis.TrendStrength = 0.3
		}

		analysis.Trends["avg_latency_overall"] = trendAnalysis
	}
}

func (rd *regressionDetector) assessQuality(result *types.BenchmarkResult) *QualityMetrics {
	quality := &QualityMetrics{
		DataCompleteness: 1.0, // Assume complete data
		OverallQuality:   0.8, // Default good quality
	}

	// Check for consistency across clients
	if len(result.ClientMetrics) > 1 {
		var latencies []float64
		for _, metrics := range result.ClientMetrics {
			latencies = append(latencies, metrics.Latency.Avg)
		}

		// Calculate coefficient of variation
		mean := rd.calculateMean(latencies)
		stddev := rd.calculateStdDev(latencies, mean)
		cv := stddev / mean

		if cv < 0.1 {
			quality.ConsistencyScore = 1.0
		} else if cv < 0.3 {
			quality.ConsistencyScore = 0.7
		} else {
			quality.ConsistencyScore = 0.4
		}
	}

	// Check reliability based on error rates
	errorRateSum := 0.0
	clientCount := 0
	for _, metrics := range result.ClientMetrics {
		errorRateSum += metrics.ErrorRate
		clientCount++
	}

	avgErrorRate := errorRateSum / float64(clientCount)
	if avgErrorRate < 0.01 {
		quality.ReliabilityScore = 1.0
	} else if avgErrorRate < 0.05 {
		quality.ReliabilityScore = 0.8
	} else {
		quality.ReliabilityScore = 0.5
	}

	// Overall quality is average of sub-scores
	quality.OverallQuality = (quality.DataCompleteness + quality.ConsistencyScore + quality.ReliabilityScore) / 3.0

	return quality
}

func (rd *regressionDetector) generateRunRecommendations(analysis *RunAnalysis) []string {
	var recommendations []string

	if analysis.OverallHealthScore < 70 {
		recommendations = append(recommendations, "Health score below 70 - investigate performance issues")
	}

	if len(analysis.Anomalies) > 0 {
		recommendations = append(recommendations, "Anomalies detected - review unusual performance patterns")
	}

	if analysis.QualityMetrics.OverallQuality < 0.7 {
		recommendations = append(recommendations, "Data quality concerns - verify measurement consistency")
	}

	return recommendations
}

func (rd *regressionDetector) getComparisonHistory(ctx context.Context, runID string, limit int) []*HistoricalComparison {
	// Placeholder for historical comparison implementation
	return []*HistoricalComparison{}
}

// Additional helper methods for calculations

func (rd *regressionDetector) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (rd *regressionDetector) calculateStdDev(values []float64, mean float64) float64 {
	if len(values) <= 1 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		diff := v - mean
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(len(values)-1))
}

func (rd *regressionDetector) calculateClientHealthScore(analysis *ClientAnalysis) float64 {
	score := 100.0

	// Penalize for regressions
	score -= float64(analysis.RegressionCount) * 10

	// Bonus for improvements
	score += float64(analysis.ImprovementCount) * 5

	// Cap at 0-100 range
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

func (rd *regressionDetector) determineClientStatus(analysis *ClientAnalysis) string {
	if analysis.RegressionCount > analysis.ImprovementCount {
		return "degraded"
	} else if analysis.ImprovementCount > analysis.RegressionCount {
		return "improved"
	}
	return "stable"
}

func (rd *regressionDetector) determineClientRiskLevel(analysis *ClientAnalysis) string {
	if analysis.RegressionCount >= 3 {
		return "critical"
	} else if analysis.RegressionCount >= 2 {
		return "high"
	} else if analysis.RegressionCount >= 1 {
		return "medium"
	}
	return "low"
}

func (rd *regressionDetector) generateClientRecommendations(analysis *ClientAnalysis) []string {
	var recommendations []string

	if analysis.RegressionCount > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Investigate %d regression(s) in %s", analysis.RegressionCount, analysis.ClientName))
	}

	if analysis.ImprovementCount > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Good performance improvements in %s", analysis.ClientName))
	}

	return recommendations
}

func (rd *regressionDetector) calculateMethodHealthScore(analysis *MethodAnalysis) float64 {
	score := 100.0
	score -= float64(analysis.RegressionCount) * 15
	score += float64(analysis.ImprovementCount) * 7

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

func (rd *regressionDetector) determineMethodTrend(analysis *MethodAnalysis) string {
	if analysis.RegressionCount > analysis.ImprovementCount {
		return "degrading"
	} else if analysis.ImprovementCount > analysis.RegressionCount {
		return "improving"
	}
	return "stable"
}

func (rd *regressionDetector) generateMethodRecommendations(analysis *MethodAnalysis) []string {
	var recommendations []string

	if analysis.RegressionCount > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Method %s shows performance degradation", analysis.MethodName))
	}

	return recommendations
}

func (rd *regressionDetector) calculateOverallHealthScore(regressions []*types.Regression, improvements []*types.Improvement, clientAnalysis map[string]*ClientAnalysis) float64 {
	score := 100.0

	// Penalize for regressions
	for _, regression := range regressions {
		switch regression.Severity {
		case "critical":
			score -= 20
		case "major":
			score -= 10
		case "minor":
			score -= 3
		}
	}

	// Bonus for improvements
	score += float64(len(improvements)) * 2

	// Cap at 0-100
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

func (rd *regressionDetector) calculatePerformanceScoreFromSummary(clientAnalysis map[string]*ClientAnalysis) float64 {
	if len(clientAnalysis) == 0 {
		return 0
	}

	totalScore := 0.0
	for _, analysis := range clientAnalysis {
		totalScore += analysis.HealthScore
	}

	return totalScore / float64(len(clientAnalysis))
}

func (rd *regressionDetector) calculateRegressionScore(regressions []*types.Regression) float64 {
	score := 0.0

	for _, regression := range regressions {
		switch regression.Severity {
		case "critical":
			score += 10
		case "major":
			score += 5
		case "minor":
			score += 1
		}
	}

	return score
}

func (rd *regressionDetector) findMostAffectedClient(regressions []*types.Regression) string {
	clientCounts := make(map[string]int)

	for _, regression := range regressions {
		clientCounts[regression.Client]++
	}

	maxCount := 0
	mostAffected := ""
	for client, count := range clientCounts {
		if count > maxCount {
			maxCount = count
			mostAffected = client
		}
	}

	return mostAffected
}

func (rd *regressionDetector) findMostAffectedMethod(regressions []*types.Regression) string {
	methodCounts := make(map[string]int)

	for _, regression := range regressions {
		if regression.Method != "" {
			methodCounts[regression.Method]++
		}
	}

	maxCount := 0
	mostAffected := ""
	for method, count := range methodCounts {
		if count > maxCount {
			maxCount = count
			mostAffected = method
		}
	}

	return mostAffected
}

func (rd *regressionDetector) determineRecommendedAction(summary RegressionSummary) string {
	if summary.CriticalRegressions > 0 || summary.OverallHealthScore < 50 {
		return "investigate"
	} else if summary.MajorRegressions > 0 || summary.OverallHealthScore < 80 {
		return "monitor"
	}
	return "none"
}

func (rd *regressionDetector) identifyRiskFactors(regressions []*types.Regression, clientAnalysis map[string]*ClientAnalysis) []string {
	var factors []string

	if len(regressions) > 5 {
		factors = append(factors, "Multiple regressions detected")
	}

	criticalCount := 0
	for _, regression := range regressions {
		if regression.Severity == "critical" {
			criticalCount++
		}
	}
	if criticalCount > 0 {
		factors = append(factors, fmt.Sprintf("%d critical regression(s)", criticalCount))
	}

	return factors
}

func (rd *regressionDetector) generateImpactAssessment(regressions []*types.Regression, riskLevel string) string {
	switch riskLevel {
	case "critical":
		return "Critical impact on system performance - immediate action required"
	case "high":
		return "High impact on system performance - urgent attention needed"
	case "medium":
		return "Moderate impact on system performance - should be addressed soon"
	default:
		return "Low impact on system performance - monitor and address as needed"
	}
}

func (rd *regressionDetector) generateMitigationSuggestions(regressions []*types.Regression, riskLevel string) []string {
	var suggestions []string

	switch riskLevel {
	case "critical":
		suggestions = append(suggestions, "Consider immediate rollback or hotfix")
		suggestions = append(suggestions, "Implement performance circuit breakers")
		suggestions = append(suggestions, "Scale up resources if needed")
	case "high":
		suggestions = append(suggestions, "Review recent deployments and changes")
		suggestions = append(suggestions, "Implement additional monitoring")
		suggestions = append(suggestions, "Prepare rollback plan")
	case "medium":
		suggestions = append(suggestions, "Schedule performance review")
		suggestions = append(suggestions, "Increase monitoring frequency")
	default:
		suggestions = append(suggestions, "Continue routine monitoring")
	}

	return suggestions
}

func (rd *regressionDetector) calculateClientRiskScore(analysis *ClientAnalysis) float64 {
	score := 0.0

	for _, regression := range analysis.Regressions {
		switch regression.Severity {
		case "critical":
			score += 30
		case "major":
			score += 15
		case "minor":
			score += 5
		}
	}

	if score > 100 {
		score = 100
	}

	return score
}
