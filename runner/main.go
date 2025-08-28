package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/analyzer"
	"github.com/jsonrpc-bench/runner/api"
	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/exporter"
	"github.com/jsonrpc-bench/runner/generator"
	"github.com/jsonrpc-bench/runner/metrics"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "", "Path to YAML configuration file")
	clientsPath := flag.String("clients", "", "Path to clients configuration file (optional)")
	outputDir := flag.String("output", "outputs", "Directory to store outputs")
	prometheusRWEndpoint := flag.String("prometheus-rw", "http://localhost:9090", "Prometheus remote write endpoint for metrics")
	prometheusRWUsername := flag.String("prometheus-rw-user", "", "Prometheus remote write username for basic authentication (optional)")
	prometheusRWPassword := flag.String("prometheus-rw-pass", "", "Prometheus remote write password for basic authentication (optional)")
	// compareResponses := flag.Bool("compare", false, "Compare JSON-RPC responses across clients")
	// validateSchema := flag.Bool("validate", true, "Validate responses against OpenRPC schema")
	// concurrency := flag.Int("concurrency", 5, "Number of concurrent requests for comparison")
	// timeout := flag.Int("timeout", 30, "Request timeout in seconds for comparison")

	// Historic storage flags
	enableHistoric := flag.Bool("historic", false, "Enable historic data storage and analysis")
	storageConfig := flag.String("storage-config", "", "Path to storage configuration file")
	historicMode := flag.Bool("historic-mode", false, "Run in historic analysis mode (no new benchmark)")

	// API server flags
	apiAddr := flag.String("api-addr", ":8081", "Address to bind the API server to")
	apiMode := flag.Bool("api", false, "Run in API server mode (HTTP server + WebSocket)")

	flag.Parse()

	// Initialize logger
	logger := logrus.New()

	// Set log level from environment variable
	logLevel := os.Getenv("LOG_LEVEL")
	switch logLevel {
	case "debug":
		logger.SetLevel(logrus.DebugLevel)
	case "warn", "warning":
		logger.SetLevel(logrus.WarnLevel)
	case "error":
		logger.SetLevel(logrus.ErrorLevel)
	default:
		logger.SetLevel(logrus.InfoLevel)
	}

	// API server mode
	if *apiMode {
		if err := runAPIServer(*storageConfig, *apiAddr, logger); err != nil {
			logger.WithError(err).Fatal("api server failed")
		}
		return
	}

	if *configPath == "" {
		logger.Fatal("Please provide a configuration file path using -config flag")
	}

	// Initialize client registry
	clientRegistry := config.NewClientRegistry()

	// Load clients configuration if provided
	if *clientsPath != "" {
		if err := clientRegistry.LoadFromFile(*clientsPath); err != nil {
			logger.WithError(err).Fatal("Failed to load clients configuration")
		}
		logger.Info("Loaded clients configuration from ", *clientsPath)
	}

	// Create config loader
	configLoader := config.NewConfigLoader(clientRegistry)

	// Load configuration with backward compatibility support
	var cfg *config.Config
	var err error
	if *clientsPath != "" {
		// Use new style loading when clients file is provided
		cfg, err = configLoader.LoadTestConfig(*configPath)
	} else {
		// Use backward compatibility loading when no clients file is provided
		cfg, err = configLoader.LoadWithBackwardCompatibility(*configPath)
	}
	if err != nil {
		logger.WithError(err).Fatal("failed to load configuration")
	}

	// Add outputs to the config
	cfg.Outputs = new(config.Outputs)
	if *prometheusRWEndpoint != "" {
		cfg.Outputs.PrometheusRW = &config.PrometheusRW{
			Endpoint: *prometheusRWEndpoint,
			BasicAuth: config.BasicAuth{
				Username: *prometheusRWUsername,
				Password: *prometheusRWPassword,
			},
		}
	} else {
		logger.Fatal("No metrics outputs configured")
	}

	// Initialize historic storage if enabled
	var historicStorage *storage.HistoricStorage
	if *enableHistoric || *historicMode {
		if *storageConfig == "" {
			logger.Fatal("Storage configuration path is required when historic mode is enabled. Use -storage-config flag")
		}

		storageCfg, err := config.LoadStorageConfig(*storageConfig, logger)
		if err != nil {
			logger.WithError(err).Fatal("Failed to load storage configuration")
		}

		if storageCfg.EnableHistoric {
			// Initialize PostgreSQL storage
			db, err := sql.Open("postgres", storageCfg.PostgreSQL.ConnectionString())
			if err != nil {
				logger.WithError(err).Fatal("Failed to connect to postgres database")
			}
			defer db.Close()

			// Test connection
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				logger.WithError(err).Fatal("Failed to ping postgres database")
			}

			// Run migrations
			if err := storage.RunMigrations(db, logger); err != nil {
				logger.WithError(err).Fatal("Failed to run database migrations")
			}

			// Initialize historic storage
			historicStorage, err = storage.NewHistoricStorage(storageCfg, logger)
			if err != nil {
				logger.WithError(err).Fatal("Failed to create historic storage")
			}

			logger.Info("Historic storage initialized successfully")
		} else {
			logger.Fatal("Historic storage must be enabled in configuration")
		}
	}

	// Handle historic mode (analysis only, no new benchmark)
	if *historicMode {
		if err := runHistoricAnalysis(cfg, historicStorage, *outputDir, logger); err != nil {
			logger.WithError(err).Fatal("Historic analysis failed")
		}
		return
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		logger.WithError(err).Fatal("Failed to create output directory")
	}

	// Generate k6 script

	k6Cmd, summaryPath, err := generator.GenerateK6(cfg, *outputDir)
	if err != nil {
		logger.WithError(err).Fatal("Failed to generate k6 command")
	}

	// Start system metrics collection
	systemCollector, err := metrics.NewSystemCollector(1 * time.Second)
	if err != nil {
		logger.WithError(err).Warn("Failed to create system collector")
	} else {
		systemCollector.Start()
		defer systemCollector.Stop()
	}

	// Run k6 benchmark
	logger.Info("Running benchmark")
	startTime := time.Now()
	err = k6Cmd.Run()
	endTime := time.Now()
	testDuration := endTime.Sub(startTime)
	if err != nil {
		logger.WithError(err).Warn("K6 command execution completed with errors")
	} else {
		logger.WithField("summary_path", summaryPath).Info("K6 command executed successfully")
	}

	// Collect k6 summary
	k6SummaryRaw, err := os.ReadFile(summaryPath)
	if err != nil {
		logger.WithError(err).Warn("Failed to read k6 summary file")
	}

	var k6Summary map[string]any
	if err := json.Unmarshal([]byte(k6SummaryRaw), &k6Summary); err != nil {
		logger.WithError(err).Warn("Failed to unmarshal k6 summary")
	}

	// Collect benchmark results
	clientsMetrics, err := metrics.CollectClientsMetrics(cfg, endTime, summaryPath)
	if err != nil {
		logger.WithError(err).Warn("Failed to collect benchmark clients metrics")
	}

	// Log summary of p99 validation
	logP99Validation(clientsMetrics, logger)

	benchmarkResults := &types.BenchmarkResult{
		Summary:       k6Summary,
		ClientMetrics: clientsMetrics,
		Timestamp:     time.Now().Format(time.DateTime),
		StartTime:     startTime.Format(time.DateTime),
		EndTime:       endTime.Format(time.DateTime),
		Duration:      testDuration.String(),
		ResponsesDir:  *outputDir,
	}

	// Add system metrics to results if available
	if systemCollector != nil {
		avgMetrics := systemCollector.GetAverageMetrics()
		// Add system metrics to each client
		for _, client := range benchmarkResults.ClientMetrics {
			client.SystemMetrics = []types.SystemMetrics{avgMetrics}
		}
	}

	// Add environment info
	benchmarkResults.Environment = metrics.GetEnvironmentInfo()

	// Perform performance analysis
	performanceAnalyzer := analyzer.NewPerformanceAnalyzer()
	performanceAnalyzer.AnalyzeResults(benchmarkResults)

	// Save to historic storage if enabled
	if *enableHistoric && historicStorage != nil {
		savedRun, err := historicStorage.SaveRun(benchmarkResults, cfg)
		if err != nil {
			logger.WithError(err).Error("Failed to save historic run")
		} else {
			logger.WithField("run_id", savedRun.ID).Info("Historic run saved successfully")
		}
	}

	// Generate ultimate HTML report
	reportPath := filepath.Join(*outputDir, "report.html")
	if err := generator.GenerateUltimateHTMLReport(cfg, benchmarkResults, reportPath); err != nil {
		// Fallback to enhanced report if ultimate fails
		logger.Warnf("Ultimate report generation failed, falling back to enhanced report: %v", err)
		if err := generator.GenerateEnhancedHTMLReport(cfg, benchmarkResults, reportPath); err != nil {
			// Fallback to old report generator if enhanced fails
			logger.Warnf("Enhanced report generation failed, falling back to basic report: %v", err)
			if err := generator.GenerateHTMLReport(cfg, benchmarkResults, reportPath); err != nil {
				logger.Fatalf("Failed to generate HTML report: %v", err)
			}
		}
	}
	logger.Infof("Generated HTML report at: %s", reportPath)

	// Export data in multiple formats
	dataExporter := exporter.NewDataExporter(*outputDir)
	if err := dataExporter.ExportAll(benchmarkResults); err != nil {
		logger.Warnf("Failed to export data: %v", err)
	} else {
		logger.Info("Exported data to CSV and JSON formats")
	}

	logger.Info("Benchmark completed")

	// // Run response comparison if enabled
	// if *compareResponses {
	// 	fmt.Println("\nStarting JSON-RPC response comparison...")
	// 	if err := runComparison(cfg, *outputDir, *validateSchema, *concurrency, *timeout); err != nil {
	// 		log.Printf("Response comparison warning: %v", err)
	// 	} else {
	// 		fmt.Println("Response comparison completed successfully!")
	// 	}
	// }
}

