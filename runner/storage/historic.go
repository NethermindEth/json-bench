package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

// HistoricStorage manages both file and PostgreSQL storage of benchmark results
type HistoricStorage struct {
	db         *Database
	basePath   string
	gitEnabled bool
	log        logrus.FieldLogger
}

// NewHistoricStorage creates a new historic storage manager
func NewHistoricStorage(cfg *config.StorageConfig, logger *logrus.Logger) (*HistoricStorage, error) {
	log := logger.WithField("component", "historic_storage")

	// Create database connection
	db, err := NewDatabase(&cfg.PostgreSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	if err := db.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Check if git is available
	gitEnabled := true
	if _, err := exec.LookPath("git"); err != nil {
		log.Warn("Git not found, git integration disabled")
		gitEnabled = false
	}

	return &HistoricStorage{
		db:         db,
		basePath:   cfg.HistoricPath,
		gitEnabled: gitEnabled,
		log:        log,
	}, nil
}

// SaveRun saves a benchmark result to both file and database storage
func (h *HistoricStorage) SaveRun(result *types.BenchmarkResult, cfg *config.Config) (*types.HistoricRun, error) {
	// Generate run ID
	runID := h.generateRunID()
	h.log.WithField("run_id", runID).Info("Saving historic run")

	// Get git info
	gitCommit, gitBranch := h.getGitInfo()

	// Create run directory
	runDir := h.createRunDirectory(runID)

	// Copy results to historic directory
	if err := h.copyResults(result, runDir); err != nil {
		return nil, fmt.Errorf("failed to copy results: %w", err)
	}

	// Save run configuration with client URLs
	if err := h.SaveRunConfig(cfg, runDir); err != nil {
		h.log.WithError(err).Error("Failed to save run configuration")
		// Continue execution even if saving config fails
	}

	// Calculate config hash
	configHash := h.calculateConfigHash(cfg)

	// Marshal full results for storage
	fullResultsJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal full results: %w", err)
	}

	// Calculate performance scores
	performanceScores := calculatePerformanceScores(result)

	// Create historic run record
	run := &types.HistoricRun{
		ID:            runID,
		Timestamp:     time.Now(),
		GitCommit:     gitCommit,
		GitBranch:     gitBranch,
		TestName:      extractTestName(cfg),
		Description:   extractDescription(cfg),
		ConfigHash:    configHash,
		ResultPath:    runDir,
		Duration:      result.Duration,
		TotalRequests: calculateTotalRequests(result),
		SuccessRate:   calculateSuccessRate(result),
		AvgLatency:    calculateAvgLatency(result),
		P95Latency:    calculateP95Latency(result),
		Clients:       extractClients(result),
		Methods:       extractMethods(result),
		Tags:          extractTags(cfg),

		// Additional fields for baseline analysis
		OverallErrorRate:  100.0 - calculateSuccessRate(result),
		AvgLatencyMs:      calculateAvgLatency(result),
		P95LatencyMs:      calculateP95Latency(result),
		P99LatencyMs:      calculateP99Latency(result),
		MaxLatencyMs:      calculateMaxLatency(result),
		TotalErrors:       calculateTotalErrors(result),
		PerformanceScores: performanceScores,
		FullResults:       fullResultsJSON,
	}

	// Save to database
	if err := h.db.InsertRun(run); err != nil {
		return nil, fmt.Errorf("failed to save run to database: %w", err)
	}

	// Convert and save time-series metrics
	metrics := h.convertToTimeSeriesMetrics(result, run)
	if err := h.saveMetricsToPostgreSQL(metrics); err != nil {
		h.log.WithError(err).Error("Failed to save metrics to postgres")
	}

	return run, nil
}

