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
func NewHistoricStorage(cfg *config.StorageConfig) (*HistoricStorage, error) {
	log := logrus.WithField("component", "historic_storage")

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
		h.log.WithError(err).Error("Failed to save metrics to PostgreSQL")
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

// GetHistoricTrends retrieves historic trend data (placeholder implementation)
func (h *HistoricStorage) GetHistoricTrends(ctx context.Context, filter types.TrendFilter) ([]*types.TrendData, error) {
	// Placeholder implementation - in a full implementation this would
	// query aggregated trend data from the database
	return []*types.TrendData{}, nil
}

// DeleteHistoricRun deletes a historic run (placeholder implementation)
func (h *HistoricStorage) DeleteHistoricRun(ctx context.Context, runID string) error {
	// Placeholder implementation
	return nil
}

// CompareRuns compares two historic runs (placeholder implementation)
func (h *HistoricStorage) CompareRuns(ctx context.Context, runID1, runID2 string) (*types.BaselineComparison, error) {
	// Placeholder implementation
	return &types.BaselineComparison{}, nil
}

// GetHistoricSummary retrieves a summary of historic data (placeholder implementation)
func (h *HistoricStorage) GetHistoricSummary(ctx context.Context, filter types.RunFilter) (*types.HistoricSummary, error) {
	// Placeholder implementation
	return &types.HistoricSummary{}, nil
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
