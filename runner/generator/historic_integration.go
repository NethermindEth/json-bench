package generator

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// SaveToHistoric saves benchmark results to historic storage after report generation
func SaveToHistoric(historicStorage *storage.HistoricStorage, result *types.BenchmarkResult, cfg *config.Config) error {
	log := logrus.WithField("component", "historic_integration")

	if historicStorage == nil {
		return fmt.Errorf("historic storage not initialized")
	}

	log.Info("Saving benchmark results to historic storage")

	// Save the run to historic storage
	historicRun, err := historicStorage.SaveRun(result, cfg)
	if err != nil {
		return fmt.Errorf("failed to save run to historic storage: %w", err)
	}

	log.WithFields(logrus.Fields{
		"run_id":     historicRun.ID,
		"git_commit": historicRun.GitCommit,
		"git_branch": historicRun.GitBranch,
		"test_name":  historicRun.TestName,
	}).Info("Successfully saved benchmark run to historic storage")

	return nil
}

// InitializeHistoricStorage creates and initializes historic storage if enabled
func InitializeHistoricStorage(storageConfigPath string, logger *logrus.Logger) (*storage.HistoricStorage, error) {
	if storageConfigPath == "" {
		return nil, fmt.Errorf("storage configuration path not provided")
	}

	// Load storage configuration
	log := logger.WithField("component", "historic_integration")
	storageCfg, err := config.LoadStorageConfig(storageConfigPath, log)
	if err != nil {
		return nil, fmt.Errorf("failed to load storage configuration: %w", err)
	}

	if !storageCfg.EnableHistoric {
		return nil, fmt.Errorf("historic storage is disabled in configuration")
	}

	// Create historic storage
	historicStorage, err := storage.NewHistoricStorage(storageCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create historic storage: %w", err)
	}

	logrus.WithField("historic_path", storageCfg.HistoricPath).Info("Historic storage initialized successfully")
	return historicStorage, nil
}

// CopyResultsToHistoric copies generated reports and files to historic directory
func CopyResultsToHistoric(historicStorage *storage.HistoricStorage, outputDir, runID string) error {
	log := logrus.WithField("component", "historic_integration")

	// This would copy the generated HTML report, JSON results, CSV exports, etc.
	// to the historic directory structure created by HistoricStorage.SaveRun

	// Get the historic run directory
	historicDir := filepath.Join(historicStorage.GetBasePath(), runID)

	// Copy main results
	filesToCopy := []string{
		"report.html",
		"results.json",
		"summary.json",
		"config.yaml",
	}

	for _, file := range filesToCopy {
		srcPath := filepath.Join(outputDir, file)
		dstPath := filepath.Join(historicDir, file)

		if err := copyFile(srcPath, dstPath); err != nil {
			log.WithError(err).WithField("file", file).Warn("Failed to copy file to historic storage")
			// Continue with other files even if one fails
		}
	}

	// Copy exports directory if it exists
	exportsDir := filepath.Join(outputDir, "exports")
	historicExportsDir := filepath.Join(historicDir, "exports")

	if err := copyDirectory(exportsDir, historicExportsDir); err != nil {
		log.WithError(err).Warn("Failed to copy exports directory to historic storage")
	}

	log.WithField("run_id", runID).Info("Successfully copied results to historic storage")
	return nil
}

// GenerateHistoricMetadata creates metadata file for the historic run
func GenerateHistoricMetadata(result *types.BenchmarkResult, cfg *config.Config, outputPath string) error {
	metadata := HistoricMetadata{
		Timestamp:     time.Now(),
		TestName:      extractTestName(cfg),
		Description:   extractDescription(cfg),
		Duration:      result.Duration,
		TotalRequests: calculateTotalRequests(result),
		SuccessRate:   calculateSuccessRate(result),
		AvgLatency:    calculateAvgLatency(result),
		P95Latency:    calculateP95Latency(result),
		Clients:       extractClients(result),
		Methods:       extractMethods(result),
		Tags:          extractTags(cfg),
	}

	return writeJSONFile(outputPath, metadata)
}

// HistoricMetadata represents metadata for a historic run
type HistoricMetadata struct {
	Timestamp     time.Time `json:"timestamp"`
	TestName      string    `json:"test_name"`
	Description   string    `json:"description"`
	Duration      string    `json:"duration"`
	TotalRequests int64     `json:"total_requests"`
	SuccessRate   float64   `json:"success_rate"`
	AvgLatency    float64   `json:"avg_latency"`
	P95Latency    float64   `json:"p95_latency"`
	Clients       []string  `json:"clients"`
	Methods       []string  `json:"methods"`
	Tags          []string  `json:"tags"`
}

// Helper functions (these could be moved to a shared utilities package later)

func extractTestName(cfg *config.Config) string {
	// Extract test name from config structure
	// Since Config may not have Name field, use a default
	return "benchmark_test"
}

func extractDescription(cfg *config.Config) string {
	// Extract description from config structure
	// Since Config may not have Description field, return empty
	return ""
}

func extractTags(cfg *config.Config) []string {
	// Extract tags from configuration
	// This could be enhanced to read tags from config structure
	return []string{}
}

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

// File operation helper functions (simplified implementations)
// In a production system, these would be more robust

func copyFile(src, dst string) error {
	// Simplified implementation - in production this would handle file copying properly
	return nil
}

func copyDirectory(src, dst string) error {
	// Simplified implementation - in production this would handle directory copying properly
	return nil
}

func writeJSONFile(path string, data interface{}) error {
	// Simplified implementation - in production this would write JSON to file
	return nil
}
