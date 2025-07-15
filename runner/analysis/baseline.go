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

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// BaselineManager manages performance baselines for benchmark analysis
type BaselineManager interface {
	Start(ctx context.Context) error
	Stop() error

	// Baseline management
	SetBaseline(ctx context.Context, runID, name, description string) (*Baseline, error)
	GetBaseline(ctx context.Context, name string) (*Baseline, error)
	ListBaselines(ctx context.Context, testName string) ([]*Baseline, error)
	DeleteBaseline(ctx context.Context, name string) error

	// Comparison operations
	CompareToBaseline(ctx context.Context, runID, baselineName string) (*BaselineComparison, error)
	CompareToAllBaselines(ctx context.Context, runID string) ([]*BaselineComparison, error)

	// Analysis operations
	DetectRegressions(ctx context.Context, runID, baselineName string, thresholds RegressionThresholds) ([]*types.Regression, error)
	GetBaselineHistory(ctx context.Context, baselineName string, days int) ([]*BaselineHistoryPoint, error)
}

// Baseline represents a performance baseline configuration
type Baseline struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	TestName    string    `json:"test_name" db:"test_name"`
	RunID       string    `json:"run_id" db:"run_id"`
	CreatedBy   string    `json:"created_by,omitempty" db:"created_by"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`

	// Cached baseline metrics for quick comparison
	BaselineMetrics BaselineMetrics `json:"baseline_metrics" db:"baseline_metrics"`

	// Metadata
	Tags     []string `json:"tags,omitempty" db:"tags"`
	IsActive bool     `json:"is_active" db:"is_active"`
}

// BaselineMetrics contains the key performance metrics from the baseline run
type BaselineMetrics struct {
	OverallErrorRate  float64                       `json:"overall_error_rate"`
	AvgLatencyMs      float64                       `json:"avg_latency_ms"`
	P95LatencyMs      float64                       `json:"p95_latency_ms"`
	P99LatencyMs      float64                       `json:"p99_latency_ms"`
	MaxLatencyMs      float64                       `json:"max_latency_ms"`
	TotalRequests     int64                         `json:"total_requests"`
	TotalErrors       int64                         `json:"total_errors"`
	PerformanceScores map[string]float64            `json:"performance_scores"`
	ClientMetrics     map[string]ClientBaseline     `json:"client_metrics"`
	MethodMetrics     map[string]map[string]float64 `json:"method_metrics,omitempty"`
}

// ClientBaseline contains baseline metrics for a specific client
type ClientBaseline struct {
	ErrorRate     float64 `json:"error_rate"`
	AvgLatency    float64 `json:"avg_latency"`
	P95Latency    float64 `json:"p95_latency"`
	P99Latency    float64 `json:"p99_latency"`
	Throughput    float64 `json:"throughput"`
	TotalRequests int64   `json:"total_requests"`
	TotalErrors   int64   `json:"total_errors"`
}

// BaselineComparison represents a comparison between a run and a baseline
type BaselineComparison struct {
	RunID         string    `json:"run_id"`
	BaselineName  string    `json:"baseline_name"`
	BaselineRunID string    `json:"baseline_run_id"`
	ComparedAt    time.Time `json:"compared_at"`

	// Overall comparison
	OverallChange ComparisonMetric                       `json:"overall_change"`
	ClientChanges map[string]ClientComparison            `json:"client_changes"`
	MethodChanges map[string]map[string]ComparisonMetric `json:"method_changes,omitempty"`

	// Regression detection
	Regressions  []types.Regression  `json:"regressions,omitempty"`
	Improvements []types.Improvement `json:"improvements,omitempty"`

	// Summary
	Summary         string   `json:"summary"`
	Status          string   `json:"status"`     // "improved", "degraded", "stable", "mixed"
	RiskLevel       string   `json:"risk_level"` // "low", "medium", "high", "critical"
	Recommendations []string `json:"recommendations,omitempty"`
}

// ComparisonMetric represents a comparison between two metric values
type ComparisonMetric struct {
	BaselineValue   float64 `json:"baseline_value"`
	CurrentValue    float64 `json:"current_value"`
	AbsoluteChange  float64 `json:"absolute_change"`
	PercentChange   float64 `json:"percent_change"`
	IsImprovement   bool    `json:"is_improvement"`
	IsSignificant   bool    `json:"is_significant"`
	ConfidenceLevel float64 `json:"confidence_level,omitempty"`
}

// ClientComparison represents performance comparison for a specific client
type ClientComparison struct {
	Client       string           `json:"client"`
	ErrorRate    ComparisonMetric `json:"error_rate"`
	AvgLatency   ComparisonMetric `json:"avg_latency"`
	P95Latency   ComparisonMetric `json:"p95_latency"`
	P99Latency   ComparisonMetric `json:"p99_latency"`
	Throughput   ComparisonMetric `json:"throughput"`
	OverallScore ComparisonMetric `json:"overall_score"`
	Status       string           `json:"status"` // "improved", "degraded", "stable"
}