// SaveRunConfig saves the full run configuration including client URLs to a JSON file
// This method stores complete client information (name and URL) in a run_config.json file
// within the run directory, allowing for future reference of which clients were used
// and their endpoints during the benchmark run.
func (h *HistoricStorage) SaveRunConfig(cfg *config.Config, runDir string) error {
	// Create a structure to hold the full client information
	type ClientInfo struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}

	type RunConfig struct {
		Clients []ClientInfo `json:"clients"`
		// Add other config fields as needed
	}

	// Extract client information from config
	var clientInfos []ClientInfo
	if cfg.ResolvedClients != nil {
		for _, client := range cfg.ResolvedClients {
			if client != nil {
				clientInfos = append(clientInfos, ClientInfo{
					Name: client.Name,
					URL:  client.URL,
				})
			}
		}
	}

	runConfig := RunConfig{
		Clients: clientInfos,
	}

	// Marshal the configuration to JSON
	configJSON, err := json.MarshalIndent(runConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal run config: %w", err)
	}

	// Save to file
	configPath := filepath.Join(runDir, "run_config.json")
	if err := os.WriteFile(configPath, configJSON, 0644); err != nil {
		return fmt.Errorf("failed to write run config: %w", err)
	}

	h.log.WithField("config_path", configPath).Debug("Saved run configuration")
	return nil
}

// generateRunID creates a unique run ID with format: YYYYMMDD-HHMMSS-COMMIT
func (h *HistoricStorage) generateRunID() string {
	now := time.Now()
	commit, _ := h.getGitInfo()
	if commit == "" {
		commit = "unknown"
	} else if len(commit) > 7 {
		commit = commit[:7]
	}
	return fmt.Sprintf("%s-%s", now.Format("20060102-150405"), commit)
}

// getGitInfo retrieves current git commit and branch
func (h *HistoricStorage) getGitInfo() (commit, branch string) {
	if !h.gitEnabled {
		return "", ""
	}

	// Get commit hash
	if cmd := exec.Command("git", "rev-parse", "HEAD"); cmd != nil {
		if output, err := cmd.Output(); err == nil {
			commit = strings.TrimSpace(string(output))
		}
	}

	// Get branch name
	if cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD"); cmd != nil {
		if output, err := cmd.Output(); err == nil {
			branch = strings.TrimSpace(string(output))
		}
	}

	return commit, branch
}

// createRunDirectory creates directory for storing run results
func (h *HistoricStorage) createRunDirectory(runID string) string {
	runDir := filepath.Join(h.basePath, runID)
	os.MkdirAll(runDir, 0755)
	os.MkdirAll(filepath.Join(runDir, "exports"), 0755)
	return runDir
}

// copyResults copies benchmark results to historic directory
func (h *HistoricStorage) copyResults(result *types.BenchmarkResult, destDir string) error {
	// This would copy HTML report, JSON results, CSV exports etc.
	// Implementation depends on how results are structured
	return nil
}

// calculateConfigHash creates hash of configuration for comparison
func (h *HistoricStorage) calculateConfigHash(cfg *config.Config) string {
	// Create a deterministic hash of the configuration
	configStr := fmt.Sprintf("%+v", cfg)
	hash := sha256.Sum256([]byte(configStr))
	return fmt.Sprintf("%x", hash)[:16]
}

