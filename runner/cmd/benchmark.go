package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/analyzer"
	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/exporter"
	"github.com/jsonrpc-bench/runner/generator"
	"github.com/jsonrpc-bench/runner/metrics"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

var (
	benchmarkConfigPath        string
	benchmarkClientsPath       string
	benchmarkPrometheusRW      string
	benchmarkPrometheusRWUser  string
	benchmarkPrometheusRWPass  string
	benchmarkEnableHistoric    bool
	benchmarkStorageConfigPath string
)

var benchmarkCmd = &cobra.Command{
	Use:   "benchmark",
	Short: "Run a benchmark (k6 → Prometheus → reports)",
	RunE:  runBenchmark,
}

func init() {
	benchmarkCmd.Flags().StringVar(&benchmarkConfigPath, "config", "", "Path to YAML configuration file")
	benchmarkCmd.Flags().StringVar(&benchmarkClientsPath, "clients", "", "Path to clients configuration file (optional)")
	benchmarkCmd.Flags().StringVar(&benchmarkPrometheusRW, "prometheus-rw", "http://localhost:9090/api/v1/write", "Prometheus remote write endpoint for metrics")
	benchmarkCmd.Flags().StringVar(&benchmarkPrometheusRWUser, "prometheus-rw-user", "", "Prometheus remote write username (optional)")
	benchmarkCmd.Flags().StringVar(&benchmarkPrometheusRWPass, "prometheus-rw-pass", "", "Prometheus remote write password (optional)")
	benchmarkCmd.Flags().BoolVar(&benchmarkEnableHistoric, "historic", false, "Persist this run to historic storage")
	benchmarkCmd.Flags().StringVar(&benchmarkStorageConfigPath, "storage-config", "", "Path to storage configuration file (required with --historic)")
}