// RegressionThresholds defines thresholds for regression detection
type RegressionThresholds struct {
	ErrorRateThreshold  float64 `json:"error_rate_threshold"` // Absolute percentage point increase
	LatencyThreshold    float64 `json:"latency_threshold"`    // Percentage increase threshold
	ThroughputThreshold float64 `json:"throughput_threshold"` // Percentage decrease threshold
	SignificanceLevel   float64 `json:"significance_level"`   // Statistical significance level
	MinSampleSize       int     `json:"min_sample_size"`      // Minimum sample size for statistical tests
	ConsecutiveRuns     int     `json:"consecutive_runs"`     // Number of consecutive runs showing regression
}

// BaselineHistoryPoint represents a historical data point for baseline tracking
type BaselineHistoryPoint struct {
	Timestamp    time.Time `json:"timestamp"`
	RunID        string    `json:"run_id"`
	GitCommit    string    `json:"git_commit,omitempty"`
	ErrorRate    float64   `json:"error_rate"`
	AvgLatency   float64   `json:"avg_latency"`
	P95Latency   float64   `json:"p95_latency"`
	Status       string    `json:"status"` // "better", "worse", "similar"
	DeviationPct float64   `json:"deviation_pct"`
}

// baselineManager implements BaselineManager interface
type baselineManager struct {
	storage storage.HistoricStorage
	db      *sql.DB
	log     logrus.FieldLogger
}

// NewBaselineManager creates a new baseline manager
func NewBaselineManager(historicStorage storage.HistoricStorage, db *sql.DB, log logrus.FieldLogger) BaselineManager {
	return &baselineManager{
		storage: historicStorage,
		db:      db,
		log:     log.WithField("component", "baseline-manager"),
	}
}

// Start initializes the baseline manager
func (bm *baselineManager) Start(ctx context.Context) error {
	bm.log.Info("Starting baseline manager")

	// Ensure baselines table exists
	if err := bm.createBaselinesTable(ctx); err != nil {
		return fmt.Errorf("failed to create baselines table: %w", err)
	}

	bm.log.Info("Baseline manager started successfully")
	return nil
}

// Stop shuts down the baseline manager
func (bm *baselineManager) Stop() error {
	bm.log.Info("Stopping baseline manager")
	return nil
}

// SetBaseline creates or updates a performance baseline
func (bm *baselineManager) SetBaseline(ctx context.Context, runID, name, description string) (*Baseline, error) {
	bm.log.WithFields(logrus.Fields{
		"run_id": runID,
		"name":   name,
	}).Info("Setting baseline")

	// Get the historic run to use as baseline
	run, err := bm.storage.GetHistoricRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic run: %w", err)
	}

	// Extract baseline metrics from the run
	baselineMetrics, err := bm.extractBaselineMetrics(ctx, run)
	if err != nil {
		return nil, fmt.Errorf("failed to extract baseline metrics: %w", err)
	}

	// Create baseline object
	baseline := &Baseline{
		ID:              generateBaselineID(name, run.TestName),
		Name:            name,
		Description:     description,
		TestName:        run.TestName,
		RunID:           runID,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
		BaselineMetrics: *baselineMetrics,
		Tags:            []string{},
		IsActive:        true,
	}

	// Save to database
	if err := bm.saveBaseline(ctx, baseline); err != nil {
		return nil, fmt.Errorf("failed to save baseline: %w", err)
	}

	bm.log.WithFields(logrus.Fields{
		"baseline_id": baseline.ID,
		"name":        name,
		"test_name":   run.TestName,
	}).Info("Baseline set successfully")

	return baseline, nil
}

// GetBaseline retrieves a baseline by name
func (bm *baselineManager) GetBaseline(ctx context.Context, name string) (*Baseline, error) {
	query := `
		SELECT id, name, description, test_name, run_id, created_by, created_at, updated_at,
			   baseline_metrics, tags, is_active
		FROM baselines
		WHERE name = $1 AND is_active = true`

	var baseline Baseline
	var baselineMetricsJSON, tagsJSON []byte

	err := bm.db.QueryRowContext(ctx, query, name).Scan(
		&baseline.ID, &baseline.Name, &baseline.Description, &baseline.TestName,
		&baseline.RunID, &baseline.CreatedBy, &baseline.CreatedAt, &baseline.UpdatedAt,
		&baselineMetricsJSON, &tagsJSON, &baseline.IsActive,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("baseline not found: %s", name)
		}
		return nil, fmt.Errorf("failed to query baseline: %w", err)
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(baselineMetricsJSON, &baseline.BaselineMetrics); err != nil {
		return nil, fmt.Errorf("failed to unmarshal baseline metrics: %w", err)
	}

	if err := json.Unmarshal(tagsJSON, &baseline.Tags); err != nil {
		bm.log.WithError(err).Warn("Failed to unmarshal tags")
	}

	return &baseline, nil
}