// convertToTimeSeriesMetrics converts BenchmarkResult to time-series metrics
func (h *HistoricStorage) convertToTimeSeriesMetrics(result *types.BenchmarkResult, run *types.HistoricRun) []types.TimeSeriesMetric {
	var metrics []types.TimeSeriesMetric

	// Convert client metrics to time-series format
	for clientName, clientMetrics := range result.ClientMetrics {
		timestamp := time.Now()

		// Add all latency percentiles and metrics for overall client performance
		metrics = append(metrics,
			// Latency metrics
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: types.MetricLatencyAvg, Value: clientMetrics.Latency.Avg,
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: types.MetricLatencyMin, Value: clientMetrics.Latency.Min,
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: types.MetricLatencyMax, Value: clientMetrics.Latency.Max,
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: types.MetricLatencyP50, Value: clientMetrics.Latency.P50,
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: types.MetricLatencyP90, Value: clientMetrics.Latency.P90,
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: types.MetricLatencyP95, Value: clientMetrics.Latency.P95,
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: types.MetricLatencyP99, Value: clientMetrics.Latency.P99,
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
			// Success rate and throughput
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: types.MetricSuccessRate, Value: 100.0 - clientMetrics.ErrorRate,
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: types.MetricErrorRate, Value: clientMetrics.ErrorRate,
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: types.MetricThroughput, Value: clientMetrics.Latency.Throughput,
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
			types.TimeSeriesMetric{
				Time: timestamp, RunID: run.ID, Client: clientName, Method: "all",
				MetricName: "total_requests", Value: float64(clientMetrics.TotalRequests),
				Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
			},
		)

		// Add method-specific metrics with all percentiles
		for methodName, methodMetrics := range clientMetrics.Methods {
			metrics = append(metrics,
				// Latency metrics
				types.TimeSeriesMetric{
					Time: timestamp, RunID: run.ID, Client: clientName, Method: methodName,
					MetricName: types.MetricLatencyAvg, Value: methodMetrics.Avg,
					Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
				},
				types.TimeSeriesMetric{
					Time: timestamp, RunID: run.ID, Client: clientName, Method: methodName,
					MetricName: types.MetricLatencyMin, Value: methodMetrics.Min,
					Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
				},
				types.TimeSeriesMetric{
					Time: timestamp, RunID: run.ID, Client: clientName, Method: methodName,
					MetricName: types.MetricLatencyMax, Value: methodMetrics.Max,
					Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
				},
				types.TimeSeriesMetric{
					Time: timestamp, RunID: run.ID, Client: clientName, Method: methodName,
					MetricName: types.MetricLatencyP50, Value: methodMetrics.P50,
					Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
				},
				types.TimeSeriesMetric{
					Time: timestamp, RunID: run.ID, Client: clientName, Method: methodName,
					MetricName: types.MetricLatencyP90, Value: methodMetrics.P90,
					Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
				},
				types.TimeSeriesMetric{
					Time: timestamp, RunID: run.ID, Client: clientName, Method: methodName,
					MetricName: types.MetricLatencyP95, Value: methodMetrics.P95,
					Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
				},
				types.TimeSeriesMetric{
					Time: timestamp, RunID: run.ID, Client: clientName, Method: methodName,
					MetricName: types.MetricLatencyP99, Value: methodMetrics.P99,
					Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
				},
				// Success rate, error rate, and throughput
				types.TimeSeriesMetric{
					Time: timestamp, RunID: run.ID, Client: clientName, Method: methodName,
					MetricName: types.MetricSuccessRate, Value: 100.0 - methodMetrics.ErrorRate,
					Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
				},
				types.TimeSeriesMetric{
					Time: timestamp, RunID: run.ID, Client: clientName, Method: methodName,
					MetricName: types.MetricErrorRate, Value: methodMetrics.ErrorRate,
					Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
				},
				types.TimeSeriesMetric{
					Time: timestamp, RunID: run.ID, Client: clientName, Method: methodName,
					MetricName: types.MetricThroughput, Value: methodMetrics.Throughput,
					Tags: map[string]string{"git_commit": run.GitCommit, "test_name": run.TestName},
				},
			)
		}
	}

	return metrics
}

// saveMetricsToPostgreSQL saves metrics to PostgreSQL database
func (h *HistoricStorage) saveMetricsToPostgreSQL(metrics []types.TimeSeriesMetric) error {
	return h.db.InsertMetrics(metrics)
}

// LoadRun loads a historic run from storage
func (h *HistoricStorage) LoadRun(runID string) (*types.BenchmarkResult, error) {
	// Load run metadata from database
	run, err := h.db.GetRun(runID)
	if err != nil {
		return nil, err
	}

	// Load full results from file storage if available
	resultPath := filepath.Join(run.ResultPath, "results.json")
	if _, err := os.Stat(resultPath); err == nil {
		// Load from file - implementation depends on file format
	}

	// For now, return minimal result based on database data
	return &types.BenchmarkResult{
		Duration: run.Duration,
		// ClientMetrics would be reconstructed from database
	}, nil
}

// Helper functions for extracting data from config and results
func extractTestName(cfg *config.Config) string {
	// Extract test name from config
	return "default_test"
}

func extractDescription(cfg *config.Config) string {
	return ""
}

func extractTags(cfg *config.Config) []string {
	return []string{}
}

// extractClients extracts client names from benchmark results
// Note: This only extracts names for the database record. Full client information
// including URLs is saved separately via SaveRunConfig method to run_config.json
func extractClients(result *types.BenchmarkResult) []string {
	clients := make([]string, 0, len(result.ClientMetrics))
	for client := range result.ClientMetrics {
		clients = append(clients, client)
	}
	return clients
}

