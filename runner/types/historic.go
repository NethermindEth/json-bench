package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// HistoricRun represents a benchmark run stored in the database
type HistoricRun struct {
	ID            string    `json:"id" db:"id"`
	Timestamp     time.Time `json:"timestamp" db:"timestamp"`
	GitCommit     string    `json:"git_commit" db:"git_commit"`
	GitBranch     string    `json:"git_branch" db:"git_branch"`
	TestName      string    `json:"test_name" db:"test_name"`
	Description   string    `json:"description" db:"description"`
	ConfigHash    string    `json:"config_hash" db:"config_hash"`
	ResultPath    string    `json:"result_path" db:"result_path"`
	Duration      string    `json:"duration" db:"duration"`
	TotalRequests int64     `json:"total_requests" db:"total_requests"`
	SuccessRate   float64   `json:"success_rate" db:"success_rate"`
	AvgLatency    float64   `json:"avg_latency" db:"avg_latency"`
	P95Latency    float64   `json:"p95_latency" db:"p95_latency"`
	Clients       []string  `json:"clients" db:"clients"`
	Methods       []string  `json:"methods" db:"methods"`
	Tags          []string  `json:"tags" db:"tags"`
	IsBaseline    bool      `json:"is_baseline" db:"is_baseline"`
	BaselineName  string    `json:"baseline_name,omitempty" db:"baseline_name"`

	// Additional fields for baseline analysis compatibility
	OverallErrorRate  float64            `json:"overall_error_rate" db:"overall_error_rate"`
	AvgLatencyMs      float64            `json:"avg_latency_ms" db:"avg_latency_ms"`
	P95LatencyMs      float64            `json:"p95_latency_ms" db:"p95_latency_ms"`
	P99LatencyMs      float64            `json:"p99_latency_ms" db:"p99_latency_ms"`
	MaxLatencyMs      float64            `json:"max_latency_ms" db:"max_latency_ms"`
	TotalErrors       int64              `json:"total_errors" db:"total_errors"`
	PerformanceScores map[string]float64 `json:"performance_scores" db:"performance_scores"`
	FullResults       json.RawMessage    `json:"full_results" db:"full_results"`

	// Additional fields for API compatibility
	BestClient     string `json:"best_client,omitempty"`
	ClientsCount   int    `json:"clients_count"`
	EndpointsCount int    `json:"endpoints_count"`
	TargetRPS      int    `json:"target_rps"`
}

// Regression represents a performance regression detected between runs
type Regression struct {
	ID             string     `json:"id"`
	RunID          string     `json:"run_id"`
	BaselineRunID  string     `json:"baseline_run_id"`
	Client         string     `json:"client"`
	Metric         string     `json:"metric"`
	Method         string     `json:"method"`
	BaselineValue  float64    `json:"baseline_value"`
	CurrentValue   float64    `json:"current_value"`
	PercentChange  float64    `json:"percent_change"`
	AbsoluteChange float64    `json:"absolute_change"`
	Severity       string     `json:"severity"` // low, medium, high, critical
	IsSignificant  bool       `json:"is_significant"`
	DetectedAt     time.Time  `json:"detected_at"`
	PValue         float64    `json:"p_value"`
	Notes          string     `json:"notes"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty"`
}

// Improvement represents a performance improvement detected between runs
type Improvement struct {
	ID             string    `json:"id"`
	RunID          string    `json:"run_id"`
	BaselineRunID  string    `json:"baseline_run_id"`
	Client         string    `json:"client"`
	Metric         string    `json:"metric"`
	Method         string    `json:"method"`
	BaselineValue  float64   `json:"baseline_value"`
	CurrentValue   float64   `json:"current_value"`
	PercentChange  float64   `json:"percent_change"`
	AbsoluteChange float64   `json:"absolute_change"`
	Significance   string    `json:"significance"` // minor, major, significant
	DetectedAt     time.Time `json:"detected_at"`
}

// BaselineComparison represents a comparison between current run and baseline
type BaselineComparison struct {
	BaselineRun  *HistoricRun     `json:"baseline_run"`
	CurrentRun   *BenchmarkResult `json:"current_run"`
	Regressions  []Regression     `json:"regressions"`
	Improvements []Improvement    `json:"improvements"`
	Summary      string           `json:"summary"`
}