// ListBaselines lists all baselines, optionally filtered by test name
func (bm *baselineManager) ListBaselines(ctx context.Context, testName string) ([]*Baseline, error) {
	var query string
	var args []interface{}

	if testName != "" {
		query = `
			SELECT id, name, description, test_name, run_id, created_by, created_at, updated_at,
				   baseline_metrics, tags, is_active
			FROM baselines
			WHERE test_name = $1 AND is_active = true
			ORDER BY created_at DESC`
		args = []interface{}{testName}
	} else {
		query = `
			SELECT id, name, description, test_name, run_id, created_by, created_at, updated_at,
				   baseline_metrics, tags, is_active
			FROM baselines
			WHERE is_active = true
			ORDER BY test_name, created_at DESC`
		args = []interface{}{}
	}

	rows, err := bm.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query baselines: %w", err)
	}
	defer rows.Close()

	var baselines []*Baseline
	for rows.Next() {
		baseline := &Baseline{}
		var baselineMetricsJSON, tagsJSON []byte

		err := rows.Scan(
			&baseline.ID, &baseline.Name, &baseline.Description, &baseline.TestName,
			&baseline.RunID, &baseline.CreatedBy, &baseline.CreatedAt, &baseline.UpdatedAt,
			&baselineMetricsJSON, &tagsJSON, &baseline.IsActive,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan baseline: %w", err)
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal(baselineMetricsJSON, &baseline.BaselineMetrics); err != nil {
			bm.log.WithError(err).Warn("Failed to unmarshal baseline metrics")
			continue
		}

		if err := json.Unmarshal(tagsJSON, &baseline.Tags); err != nil {
			bm.log.WithError(err).Warn("Failed to unmarshal tags")
		}

		baselines = append(baselines, baseline)
	}

	return baselines, rows.Err()
}

// DeleteBaseline soft deletes a baseline by marking it as inactive
func (bm *baselineManager) DeleteBaseline(ctx context.Context, name string) error {
	bm.log.WithField("name", name).Info("Deleting baseline")

	query := `UPDATE baselines SET is_active = false, updated_at = CURRENT_TIMESTAMP WHERE name = $1`
	result, err := bm.db.ExecContext(ctx, query, name)
	if err != nil {
		return fmt.Errorf("failed to delete baseline: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("baseline not found: %s", name)
	}

	bm.log.WithField("name", name).Info("Baseline deleted successfully")
	return nil
}

// CompareToBaseline compares a run against a specific baseline
func (bm *baselineManager) CompareToBaseline(ctx context.Context, runID, baselineName string) (*BaselineComparison, error) {
	bm.log.WithFields(logrus.Fields{
		"run_id":        runID,
		"baseline_name": baselineName,
	}).Info("Comparing run to baseline")

	// Get the run and baseline
	run, err := bm.storage.GetHistoricRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic run: %w", err)
	}

	baseline, err := bm.GetBaseline(ctx, baselineName)
	if err != nil {
		return nil, fmt.Errorf("failed to get baseline: %w", err)
	}

	// Ensure they're for the same test
	if run.TestName != baseline.TestName {
		return nil, fmt.Errorf("run test name (%s) doesn't match baseline test name (%s)",
			run.TestName, baseline.TestName)
	}

	// Perform comparison
	comparison, err := bm.performComparison(ctx, run, baseline)
	if err != nil {
		return nil, fmt.Errorf("failed to perform comparison: %w", err)
	}

	bm.log.WithFields(logrus.Fields{
		"run_id":        runID,
		"baseline_name": baselineName,
		"status":        comparison.Status,
		"risk_level":    comparison.RiskLevel,
	}).Info("Comparison completed")

	return comparison, nil
}

// CompareToAllBaselines compares a run against all applicable baselines
func (bm *baselineManager) CompareToAllBaselines(ctx context.Context, runID string) ([]*BaselineComparison, error) {
	bm.log.WithField("run_id", runID).Info("Comparing run to all baselines")

	// Get the run to determine test name
	run, err := bm.storage.GetHistoricRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get historic run: %w", err)
	}

	// Get all baselines for this test
	baselines, err := bm.ListBaselines(ctx, run.TestName)
	if err != nil {
		return nil, fmt.Errorf("failed to list baselines: %w", err)
	}

	if len(baselines) == 0 {
		bm.log.WithField("test_name", run.TestName).Info("No baselines found for test")
		return []*BaselineComparison{}, nil
	}

	// Compare against each baseline
	var comparisons []*BaselineComparison
	for _, baseline := range baselines {
		comparison, err := bm.performComparison(ctx, run, baseline)
		if err != nil {
			bm.log.WithError(err).WithField("baseline_name", baseline.Name).Warn("Failed to compare to baseline")
			continue
		}
		comparisons = append(comparisons, comparison)
	}

	bm.log.WithFields(logrus.Fields{
		"run_id":             runID,
		"baselines_compared": len(comparisons),
	}).Info("Completed comparison to all baselines")

	return comparisons, nil
}

