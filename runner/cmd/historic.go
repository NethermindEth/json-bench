package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/generator"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

var (
	historicConfigPath        string
	historicStorageConfigPath string
)

var historicCmd = &cobra.Command{
	Use:   "historic",
	Short: "Generate a historic-analysis report from PostgreSQL",
	RunE:  runHistoric,
}

func init() {
	historicCmd.Flags().StringVar(&historicConfigPath, "config", "", "Path to YAML configuration file")
	historicCmd.Flags().StringVar(&historicStorageConfigPath, "storage-config", "", "Path to storage configuration file")
	_ = historicCmd.MarkFlagRequired("storage-config")
}

func runHistoric(cmd *cobra.Command, args []string) error {
	configureLogger()

	if historicConfigPath == "" {
		return fmt.Errorf("--config is required")
	}

	registry, err := loadClientRegistry("")
	if err != nil {
		return err
	}
	cfg, err := loadBenchmarkConfig(historicConfigPath, "", registry)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	historic, db, err := openHistoricStorage(historicStorageConfigPath)
	if err != nil {
		return err
	}
	defer db.Close()

	return generateHistoricReport(cfg.TestName, historic, outputDir)
}

func generateHistoricReport(testName string, historic *storage.HistoricStorage, outDir string) error {
	ctx := context.Background()
	logger.Info("Running historic analysis mode")

	if testName == "" {
		testName = "unknown"
	}

	summary, err := historic.GetHistoricSummary(ctx, types.RunFilter{TestName: testName, Limit: 100})
	if err != nil {
		return fmt.Errorf("failed to get historic summary: %w", err)
	}
	logger.WithFields(logrus.Fields{"total_runs": summary.TotalRuns}).Info("Historic summary retrieved")

	trendData, err := historic.GetHistoricTrends(ctx, types.TrendFilter{Since: time.Now().AddDate(0, 0, -30)})
	if err != nil {
		logger.WithError(err).Warn("Failed to get historic trends")
	}

	recentRuns, err := historic.ListHistoricRuns(ctx, types.RunFilter{TestName: testName, Limit: 10})
	if err != nil {
		logger.WithError(err).Warn("Failed to get recent runs")
	}

	if err := generator.GenerateHistoricAnalysisReport(summary, trendData, recentRuns, outDir); err != nil {
		return fmt.Errorf("failed to generate historic analysis report: %w", err)
	}

	logger.Info("Historic analysis completed successfully")
	return nil
}