func extractMethods(result *types.BenchmarkResult) []string {
	methodSet := make(map[string]bool)
	for _, clientMetrics := range result.ClientMetrics {
		for method := range clientMetrics.Methods {
			methodSet[method] = true
		}
	}

	methods := make([]string, 0, len(methodSet))
	for method := range methodSet {
		methods = append(methods, method)
	}
	return methods
}

func calculateTotalRequests(result *types.BenchmarkResult) int64 {
	var total int64
	for _, clientMetrics := range result.ClientMetrics {
		total += clientMetrics.TotalRequests
	}
	return total
}

func calculateSuccessRate(result *types.BenchmarkResult) float64 {
	var totalRequests, totalErrors int64
	for _, clientMetrics := range result.ClientMetrics {
		totalRequests += clientMetrics.TotalRequests
		totalErrors += clientMetrics.TotalErrors
	}
	if totalRequests == 0 {
		return 100.0
	}
	return float64(totalRequests-totalErrors) / float64(totalRequests) * 100.0
}

func calculateAvgLatency(result *types.BenchmarkResult) float64 {
	var sum float64
	var count int
	for _, clientMetrics := range result.ClientMetrics {
		sum += clientMetrics.Latency.Avg
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func calculateP95Latency(result *types.BenchmarkResult) float64 {
	var sum float64
	var count int
	for _, clientMetrics := range result.ClientMetrics {
		sum += clientMetrics.Latency.P95
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// GetBasePath returns the base path for historic storage
func (h *HistoricStorage) GetBasePath() string {
	return h.basePath
}

// GetHistoricRun retrieves a historic run by ID from the database
func (h *HistoricStorage) GetHistoricRun(ctx context.Context, runID string) (*types.HistoricRun, error) {
	return h.db.GetRun(runID)
}

// ListHistoricRuns retrieves a list of historic runs with filtering
func (h *HistoricStorage) ListHistoricRuns(ctx context.Context, filter types.RunFilter) ([]*types.HistoricRun, error) {
	return h.db.ListRuns(filter)
}

// GetHistoricTrends returns one TrendData per top-level run metric
// (avg_latency, p95_latency, success_rate, total_requests) over the
// runs matching filter.TestName / GitBranch / Since / Until.
//
// Each TrendData carries the raw per-run samples in chronological
// order; the caller (analysis package, dashboard, etc.) is responsible
// for any further smoothing. PercentChange is computed first-to-last
// and Direction is derived from a 1% threshold so flat series read as
// "stable" rather than spuriously labeled improving/degrading.
func (h *HistoricStorage) GetHistoricTrends(ctx context.Context, filter types.TrendFilter) ([]*types.TrendData, error) {
	query := `SELECT id, timestamp, git_commit, total_requests, success_rate, avg_latency, p95_latency
		FROM benchmark_runs WHERE 1=1`
	args := []interface{}{}
	idx := 1
	if filter.TestName != "" {
		query += fmt.Sprintf(" AND test_name = $%d", idx)
		args = append(args, filter.TestName)
		idx++
	}
	if filter.GitBranch != "" {
		query += fmt.Sprintf(" AND git_branch = $%d", idx)
		args = append(args, filter.GitBranch)
		idx++
	}
	if !filter.Since.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= $%d", idx)
		args = append(args, filter.Since)
		idx++
	}
	if !filter.Until.IsZero() {
		query += fmt.Sprintf(" AND timestamp <= $%d", idx)
		args = append(args, filter.Until)
		idx++
	}
	query += " ORDER BY timestamp ASC"

	rows, err := h.db.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query benchmark_runs: %w", err)
	}
	defer rows.Close()

	type sample struct {
		ts            time.Time
		runID         string
		gitCommit     string
		totalRequests float64
		successRate   float64
		avgLatency    float64
		p95Latency    float64
	}
	var samples []sample
	for rows.Next() {
		var s sample
		var totalRequests int64
		if err := rows.Scan(&s.runID, &s.ts, &s.gitCommit, &totalRequests, &s.successRate, &s.avgLatency, &s.p95Latency); err != nil {
			return nil, fmt.Errorf("scan trend row: %w", err)
		}
		s.totalRequests = float64(totalRequests)
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trend rows: %w", err)
	}

	if len(samples) == 0 {
		return []*types.TrendData{}, nil
	}

	metrics := []struct {
		name   string
		select_ func(sample) float64
	}{
		{"avg_latency", func(s sample) float64 { return s.avgLatency }},
		{"p95_latency", func(s sample) float64 { return s.p95Latency }},
		{"success_rate", func(s sample) float64 { return s.successRate }},
		{"total_requests", func(s sample) float64 { return s.totalRequests }},
	}

	period := filter.Interval
	if period == "" {
		period = "raw"
	}

	result := make([]*types.TrendData, 0, len(metrics))
	for _, m := range metrics {
		points := make([]types.TrendPoint, 0, len(samples))
		for _, s := range samples {
			points = append(points, types.TrendPoint{
				Timestamp: s.ts,
				Value:     m.select_(s),
				RunID:     s.runID,
				GitCommit: s.gitCommit,
			})
		}
		first, last := points[0].Value, points[len(points)-1].Value
		var pctChange float64
		if first != 0 {
			pctChange = (last - first) / first * 100
		}
		direction := "stable"
		if pctChange > 1 {
			direction = trendDirectionFor(m.name, true)
		} else if pctChange < -1 {
			direction = trendDirectionFor(m.name, false)
		}
		result = append(result, &types.TrendData{
			Period:        period,
			TrendPoints:   points,
			Direction:     direction,
			PercentChange: pctChange,
		})
	}
	return result, nil
}

// trendDirectionFor maps an absolute change direction onto the
// improving/degrading semantics the dashboard wants. Latency going up
// is bad; success rate going up is good. metricUp == true means the
// last sample is larger than the first.
func trendDirectionFor(metric string, metricUp bool) string {
	higherIsBetter := metric == "success_rate" || metric == "total_requests"
	if metricUp == higherIsBetter {
		return "improving"
	}
	return "degrading"
}

// DeleteHistoricRun removes a run row and best-effort cleans up its
// per-run files under basePath. The caller (HandleDeleteRun) maps a
// "run not found" error to HTTP 404; everything else is a 500.
func (h *HistoricStorage) DeleteHistoricRun(ctx context.Context, runID string) error {
	tx, err := h.db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Drop dependent rows first so the FK-less per-metric table doesn't
	// orphan samples whose run has gone away.
	if _, err := tx.ExecContext(ctx, `DELETE FROM benchmark_metrics WHERE run_id = $1`, runID); err != nil {
		return fmt.Errorf("delete benchmark_metrics: %w", err)
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM benchmark_runs WHERE id = $1`, runID)
	if err != nil {
		return fmt.Errorf("delete benchmark_runs: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("run not found: %s", runID)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if h.basePath != "" {
		runDir := filepath.Join(h.basePath, runID)
		if err := os.RemoveAll(runDir); err != nil && !os.IsNotExist(err) {
			h.log.WithError(err).WithField("run_dir", runDir).Warn("Failed to remove run directory")
		}
	}
	return nil
}

// CompareRuns produces a BaselineComparison-shaped diff between two
// runs: runID1 is treated as the baseline (older / reference) and
// runID2 as the current run under analysis. Per-client/per-method
// average-latency, p95, and success-rate deltas drive the regression /
// improvement lists. The CurrentRun slot is filled from the parsed
// `full_results` blob so the React dashboard sees the same shape it
// gets from a fresh in-memory comparison.
func (h *HistoricStorage) CompareRuns(ctx context.Context, runID1, runID2 string) (*types.BaselineComparison, error) {
	baselineRun, err := h.db.GetRun(runID1)
	if err != nil {
		return nil, fmt.Errorf("get baseline run %s: %w", runID1, err)
	}
	currentRun, err := h.db.GetRun(runID2)
	if err != nil {
		return nil, fmt.Errorf("get current run %s: %w", runID2, err)
	}

	current := &types.BenchmarkResult{}
	if len(currentRun.FullResults) > 0 {
		if err := json.Unmarshal(currentRun.FullResults, current); err != nil {
			h.log.WithError(err).WithField("run_id", runID2).Warn("Failed to parse full_results; comparison will use top-level metrics only")
			current = nil
		}
	}

	baseline := &types.BenchmarkResult{}
	if len(baselineRun.FullResults) > 0 {
		if err := json.Unmarshal(baselineRun.FullResults, baseline); err != nil {
			h.log.WithError(err).WithField("run_id", runID1).Warn("Failed to parse baseline full_results; comparison will use top-level metrics only")
			baseline = nil
		}
	}

	comparison := &types.BaselineComparison{
		BaselineRun: baselineRun,
		CurrentRun:  current,
	}

	if baseline == nil || current == nil {
		// Fall back to the top-level aggregate diff so the API still has
		// something meaningful to return when full_results is missing.
		comparison.Summary = summarizeAggregateDiff(baselineRun, currentRun)
		return comparison, nil
	}

	regressions, improvements := diffClientMetrics(baseline.ClientMetrics, current.ClientMetrics, runID2, runID1)
	comparison.Regressions = regressions
	comparison.Improvements = improvements
	comparison.Summary = fmt.Sprintf("Compared %s -> %s: %d regression(s), %d improvement(s)",
		runID1, runID2, len(regressions), len(improvements))
	return comparison, nil
}

// summarizeAggregateDiff is the fallback summary used when one or both
// FullResults blobs are missing or malformed.
func summarizeAggregateDiff(base, curr *types.HistoricRun) string {
	avgDelta := pctDelta(base.AvgLatency, curr.AvgLatency)
	p95Delta := pctDelta(base.P95Latency, curr.P95Latency)
	succDelta := curr.SuccessRate - base.SuccessRate
	return fmt.Sprintf("avg_latency %+.2f%%, p95_latency %+.2f%%, success_rate %+.2f pp",
		avgDelta, p95Delta, succDelta)
}

func pctDelta(base, curr float64) float64 {
	if base == 0 {
		return 0
	}
	return (curr - base) / base * 100
}

// diffClientMetrics walks the per-client/per-method MetricSummary maps
// and emits a Regression for any (client, method, metric) tuple where
// the current value is materially worse than the baseline. The 5%
// threshold matches the analysis package's RegressionThresholds.LatencyThreshold
// default and the 1pp threshold for success rate matches the API
// contract.
func diffClientMetrics(base, curr map[string]*types.ClientMetrics, runID, baselineRunID string) ([]types.Regression, []types.Improvement) {
	var regressions []types.Regression
	var improvements []types.Improvement
	now := time.Now()

	for clientName, currClient := range curr {
		baseClient, ok := base[clientName]
		if !ok {
			continue
		}
		for methodName, currMethod := range currClient.Methods {
			baseMethod, ok := baseClient.Methods[methodName]
			if !ok {
				continue
			}

			for _, m := range []struct {
				name        string
				base, curr  float64
				higherWorse bool
				threshold   float64
			}{
				{"avg_latency", baseMethod.Avg, currMethod.Avg, true, 5},
				{"p95_latency", baseMethod.P95, currMethod.P95, true, 5},
				{"success_rate", baseMethod.SuccessRate, currMethod.SuccessRate, false, 1},
			} {
				delta := m.curr - m.base
				pct := pctDelta(m.base, m.curr)
				worse := (m.higherWorse && pct > m.threshold) || (!m.higherWorse && delta < -m.threshold)
				better := (m.higherWorse && pct < -m.threshold) || (!m.higherWorse && delta > m.threshold)

				if worse {
					regressions = append(regressions, types.Regression{
						ID:             fmt.Sprintf("%s-%s-%s-%s", runID, clientName, methodName, m.name),
						RunID:          runID,
						BaselineRunID:  baselineRunID,
						Client:         clientName,
						Method:         methodName,
						Metric:         m.name,
						BaselineValue:  m.base,
						CurrentValue:   m.curr,
						AbsoluteChange: delta,
						PercentChange:  pct,
						Severity:       severityFor(pct, m.higherWorse),
						IsSignificant:  true,
						DetectedAt:     now,
					})
				} else if better {
					improvements = append(improvements, types.Improvement{
						ID:             fmt.Sprintf("%s-%s-%s-%s", runID, clientName, methodName, m.name),
						RunID:          runID,
						BaselineRunID:  baselineRunID,
						Client:         clientName,
						Method:         methodName,
						Metric:         m.name,
						BaselineValue:  m.base,
						CurrentValue:   m.curr,
						AbsoluteChange: delta,
						PercentChange:  pct,
						Significance:   significanceFor(pct, m.higherWorse),
						DetectedAt:     now,
					})
				}
			}
		}
	}
	return regressions, improvements
}

func severityFor(pct float64, higherIsWorse bool) string {
	mag := pct
	if !higherIsWorse {
		mag = -pct
	}
	switch {
	case mag > 25:
		return "critical"
	case mag > 15:
		return "high"
	case mag > 5:
		return "medium"
	default:
		return "low"
	}
}

func significanceFor(pct float64, higherIsWorse bool) string {
	mag := pct
	if higherIsWorse {
		mag = -pct
	}
	switch {
	case mag > 20:
		return "significant"
	case mag > 10:
		return "major"
	default:
		return "minor"
	}
}

// GetHistoricSummary aggregates a per-test historic snapshot:
// TotalRuns, FirstRun / LastRun timestamps, BestRun and WorstRun by
// avg_latency (lowest / highest), and a slice of the most recent runs.
// Trends, Regressions and Improvements are left empty here; those are
// produced by dedicated analysers.
func (h *HistoricStorage) GetHistoricSummary(ctx context.Context, filter types.RunFilter) (*types.HistoricSummary, error) {
	allFilter := filter
	allFilter.Limit = 0
	allFilter.Offset = 0
	runs, err := h.db.ListRuns(allFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to list runs for summary: %w", err)
	}

	summary := &types.HistoricSummary{
		TestName:  filter.TestName,
		TotalRuns: len(runs),
	}
	if len(runs) == 0 {
		return summary, nil
	}

	// ListRuns orders by timestamp DESC, so runs[0] is newest, runs[len-1] is oldest.
	summary.LastRun = runs[0].Timestamp
	summary.FirstRun = runs[len(runs)-1].Timestamp

	best := runs[0]
	worst := runs[0]
	for _, r := range runs[1:] {
		if r.AvgLatency < best.AvgLatency {
			best = r
		}
		if r.AvgLatency > worst.AvgLatency {
			worst = r
		}
	}
	summary.BestRun = best
	summary.WorstRun = worst

	const recentRunsCap = 10
	if len(runs) > recentRunsCap {
		summary.RecentRuns = runs[:recentRunsCap]
	} else {
		summary.RecentRuns = runs
	}

	return summary, nil
}

func calculateP99Latency(result *types.BenchmarkResult) float64 {
	var sum float64
	var count int
	for _, clientMetrics := range result.ClientMetrics {
		sum += clientMetrics.Latency.P99
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func calculateMaxLatency(result *types.BenchmarkResult) float64 {
	var max float64
	for _, clientMetrics := range result.ClientMetrics {
		if clientMetrics.Latency.Max > max {
			max = clientMetrics.Latency.Max
		}
	}
	return max
}

func calculateTotalErrors(result *types.BenchmarkResult) int64 {
	var total int64
	for _, clientMetrics := range result.ClientMetrics {
		total += clientMetrics.TotalErrors
	}
	return total
}

func calculatePerformanceScores(result *types.BenchmarkResult) map[string]float64 {
	scores := make(map[string]float64)

	// Calculate overall performance score
	avgLatency := calculateAvgLatency(result)
	successRate := calculateSuccessRate(result)
	totalRequests := calculateTotalRequests(result)

	// Simple scoring algorithm - can be enhanced
	latencyScore := 100.0 / (1.0 + avgLatency/100.0)   // Lower latency = higher score
	errorScore := successRate                          // Higher success rate = higher score
	throughputScore := float64(totalRequests) / 1000.0 // Higher throughput = higher score

	overallScore := (latencyScore*0.4 + errorScore*0.4 + throughputScore*0.2)
	if overallScore > 100 {
		overallScore = 100
	}

	scores["overall"] = overallScore

	// Calculate per-client scores
	for clientName, clientMetrics := range result.ClientMetrics {
		clientLatencyScore := 100.0 / (1.0 + clientMetrics.Latency.Avg/100.0)
		clientErrorScore := 100.0 - clientMetrics.ErrorRate
		clientThroughputScore := clientMetrics.Latency.Throughput / 100.0

		clientOverallScore := (clientLatencyScore*0.4 + clientErrorScore*0.4 + clientThroughputScore*0.2)
		if clientOverallScore > 100 {
			clientOverallScore = 100
		}

		scores["client_"+clientName] = clientOverallScore
	}

	return scores
}
