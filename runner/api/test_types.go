package api

import (
	"time"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/types"
)

// Test types to support the comprehensive API tests
// These types are used by the test files and may not exist in the actual implementation

// Analysis types that may be missing

type TrendAnalysis struct {
	TestName string                          `json:"test_name"`
	Days     int                             `json:"days"`
	Trends   map[string]*types.HistoricTrend `json:"trends"`
}

type MethodTrends struct {
	TestName string                          `json:"test_name"`
	Method   string                          `json:"method"`
	Days     int                             `json:"days"`
	Trends   map[string]*types.HistoricTrend `json:"trends"`
}

type ClientTrends struct {
	TestName string                          `json:"test_name"`
	Client   string                          `json:"client"`
	Days     int                             `json:"days"`
	Trends   map[string]*types.HistoricTrend `json:"trends"`
}

type MovingAverage struct {
	TestName   string               `json:"test_name"`
	Metric     string               `json:"metric"`
	WindowSize int                  `json:"window_size"`
	Days       int                  `json:"days"`
	DataPoints []MovingAveragePoint `json:"data_points"`
}

type MovingAveragePoint struct {
	Timestamp     time.Time `json:"timestamp"`
	Value         float64   `json:"value"`
	MovingAverage float64   `json:"moving_average"`
}

type TrendForecast struct {
	TestName     string          `json:"test_name"`
	Metric       string          `json:"metric"`
	HistoryDays  int             `json:"history_days"`
	ForecastDays int             `json:"forecast_days"`
	Forecast     []ForecastPoint `json:"forecast"`
	Confidence   float64         `json:"confidence"`
	Trend        string          `json:"trend"`
}

type ForecastPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
	Lower     float64   `json:"lower_bound"`
	Upper     float64   `json:"upper_bound"`
}

type RegressionReport struct {
	RunID       string                    `json:"run_id"`
	TestName    string                    `json:"test_name"`
	Timestamp   time.Time                 `json:"timestamp"`
	Options     analysis.DetectionOptions `json:"options"`
	Regressions []*types.Regression       `json:"regressions"`
	Summary     string                    `json:"summary"`
	Statistics  ReportStatistics          `json:"statistics"`
}

type ReportStatistics struct {
	TotalRegressions    int      `json:"total_regressions"`
	CriticalRegressions int      `json:"critical_regressions"`
	MajorRegressions    int      `json:"major_regressions"`
	MinorRegressions    int      `json:"minor_regressions"`
	AverageChange       float64  `json:"average_change"`
	MaxChange           float64  `json:"max_change"`
	AffectedClients     []string `json:"affected_clients"`
	AffectedMethods     []string `json:"affected_methods"`
}

type RunAnalysis struct {
	RunID             string                    `json:"run_id"`
	TestName          string                    `json:"test_name"`
	AnalysisTimestamp time.Time                 `json:"analysis_timestamp"`
	PerformanceScore  float64                   `json:"performance_score"`
	RegressionCount   int                       `json:"regression_count"`
	ImprovementCount  int                       `json:"improvement_count"`
	ClientAnalysis    map[string]ClientAnalysis `json:"client_analysis"`
	MethodAnalysis    map[string]MethodAnalysis `json:"method_analysis"`
	Recommendations   []string                  `json:"recommendations"`
	TrendDirection    string                    `json:"trend_direction"`
	Confidence        float64                   `json:"confidence"`
}

type ClientAnalysis struct {
	Client           string   `json:"client"`
	PerformanceScore float64  `json:"performance_score"`
	ErrorRate        float64  `json:"error_rate"`
	AvgLatency       float64  `json:"avg_latency"`
	P95Latency       float64  `json:"p95_latency"`
	Stability        float64  `json:"stability"`
	Trend            string   `json:"trend"`
	Issues           []string `json:"issues"`
}

type MethodAnalysis struct {
	Method     string   `json:"method"`
	CallCount  int64    `json:"call_count"`
	ErrorRate  float64  `json:"error_rate"`
	AvgLatency float64  `json:"avg_latency"`
	P95Latency float64  `json:"p95_latency"`
	Trend      string   `json:"trend"`
	Issues     []string `json:"issues"`
}

// Baseline types

type Baseline struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	TestName    string          `json:"test_name"`
	RunID       string          `json:"run_id"`
	GitBranch   string          `json:"git_branch"`
	GitCommit   string          `json:"git_commit,omitempty"`
	CreatedBy   string          `json:"created_by,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	IsActive    bool            `json:"is_active"`
	Metrics     BaselineMetrics `json:"metrics"`
}

type BaselineMetrics struct {
	OverallErrorRate float64                   `json:"overall_error_rate"`
	AvgLatencyMs     float64                   `json:"avg_latency_ms"`
	P95LatencyMs     float64                   `json:"p95_latency_ms"`
	P99LatencyMs     float64                   `json:"p99_latency_ms"`
	TotalRequests    int64                     `json:"total_requests"`
	ClientMetrics    map[string]ClientBaseline `json:"client_metrics"`
	MethodMetrics    map[string]MethodBaseline `json:"method_metrics"`
}

type ClientBaseline struct {
	ErrorRate     float64 `json:"error_rate"`
	AvgLatency    float64 `json:"avg_latency"`
	P95Latency    float64 `json:"p95_latency"`
	P99Latency    float64 `json:"p99_latency"`
	TotalRequests int64   `json:"total_requests"`
}

type MethodBaseline struct {
	ErrorRate  float64 `json:"error_rate"`
	AvgLatency float64 `json:"avg_latency"`
	P95Latency float64 `json:"p95_latency"`
	CallCount  int64   `json:"call_count"`
}

type BaselineComparison struct {
	BaselineName  string                  `json:"baseline_name"`
	RunID         string                  `json:"run_id"`
	TestName      string                  `json:"test_name"`
	ComparedAt    time.Time               `json:"compared_at"`
	Summary       string                  `json:"summary"`
	OverallChange float64                 `json:"overall_change"`
	ClientChanges map[string]ClientChange `json:"client_changes"`
	MethodChanges map[string]MethodChange `json:"method_changes"`
	Verdict       string                  `json:"verdict"`
	Confidence    float64                 `json:"confidence"`
}

type ClientChange struct {
	Client           string  `json:"client"`
	ErrorRateChange  float64 `json:"error_rate_change"`
	LatencyChange    float64 `json:"latency_change"`
	P95LatencyChange float64 `json:"p95_latency_change"`
	Status           string  `json:"status"`
}

type MethodChange struct {
	Method          string  `json:"method"`
	ErrorRateChange float64 `json:"error_rate_change"`
	LatencyChange   float64 `json:"latency_change"`
	CallCountChange int64   `json:"call_count_change"`
	Status          string  `json:"status"`
}

// DetectionOptions for regression analysis
type DetectionOptions = analysis.DetectionOptions

// RegressionThreshold for setting thresholds
type RegressionThreshold = analysis.RegressionThreshold