func runBenchmark(cmd *cobra.Command, args []string) error {
	configureLogger()

	if benchmarkConfigPath == "" {
		return fmt.Errorf("--config is required")
	}
	if benchmarkPrometheusRW == "" {
		return fmt.Errorf("--prometheus-rw is required")
	}
	if benchmarkEnableHistoric && benchmarkStorageConfigPath == "" {
		return fmt.Errorf("--storage-config is required when --historic is set")
	}

	registry, err := loadClientRegistry(benchmarkClientsPath)
	if err != nil {
		return err
	}

	cfg, err := loadBenchmarkConfig(benchmarkConfigPath, benchmarkClientsPath, registry)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	cfg.Outputs = &config.Outputs{
		PrometheusRW: &config.PrometheusRW{
			Endpoint: benchmarkPrometheusRW,
			BasicAuth: config.BasicAuth{
				Username: benchmarkPrometheusRWUser,
				Password: benchmarkPrometheusRWPass,
			},
		},
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	var historic *storage.HistoricStorage
	if benchmarkEnableHistoric {
		h, db, err := openHistoricStorage(benchmarkStorageConfigPath)
		if err != nil {
			return err
		}
		defer db.Close()
		historic = h
		logger.Info("Historic storage initialized successfully")
	}

	k6Cmd, summaryPath, err := generator.GenerateK6(cfg, outputDir)
	if err != nil {
		return fmt.Errorf("failed to generate k6 command: %w", err)
	}

	systemCollector, err := metrics.NewSystemCollector(1 * time.Second)
	if err != nil {
		logger.WithError(err).Warn("Failed to create system collector")
	} else {
		systemCollector.Start()
		defer systemCollector.Stop()
	}

	logger.Info("Running benchmark")
	startTime := time.Now()
	runErr := k6Cmd.Run()
	endTime := time.Now()
	testDuration := endTime.Sub(startTime)
	if runErr != nil {
		logger.WithError(runErr).Warn("K6 command execution completed with errors")
	} else {
		logger.WithField("summary_path", summaryPath).Info("K6 command executed successfully")
	}

	k6SummaryRaw, err := os.ReadFile(summaryPath)
	if err != nil {
		logger.WithError(err).Warn("Failed to read k6 summary file")
	}

	var k6Summary map[string]any
	if err := json.Unmarshal(k6SummaryRaw, &k6Summary); err != nil {
		logger.WithError(err).Warn("Failed to unmarshal k6 summary")
	}

	clientsMetrics, err := metrics.CollectClientsMetrics(cfg, endTime, summaryPath)
	if err != nil {
		logger.WithError(err).Warn("Failed to collect benchmark clients metrics")
	}

	logP99Validation(clientsMetrics)

	benchmarkResults := &types.BenchmarkResult{
		Summary:       k6Summary,
		ClientMetrics: clientsMetrics,
		Timestamp:     time.Now().Format(time.DateTime),
		StartTime:     startTime.Format(time.DateTime),
		EndTime:       endTime.Format(time.DateTime),
		Duration:      testDuration.String(),
		ResponsesDir:  outputDir,
	}

	if systemCollector != nil {
		avgMetrics := systemCollector.GetAverageMetrics()
		for _, client := range benchmarkResults.ClientMetrics {
			client.SystemMetrics = []types.SystemMetrics{avgMetrics}
		}
	}

	benchmarkResults.Environment = metrics.GetEnvironmentInfo()

	performanceAnalyzer := analyzer.NewPerformanceAnalyzer()
	performanceAnalyzer.AnalyzeResults(benchmarkResults)

	if historic != nil {
		savedRun, err := historic.SaveRun(benchmarkResults, cfg)
		if err != nil {
			logger.WithError(err).Error("Failed to save historic run")
		} else {
			logger.WithField("run_id", savedRun.ID).Info("Historic run saved successfully")
		}
	}

	reportPath := filepath.Join(outputDir, "report.html")
	if err := generator.GenerateUltimateHTMLReport(cfg, benchmarkResults, reportPath); err != nil {
		logger.Warnf("Ultimate report generation failed, falling back to enhanced report: %v", err)
		if err := generator.GenerateEnhancedHTMLReport(cfg, benchmarkResults, reportPath); err != nil {
			logger.Warnf("Enhanced report generation failed, falling back to basic report: %v", err)
			if err := generator.GenerateHTMLReport(cfg, benchmarkResults, reportPath); err != nil {
				return fmt.Errorf("failed to generate HTML report: %w", err)
			}
		}
	}
	logger.Infof("Generated HTML report at: %s", reportPath)

	dataExporter := exporter.NewDataExporter(outputDir)
	if err := dataExporter.ExportAll(benchmarkResults); err != nil {
		logger.Warnf("Failed to export data: %v", err)
	} else {
		logger.Info("Exported data to CSV and JSON formats")
	}

	logger.Info("Benchmark completed")
	return nil
}

func logP99Validation(clientsMetrics map[string]*types.ClientMetrics) {
	totalMethods := 0
	methodsWithP99 := 0
	methodsWithZeroP99 := 0

	for clientName, client := range clientsMetrics {
		for methodName, method := range client.Methods {
			totalMethods++
			if method.P99 > 0 {
				methodsWithP99++
			} else if method.Count > 0 {
				methodsWithZeroP99++
				logger.Warnf("Method %s.%s has zero p99 value (count: %d, avg: %.2f)",
					clientName, methodName, method.Count, method.Avg)
			}
		}
	}

	if totalMethods > 0 {
		p99Coverage := float64(methodsWithP99) / float64(totalMethods) * 100
		logger.WithFields(logrus.Fields{
			"methods_with_p99": methodsWithP99,
			"total_methods":    totalMethods,
		}).Infof("P99 validation summary: %.1f%% coverage", p99Coverage)
		if methodsWithZeroP99 > 0 {
			logger.Warnf("%d methods with actual traffic have p99=0", methodsWithZeroP99)
		}
	}
}