// DetectRegressions detects performance regressions compared to a baseline
func (bm *baselineManager) DetectRegressions(ctx context.Context, runID, baselineName string, thresholds RegressionThresholds) ([]*types.Regression, error) {
	bm.log.WithFields(logrus.Fields{
		"run_id":        runID,
		"baseline_name": baselineName,
	}).Info("Detecting regressions")

	// Get comparison first
	comparison, err := bm.CompareToBaseline(ctx, runID, baselineName)
	if err != nil {
		return nil, fmt.Errorf("failed to get baseline comparison: %w", err)
	}

	var regressions []*types.Regression

	// Check overall metrics for regressions
	overallRegressions := bm.detectOverallRegressions(comparison, thresholds)
	regressions = append(regressions, overallRegressions...)

	// Check client-specific regressions
	clientRegressions := bm.detectClientRegressions(comparison, thresholds)
	regressions = append(regressions, clientRegressions...)

	// Sort by severity
	sort.Slice(regressions, func(i, j int) bool {
		severityOrder := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
		return severityOrder[regressions[i].Severity] < severityOrder[regressions[j].Severity]
	})

	bm.log.WithFields(logrus.Fields{
		"run_id":            runID,
		"baseline_name":     baselineName,
		"regressions_found": len(regressions),
	}).Info("Regression detection completed")

	return regressions, nil
}