// logP99Validation logs a summary of p99 validation
func logP99Validation(clientsMetrics map[string]*types.ClientMetrics, logger *logrus.Logger) {
	totalMethods := 0
	methodsWithP99 := 0
	methodsWithZeroP99 := 0

	for clientName, client := range clientsMetrics {
		for methodName, method := range client.Methods {
			totalMethods++
			if method.P99 > 0 {
				methodsWithP99++
			} else if method.Count > 0 {
				// Only count as zero if there were actual calls
				methodsWithZeroP99++
				logger.Warnf("Method %s.%s has zero p99 value (count: %d, avg: %.2f)",
					clientName, methodName, method.Count, method.Avg)
			}
		}
	}

	if totalMethods > 0 {
		p99Coverage := float64(methodsWithP99) / float64(totalMethods) * 100
		logger.Infof("P99 validation summary: %d/%d methods have p99 values (%.1f%% coverage)",
			methodsWithP99, totalMethods, p99Coverage)
		if methodsWithZeroP99 > 0 {
			logger.Warnf("%d methods with actual traffic have p99=0", methodsWithZeroP99)
		}
	}
}

// runHistoricAnalysis runs historic analysis mode
func runHistoricAnalysis(cfg *config.Config, historicStorage *storage.HistoricStorage, outputDir string, logger *logrus.Logger) error {
	ctx := context.Background()

	logger.Info("Running historic analysis mode")

	// Extract test name from config
	testName := cfg.TestName
	if testName == "" {
		testName = "unknown"
	}

	// Get historic summary
	filter := types.RunFilter{
		TestName: testName,
		Limit:    100,
	}
	summary, err := historicStorage.GetHistoricSummary(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to get historic summary: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"total_runs": summary.TotalRuns,
	}).Info("Historic summary retrieved")

	// Get recent trends for key metrics using TrendFilter
	trendFilter := types.TrendFilter{
		Since: time.Now().AddDate(0, 0, -30),
	}
	trendData, err := historicStorage.GetHistoricTrends(ctx, trendFilter)
	if err != nil {
		logger.WithError(err).Warn("Failed to get historic trends")
	}
	// Get recent runs for comparison
	filter2 := types.RunFilter{
		TestName: testName,
		Limit:    10,
	}
	recentRuns, err := historicStorage.ListHistoricRuns(ctx, filter2)
	if err != nil {
		logger.WithError(err).Warn("Failed to get recent runs")
	}

	// Generate historic analysis report
	if err := generator.GenerateHistoricAnalysisReport(summary, trendData, recentRuns, outputDir); err != nil {
		return fmt.Errorf("failed to generate historic analysis report: %w", err)
	}

	logger.Info("Historic analysis completed successfully")
	return nil
}