// TrendPoint represents a single point in a performance trend
type TrendPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
	RunID     string    `json:"run_id"`
	GitCommit string    `json:"git_commit,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
}

// RegressionReport represents a comprehensive regression analysis
type RegressionReport struct {
	RunID       string       `json:"run_id"`
	Regressions []Regression `json:"regressions"`
	HasCritical bool         `json:"has_critical"`
	Summary     string       `json:"summary"`
}

// RunFilter represents filtering criteria for historic runs
type RunFilter struct {
	GitBranch  string    `json:"git_branch,omitempty"`
	TestName   string    `json:"test_name,omitempty"`
	Client     string    `json:"client,omitempty"`
	Method     string    `json:"method,omitempty"`
	IsBaseline *bool     `json:"is_baseline,omitempty"`
	Since      time.Time `json:"since,omitempty"`
	Until      time.Time `json:"until,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	Limit      int       `json:"limit,omitempty"`
	Offset     int       `json:"offset,omitempty"`
}

// TrendFilter represents filtering criteria for trend analysis
type TrendFilter struct {
	Client    string    `json:"client,omitempty"`
	Method    string    `json:"method,omitempty"`
	GitBranch string    `json:"git_branch,omitempty"`
	Since     time.Time `json:"since,omitempty"`
	Until     time.Time `json:"until,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	Interval  string    `json:"interval,omitempty"` // hour, day, week, month
}

// MetricQuery represents a query for time-series metrics
type MetricQuery struct {
	MetricNames []string  `json:"metric_names"`
	Client      string    `json:"client,omitempty"`
	Method      string    `json:"method,omitempty"`
	RunID       string    `json:"run_id,omitempty"`
	Since       time.Time `json:"since,omitempty"`
	Until       time.Time `json:"until,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Limit       int       `json:"limit,omitempty"`
	Offset      int       `json:"offset,omitempty"`
}

// HistoricTrend represents a historic trend data point
type HistoricTrend struct {
	Timestamp     time.Time `json:"timestamp"`
	MetricName    string    `json:"metric_name"`
	Value         float64   `json:"value"`
	TrendValue    float64   `json:"trend_value"`
	MovingAverage float64   `json:"moving_average"`
}

// HistoricComparison represents a comparison between historic runs
type HistoricComparison struct {
	Run1        *HistoricRun `json:"run1"`
	Run2        *HistoricRun `json:"run2"`
	Differences []string     `json:"differences"`
	Summary     string       `json:"summary"`
}

// HistoricSummary represents a summary of historic data
type HistoricSummary struct {
	TotalRuns    int            `json:"total_runs"`
	TestName     string         `json:"test_name"`
	FirstRun     time.Time      `json:"first_run"`
	LastRun      time.Time      `json:"last_run"`
	RecentRuns   []*HistoricRun `json:"recent_runs"`
	BestRun      *HistoricRun   `json:"best_run"`
	WorstRun     *HistoricRun   `json:"worst_run"`
	Trends       []*TrendData   `json:"trends"`
	Regressions  []*Regression  `json:"regressions"`
	Improvements []*Improvement `json:"improvements"`
}

// TrendData represents trend analysis results
type TrendData struct {
	Period        string       `json:"period"`
	TrendPoints   []TrendPoint `json:"trend_points"`
	Direction     string       `json:"direction"` // improving, degrading, stable
	PercentChange float64      `json:"percent_change"`
	Forecast      []TrendPoint `json:"forecast,omitempty"` // optional future predictions
}

// StringSlice is a custom type for handling string slices in database
type StringSlice []string

// Value implements the driver.Valuer interface for database storage
func (s StringSlice) Value() (driver.Value, error) {
	if len(s) == 0 {
		return nil, nil
	}
	return json.Marshal(s)
}

// Scan implements the sql.Scanner interface for database retrieval
func (s *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}

	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, s)
	case string:
		return json.Unmarshal([]byte(v), s)
	default:
		return fmt.Errorf("cannot scan %T into StringSlice", value)
	}
}

// ToSlice converts StringSlice to []string
func (s StringSlice) ToSlice() []string {
	return []string(s)
}

// FromSlice creates StringSlice from []string
func FromSlice(slice []string) StringSlice {
	return StringSlice(slice)
}

// Contains checks if the slice contains the given string
func (s StringSlice) Contains(str string) bool {
	for _, item := range s {
		if item == str {
			return true
		}
	}
	return false
}

// Add adds a string to the slice if it doesn't already exist
func (s *StringSlice) Add(str string) {
	if !s.Contains(str) {
		*s = append(*s, str)
	}
}

// Remove removes a string from the slice
func (s *StringSlice) Remove(str string) {
	for i, item := range *s {
		if item == str {
			*s = append((*s)[:i], (*s)[i+1:]...)
			return
		}
	}
}