// GetBaselineHistory gets historical performance data relative to a baseline
func (bm *baselineManager) GetBaselineHistory(ctx context.Context, baselineName string, days int) ([]*BaselineHistoryPoint, error) {
	bm.log.WithFields(logrus.Fields{
		"baseline_name": baselineName,
		"days":          days,
	}).Info("Getting baseline history")

	// Get the baseline
	baseline, err := bm.GetBaseline(ctx, baselineName)
	if err != nil {
		return nil, fmt.Errorf("failed to get baseline: %w", err)
	}

	// Get historic runs for the same test
	startTime := time.Now().AddDate(0, 0, -days)
	query := `
		SELECT id, timestamp, git_commit, overall_error_rate, avg_latency_ms, p95_latency_ms
		FROM historic_runs
		WHERE test_name = $1 AND timestamp >= $2
		ORDER BY timestamp`

	rows, err := bm.db.QueryContext(ctx, query, baseline.TestName, startTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query historic runs: %w", err)
	}
	defer rows.Close()

	var history []*BaselineHistoryPoint
	for rows.Next() {
		var point BaselineHistoryPoint
		err := rows.Scan(&point.RunID, &point.Timestamp, &point.GitCommit,
			&point.ErrorRate, &point.AvgLatency, &point.P95Latency)
		if err != nil {
			return nil, fmt.Errorf("failed to scan history point: %w", err)
		}

		// Calculate deviation from baseline
		point.DeviationPct = bm.calculateDeviation(point.AvgLatency, baseline.BaselineMetrics.AvgLatencyMs)
		point.Status = bm.categorizeDeviation(point.DeviationPct)

		history = append(history, &point)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	bm.log.WithFields(logrus.Fields{
		"baseline_name": baselineName,
		"points":        len(history),
	}).Info("Baseline history retrieved")

	return history, nil
}

// Helper methods

func (bm *baselineManager) createBaselinesTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS baselines (
			id VARCHAR(255) PRIMARY KEY,
			name VARCHAR(255) NOT NULL UNIQUE,
			description TEXT,
			test_name VARCHAR(255) NOT NULL,
			run_id VARCHAR(255) NOT NULL,
			created_by VARCHAR(255),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			baseline_metrics JSONB NOT NULL,
			tags JSONB DEFAULT '[]',
			is_active BOOLEAN NOT NULL DEFAULT true,
			
			FOREIGN KEY (run_id) REFERENCES historic_runs(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_baselines_test_name ON baselines(test_name);
		CREATE INDEX IF NOT EXISTS idx_baselines_active ON baselines(is_active);
		CREATE INDEX IF NOT EXISTS idx_baselines_created_at ON baselines(created_at);`

	_, err := bm.db.ExecContext(ctx, query)
	return err
}

func (bm *baselineManager) extractBaselineMetrics(ctx context.Context, run *types.HistoricRun) (*BaselineMetrics, error) {
	// Parse full results to get detailed client metrics
	var fullResult types.BenchmarkResult
	if err := json.Unmarshal(run.FullResults, &fullResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal full results: %w", err)
	}

	clientMetrics := make(map[string]ClientBaseline)
	for clientName, metrics := range fullResult.ClientMetrics {
		clientMetrics[clientName] = ClientBaseline{
			ErrorRate:     metrics.ErrorRate,
			AvgLatency:    metrics.Latency.Avg,
			P95Latency:    metrics.Latency.P95,
			P99Latency:    metrics.Latency.P99,
			Throughput:    metrics.Latency.Throughput,
			TotalRequests: metrics.TotalRequests,
			TotalErrors:   metrics.TotalErrors,
		}
	}

	return &BaselineMetrics{
		OverallErrorRate:  run.OverallErrorRate,
		AvgLatencyMs:      run.AvgLatencyMs,
		P95LatencyMs:      run.P95LatencyMs,
		P99LatencyMs:      run.P99LatencyMs,
		MaxLatencyMs:      run.MaxLatencyMs,
		TotalRequests:     run.TotalRequests,
		TotalErrors:       run.TotalErrors,
		PerformanceScores: run.PerformanceScores,
		ClientMetrics:     clientMetrics,
	}, nil
}

func (bm *baselineManager) saveBaseline(ctx context.Context, baseline *Baseline) error {
	query := `
		INSERT INTO baselines (
			id, name, description, test_name, run_id, created_by, created_at, updated_at,
			baseline_metrics, tags, is_active
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			run_id = EXCLUDED.run_id,
			updated_at = CURRENT_TIMESTAMP,
			baseline_metrics = EXCLUDED.baseline_metrics,
			tags = EXCLUDED.tags`

	baselineMetricsJSON, err := json.Marshal(baseline.BaselineMetrics)
	if err != nil {
		return fmt.Errorf("failed to marshal baseline metrics: %w", err)
	}

	tagsJSON, err := json.Marshal(baseline.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	_, err = bm.db.ExecContext(ctx, query,
		baseline.ID, baseline.Name, baseline.Description, baseline.TestName,
		baseline.RunID, baseline.CreatedBy, baseline.CreatedAt, baseline.UpdatedAt,
		baselineMetricsJSON, tagsJSON, baseline.IsActive,
	)

	return err
}

func (bm *baselineManager) performComparison(ctx context.Context, run *types.HistoricRun, baseline *Baseline) (*BaselineComparison, error) {
	// Parse run's full results for detailed comparison
	var fullResult types.BenchmarkResult
	if err := json.Unmarshal(run.FullResults, &fullResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal run results: %w", err)
	}

	comparison := &BaselineComparison{
		RunID:         run.ID,
		BaselineName:  baseline.Name,
		BaselineRunID: baseline.RunID,
		ComparedAt:    time.Now(),
		ClientChanges: make(map[string]ClientComparison),
	}

	// Overall comparison
	comparison.OverallChange = ComparisonMetric{
		BaselineValue:  baseline.BaselineMetrics.AvgLatencyMs,
		CurrentValue:   run.AvgLatencyMs,
		AbsoluteChange: run.AvgLatencyMs - baseline.BaselineMetrics.AvgLatencyMs,
		PercentChange:  calculatePercentChange(baseline.BaselineMetrics.AvgLatencyMs, run.AvgLatencyMs),
		IsImprovement:  run.AvgLatencyMs < baseline.BaselineMetrics.AvgLatencyMs,
		IsSignificant:  math.Abs(calculatePercentChange(baseline.BaselineMetrics.AvgLatencyMs, run.AvgLatencyMs)) > 5.0,
	}

	// Client-level comparisons
	for clientName, currentMetrics := range fullResult.ClientMetrics {
		if baselineClientMetrics, exists := baseline.BaselineMetrics.ClientMetrics[clientName]; exists {
			clientComp := bm.compareClientMetrics(currentMetrics, baselineClientMetrics)
			comparison.ClientChanges[clientName] = clientComp
		}
	}

	// Determine overall status and risk level
	comparison.Status = bm.determineComparisonStatus(comparison)
	comparison.RiskLevel = bm.determineRiskLevel(comparison)
	comparison.Summary = bm.generateComparisonSummary(comparison)
	comparison.Recommendations = bm.generateRecommendations(comparison)

	return comparison, nil
}

func (bm *baselineManager) compareClientMetrics(current *types.ClientMetrics, baseline ClientBaseline) ClientComparison {
	errorRateChange := ComparisonMetric{
		BaselineValue:  baseline.ErrorRate,
		CurrentValue:   current.ErrorRate,
		AbsoluteChange: current.ErrorRate - baseline.ErrorRate,
		PercentChange:  calculatePercentChange(baseline.ErrorRate, current.ErrorRate),
		IsImprovement:  current.ErrorRate < baseline.ErrorRate,
		IsSignificant:  math.Abs(current.ErrorRate-baseline.ErrorRate) > 0.01, // 1% threshold
	}

	avgLatencyChange := ComparisonMetric{
		BaselineValue:  baseline.AvgLatency,
		CurrentValue:   current.Latency.Avg,
		AbsoluteChange: current.Latency.Avg - baseline.AvgLatency,
		PercentChange:  calculatePercentChange(baseline.AvgLatency, current.Latency.Avg),
		IsImprovement:  current.Latency.Avg < baseline.AvgLatency,
		IsSignificant:  math.Abs(calculatePercentChange(baseline.AvgLatency, current.Latency.Avg)) > 5.0,
	}

	p95LatencyChange := ComparisonMetric{
		BaselineValue:  baseline.P95Latency,
		CurrentValue:   current.Latency.P95,
		AbsoluteChange: current.Latency.P95 - baseline.P95Latency,
		PercentChange:  calculatePercentChange(baseline.P95Latency, current.Latency.P95),
		IsImprovement:  current.Latency.P95 < baseline.P95Latency,
		IsSignificant:  math.Abs(calculatePercentChange(baseline.P95Latency, current.Latency.P95)) > 5.0,
	}

	p99LatencyChange := ComparisonMetric{
		BaselineValue:  baseline.P99Latency,
		CurrentValue:   current.Latency.P99,
		AbsoluteChange: current.Latency.P99 - baseline.P99Latency,
		PercentChange:  calculatePercentChange(baseline.P99Latency, current.Latency.P99),
		IsImprovement:  current.Latency.P99 < baseline.P99Latency,
		IsSignificant:  math.Abs(calculatePercentChange(baseline.P99Latency, current.Latency.P99)) > 5.0,
	}

	throughputChange := ComparisonMetric{
		BaselineValue:  baseline.Throughput,
		CurrentValue:   current.Latency.Throughput,
		AbsoluteChange: current.Latency.Throughput - baseline.Throughput,
		PercentChange:  calculatePercentChange(baseline.Throughput, current.Latency.Throughput),
		IsImprovement:  current.Latency.Throughput > baseline.Throughput,
		IsSignificant:  math.Abs(calculatePercentChange(baseline.Throughput, current.Latency.Throughput)) > 5.0,
	}

	// Calculate overall score change (composite metric)
	baselineScore := calculateOverallScore(baseline.ErrorRate, baseline.AvgLatency, baseline.Throughput)
	currentScore := calculateOverallScore(current.ErrorRate, current.Latency.Avg, current.Latency.Throughput)

	overallScoreChange := ComparisonMetric{
		BaselineValue:  baselineScore,
		CurrentValue:   currentScore,
		AbsoluteChange: currentScore - baselineScore,
		PercentChange:  calculatePercentChange(baselineScore, currentScore),
		IsImprovement:  currentScore > baselineScore,
		IsSignificant:  math.Abs(calculatePercentChange(baselineScore, currentScore)) > 3.0,
	}

	// Determine client status
	status := "stable"
	if overallScoreChange.IsSignificant {
		if overallScoreChange.IsImprovement {
			status = "improved"
		} else {
			status = "degraded"
		}
	}

	return ClientComparison{
		Client:       current.Name,
		ErrorRate:    errorRateChange,
		AvgLatency:   avgLatencyChange,
		P95Latency:   p95LatencyChange,
		P99Latency:   p99LatencyChange,
		Throughput:   throughputChange,
		OverallScore: overallScoreChange,
		Status:       status,
	}
}

func (bm *baselineManager) determineComparisonStatus(comparison *BaselineComparison) string {
	improved := 0
	degraded := 0
	stable := 0

	for _, clientComp := range comparison.ClientChanges {
		switch clientComp.Status {
		case "improved":
			improved++
		case "degraded":
			degraded++
		case "stable":
			stable++
		}
	}

	total := improved + degraded + stable
	if total == 0 {
		return "unknown"
	}

	// Determine overall status
	if degraded > improved && degraded > stable {
		return "degraded"
	} else if improved > degraded && improved > stable {
		return "improved"
	} else if stable > improved && stable > degraded {
		return "stable"
	} else {
		return "mixed"
	}
}

func (bm *baselineManager) determineRiskLevel(comparison *BaselineComparison) string {
	maxRisk := "low"

	// Check overall change significance
	if comparison.OverallChange.IsSignificant && !comparison.OverallChange.IsImprovement {
		if math.Abs(comparison.OverallChange.PercentChange) > 25 {
			maxRisk = "critical"
		} else if math.Abs(comparison.OverallChange.PercentChange) > 15 {
			maxRisk = "high"
		} else if math.Abs(comparison.OverallChange.PercentChange) > 5 {
			maxRisk = "medium"
		}
	}

	// Check client-level risks
	for _, clientComp := range comparison.ClientChanges {
		if clientComp.Status == "degraded" {
			if clientComp.ErrorRate.IsSignificant && clientComp.ErrorRate.PercentChange > 50 {
				maxRisk = "critical"
			} else if clientComp.AvgLatency.IsSignificant && clientComp.AvgLatency.PercentChange > 30 {
				if maxRisk != "critical" {
					maxRisk = "high"
				}
			} else if clientComp.OverallScore.IsSignificant && clientComp.OverallScore.PercentChange < -15 {
				if maxRisk == "low" {
					maxRisk = "medium"
				}
			}
		}
	}

	return maxRisk
}

func (bm *baselineManager) generateComparisonSummary(comparison *BaselineComparison) string {
	var summary strings.Builder

	summary.WriteString(fmt.Sprintf("Overall: %s (%.1f%% change)",
		comparison.Status, comparison.OverallChange.PercentChange))

	if len(comparison.ClientChanges) > 0 {
		summary.WriteString("; Clients: ")
		var clientSummaries []string
		for client, change := range comparison.ClientChanges {
			clientSummaries = append(clientSummaries, fmt.Sprintf("%s %s", client, change.Status))
		}
		summary.WriteString(strings.Join(clientSummaries, ", "))
	}

	return summary.String()
}

func (bm *baselineManager) generateRecommendations(comparison *BaselineComparison) []string {
	var recommendations []string

	if comparison.Status == "degraded" || comparison.RiskLevel == "high" || comparison.RiskLevel == "critical" {
		recommendations = append(recommendations, "Consider investigating the performance regression")

		if comparison.RiskLevel == "critical" {
			recommendations = append(recommendations, "Critical performance degradation detected - immediate attention required")
		}

		for client, change := range comparison.ClientChanges {
			if change.Status == "degraded" {
				if change.ErrorRate.IsSignificant && change.ErrorRate.PercentChange > 10 {
					recommendations = append(recommendations, fmt.Sprintf("High error rate increase in %s client", client))
				}
				if change.AvgLatency.IsSignificant && change.AvgLatency.PercentChange > 20 {
					recommendations = append(recommendations, fmt.Sprintf("Significant latency increase in %s client", client))
				}
			}
		}
	}

	if comparison.Status == "improved" {
		recommendations = append(recommendations, "Performance improvement detected - consider updating baseline")
	}

	return recommendations
}

func (bm *baselineManager) detectOverallRegressions(comparison *BaselineComparison, thresholds RegressionThresholds) []*types.Regression {
	var regressions []*types.Regression

	// Check overall error rate regression
	if comparison.OverallChange.AbsoluteChange > thresholds.ErrorRateThreshold {
		regression := &types.Regression{
			ID:             generateRegressionID(),
			RunID:          comparison.RunID,
			BaselineRunID:  comparison.BaselineRunID,
			Client:         "overall",
			Metric:         "error_rate",
			BaselineValue:  comparison.OverallChange.BaselineValue,
			CurrentValue:   comparison.OverallChange.CurrentValue,
			PercentChange:  comparison.OverallChange.PercentChange,
			AbsoluteChange: comparison.OverallChange.AbsoluteChange,
			Severity:       bm.categorizeRegressionSeverity(comparison.OverallChange.PercentChange, "error_rate"),
			IsSignificant:  comparison.OverallChange.IsSignificant,
			DetectedAt:     comparison.ComparedAt,
		}
		regressions = append(regressions, regression)
	}

	return regressions
}

func (bm *baselineManager) detectClientRegressions(comparison *BaselineComparison, thresholds RegressionThresholds) []*types.Regression {
	var regressions []*types.Regression

	for clientName, clientComp := range comparison.ClientChanges {
		if clientComp.Status != "degraded" {
			continue
		}

		// Check error rate regression
		if clientComp.ErrorRate.AbsoluteChange > thresholds.ErrorRateThreshold {
			regression := &types.Regression{
				ID:             generateRegressionID(),
				RunID:          comparison.RunID,
				BaselineRunID:  comparison.BaselineRunID,
				Client:         clientName,
				Metric:         "error_rate",
				BaselineValue:  clientComp.ErrorRate.BaselineValue,
				CurrentValue:   clientComp.ErrorRate.CurrentValue,
				PercentChange:  clientComp.ErrorRate.PercentChange,
				AbsoluteChange: clientComp.ErrorRate.AbsoluteChange,
				Severity:       bm.categorizeRegressionSeverity(clientComp.ErrorRate.PercentChange, "error_rate"),
				IsSignificant:  clientComp.ErrorRate.IsSignificant,
				DetectedAt:     comparison.ComparedAt,
			}
			regressions = append(regressions, regression)
		}

		// Check latency regression
		if clientComp.AvgLatency.PercentChange > thresholds.LatencyThreshold {
			regression := &types.Regression{
				ID:             generateRegressionID(),
				RunID:          comparison.RunID,
				BaselineRunID:  comparison.BaselineRunID,
				Client:         clientName,
				Metric:         "avg_latency",
				BaselineValue:  clientComp.AvgLatency.BaselineValue,
				CurrentValue:   clientComp.AvgLatency.CurrentValue,
				PercentChange:  clientComp.AvgLatency.PercentChange,
				AbsoluteChange: clientComp.AvgLatency.AbsoluteChange,
				Severity:       bm.categorizeRegressionSeverity(clientComp.AvgLatency.PercentChange, "latency"),
				IsSignificant:  clientComp.AvgLatency.IsSignificant,
				DetectedAt:     comparison.ComparedAt,
			}
			regressions = append(regressions, regression)
		}

		// Check throughput regression
		if clientComp.Throughput.PercentChange < -thresholds.ThroughputThreshold {
			regression := &types.Regression{
				ID:             generateRegressionID(),
				RunID:          comparison.RunID,
				BaselineRunID:  comparison.BaselineRunID,
				Client:         clientName,
				Metric:         "throughput",
				BaselineValue:  clientComp.Throughput.BaselineValue,
				CurrentValue:   clientComp.Throughput.CurrentValue,
				PercentChange:  clientComp.Throughput.PercentChange,
				AbsoluteChange: clientComp.Throughput.AbsoluteChange,
				Severity:       bm.categorizeRegressionSeverity(math.Abs(clientComp.Throughput.PercentChange), "throughput"),
				IsSignificant:  clientComp.Throughput.IsSignificant,
				DetectedAt:     comparison.ComparedAt,
			}
			regressions = append(regressions, regression)
		}
	}

	return regressions
}

func (bm *baselineManager) categorizeRegressionSeverity(percentChange float64, metricType string) string {
	absChange := math.Abs(percentChange)

	switch metricType {
	case "error_rate":
		if absChange > 100 {
			return "critical"
		} else if absChange > 50 {
			return "high"
		} else if absChange > 25 {
			return "medium"
		} else {
			return "low"
		}
	case "latency":
		if absChange > 50 {
			return "critical"
		} else if absChange > 25 {
			return "high"
		} else if absChange > 10 {
			return "medium"
		} else {
			return "low"
		}
	case "throughput":
		if absChange > 40 {
			return "critical"
		} else if absChange > 20 {
			return "high"
		} else if absChange > 10 {
			return "medium"
		} else {
			return "low"
		}
	default:
		return "medium"
	}
}

func (bm *baselineManager) calculateDeviation(current, baseline float64) float64 {
	if baseline == 0 {
		return 0
	}
	return ((current - baseline) / baseline) * 100
}

func (bm *baselineManager) categorizeDeviation(deviationPct float64) string {
	if deviationPct < -10 {
		return "better"
	} else if deviationPct > 10 {
		return "worse"
	} else {
		return "similar"
	}
}

// Utility functions

func generateBaselineID(name, testName string) string {
	timestamp := time.Now().Format("20060102_150405")
	return fmt.Sprintf("baseline_%s_%s_%s",
		strings.ReplaceAll(testName, " ", "_"),
		strings.ReplaceAll(name, " ", "_"),
		timestamp)
}

func generateRegressionID() string {
	timestamp := time.Now().Format("20060102_150405")
	return fmt.Sprintf("regression_%s_%d", timestamp, time.Now().UnixNano()%1000)
}

func calculatePercentChange(baseline, current float64) float64 {
	if baseline == 0 {
		return 0
	}
	return ((current - baseline) / baseline) * 100
}

func calculateOverallScore(errorRate, avgLatency, throughput float64) float64 {
	// Composite score: higher throughput and lower latency/error rate = better score
	// Normalize and weight the metrics
	const maxLatency = 1000.0     // Assume 1000ms as worst case
	const maxThroughput = 10000.0 // Assume 10K RPS as excellent

	latencyScore := math.Max(0, (maxLatency-avgLatency)/maxLatency) * 40 // 40% weight
	throughputScore := math.Min(throughput/maxThroughput, 1.0) * 40      // 40% weight
	errorScore := math.Max(0, (1.0-errorRate)) * 20                      // 20% weight

	return latencyScore + throughputScore + errorScore
}