// runAPIServer runs the HTTP API server for serving historic data
func runAPIServer(storageConfigPath string, apiAddr string, logger *logrus.Logger) error {
	if storageConfigPath == "" {
		return fmt.Errorf("storage configuration path is required for API server mode")
	}

	// Load storage configuration
	storageCfg, err := config.LoadStorageConfig(storageConfigPath, logger)
	if err != nil {
		return fmt.Errorf("failed to load storage configuration: %w", err)
	}

	// Initialize PostgreSQL database
	db, err := sql.Open("postgres", storageCfg.PostgreSQL.ConnectionString())
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer db.Close()

	// Test database connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping postgres database: %w", err)
	}

	// Run migrations
	if err := storage.RunMigrations(db, logger); err != nil {
		return fmt.Errorf("failed to run database migrations: %w", err)
	}

	// Initialize historic storage
	historicStorage, err := storage.NewHistoricStorage(storageCfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create historic storage: %w", err)
	}

	// Initialize analysis components
	baselineManager := analysis.NewBaselineManager(*historicStorage, db, logger)
	trendAnalyzer := analysis.NewTrendAnalyzer(*historicStorage, db, logger)
	regressionDetector := analysis.NewRegressionDetector(*historicStorage, baselineManager, db, logger)

	// Create API server
	apiServer := api.NewServer(
		apiAddr,
		*historicStorage,
		baselineManager,
		trendAnalyzer,
		regressionDetector,
		db,
		logger,
	)

	// Create context for graceful shutdown
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// Start API server
	if err := apiServer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start api server: %w", err)
	}
	logger.Info("Press Ctrl+C to stop the server")

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for interrupt signal
	select {
	case <-sigChan:
		logger.Info("Received shutdown signal, shutting down API server...")
		return apiServer.Stop()
	case <-ctx.Done():
		logger.Info("Context cancelled, shutting down API server...")
		return apiServer.Stop()
	}
}
