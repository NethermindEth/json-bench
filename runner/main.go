package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/analyzer"
	"github.com/jsonrpc-bench/runner/api"
	"github.com/jsonrpc-bench/runner/comparator"
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
	outputDir := flag.String("output", "results", "Directory to store results")
	compareResponses := flag.Bool("compare", false, "Compare JSON-RPC responses across clients")
	validateSchema := flag.Bool("validate", true, "Validate responses against OpenRPC schema")
	concurrency := flag.Int("concurrency", 5, "Number of concurrent requests for comparison")
	timeout := flag.Int("timeout", 30, "Request timeout in seconds for comparison")

	// Historic storage flags
	enableHistoric := flag.Bool("historic", false, "Enable historic data storage and analysis")
	storageConfig := flag.String("storage-config", "", "Path to storage configuration file")
	historicMode := flag.Bool("historic-mode", false, "Run in historic analysis mode (no new benchmark)")

	// API server flags
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
		if err := runAPIServer(*storageConfig, logger); err != nil {
			log.Fatalf("API server failed: %v", err)
		}
		return
	}

	if *configPath == "" && !*historicMode {
		log.Fatal("Please provide a configuration file path using -config flag")
	}

	// Initialize client registry
	clientRegistry := config.NewClientRegistry()

	// Load clients configuration if provided
	if *clientsPath != "" {
		if err := clientRegistry.LoadFromFile(*clientsPath); err != nil {
			log.Fatalf("Failed to load clients configuration: %v", err)
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
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize historic storage if enabled
	var historicStorage *storage.HistoricStorage
	if *enableHistoric || *historicMode {
		if *storageConfig == "" {
			log.Fatal("Storage configuration path is required when historic mode is enabled. Use -storage-config flag.")
		}

		storageCfg, err := config.LoadStorageConfig(*storageConfig, logger)
		if err != nil {
			log.Fatalf("Failed to load storage configuration: %v", err)
		}

		if storageCfg.EnableHistoric {
			// Initialize PostgreSQL storage
			db, err := sql.Open("postgres", storageCfg.PostgreSQL.ConnectionString())
			if err != nil {
				log.Fatalf("Failed to connect to PostgreSQL: %v", err)
			}
			defer db.Close()

			// Test connection
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				log.Fatalf("Failed to ping PostgreSQL database: %v", err)
			}

			// Run migrations
			if err := storage.RunMigrations(db); err != nil {
				log.Fatalf("Failed to run database migrations: %v", err)
			}

			// Initialize historic storage
			historicStorage, err = storage.NewHistoricStorage(storageCfg)
			if err != nil {
				log.Fatalf("Failed to create historic storage: %v", err)
			}

			logger.Info("Historic storage initialized successfully")
		} else {
			log.Fatal("Historic storage must be enabled in configuration")
		}
	}

	// Handle historic mode (analysis only, no new benchmark)
	if *historicMode {
		if err := runHistoricAnalysis(cfg, historicStorage, *outputDir, logger); err != nil {
			log.Fatalf("Historic analysis failed: %v", err)
		}
		return
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Generate k6 script
	scriptPath := filepath.Join(*outputDir, "k6-script.js")
	if err := generator.GenerateK6Script(cfg, scriptPath); err != nil {
		log.Fatalf("Failed to generate k6 script: %v", err)
	}
	fmt.Printf("Generated k6 script at: %s\n", scriptPath)

	// Start system metrics collection
	systemCollector, err := metrics.NewSystemCollector(1 * time.Second)
	if err != nil {
		log.Printf("Warning: Failed to create system collector: %v", err)
	} else {
		systemCollector.Start()
		defer systemCollector.Stop()
	}

	// Run k6 benchmark
	fmt.Println("Running benchmark...")
	results, err := generator.RunK6Benchmark(scriptPath, *outputDir)
	if err != nil {
		// Log the error but continue to generate the report
		log.Printf("Benchmark execution warning: %v", err)
	}

	// Add system metrics to results if available
	if systemCollector != nil {
		avgMetrics := systemCollector.GetAverageMetrics()
		// Add system metrics to each client
		for _, client := range results.ClientMetrics {
			client.SystemMetrics = []types.SystemMetrics{avgMetrics}
		}
	}

	// Add environment info
	results.Environment = metrics.GetEnvironmentInfo()

	// Perform performance analysis
	performanceAnalyzer := analyzer.NewPerformanceAnalyzer()
	performanceAnalyzer.AnalyzeResults(results)

	// Save to historic storage if enabled
	if *enableHistoric && historicStorage != nil {
		savedRun, err := historicStorage.SaveRun(results, cfg)
		if err != nil {
			logger.WithError(err).Error("Failed to save historic run")
		} else {
			logger.WithField("run_id", savedRun.ID).Info("Historic run saved successfully")
		}
	}

	// Generate ultimate HTML report
	reportPath := filepath.Join(*outputDir, "report.html")
	if err := generator.GenerateUltimateHTMLReport(cfg, results, reportPath); err != nil {
		// Fallback to enhanced report if ultimate fails
		log.Printf("Warning: Ultimate report generation failed, falling back to enhanced report: %v", err)
		if err := generator.GenerateEnhancedHTMLReport(cfg, results, reportPath); err != nil {
			// Fallback to old report generator if enhanced fails
			log.Printf("Warning: Enhanced report generation failed, falling back to basic report: %v", err)
			if err := generator.GenerateHTMLReport(cfg, results, reportPath); err != nil {
				log.Fatalf("Failed to generate HTML report: %v", err)
			}
		}
	}
	fmt.Printf("Generated HTML report at: %s\n", reportPath)

	// Export data in multiple formats
	dataExporter := exporter.NewDataExporter(*outputDir)
	if err := dataExporter.ExportAll(results); err != nil {
		log.Printf("Warning: Failed to export data: %v", err)
	} else {
		fmt.Println("Exported data to CSV and JSON formats")
	}

	fmt.Println("Benchmark completed successfully!")

	// Run response comparison if enabled
	if *compareResponses {
		fmt.Println("\nStarting JSON-RPC response comparison...")
		if err := runComparison(cfg, *outputDir, *validateSchema, *concurrency, *timeout); err != nil {
			log.Printf("Response comparison warning: %v", err)
		} else {
			fmt.Println("Response comparison completed successfully!")
		}
	}
}

// runComparison runs a comparison of JSON-RPC responses across all clients in the config
func runComparison(cfg *config.Config, outputDir string, validateSchema bool, concurrency, timeout int) error {
	// Use resolved clients directly for the comparator
	clientsList := cfg.ResolvedClients

	// Extract methods from endpoints
	methods := make([]string, 0, len(cfg.Endpoints))
	for _, endpoint := range cfg.Endpoints {
		methods = append(methods, endpoint.Method)
	}

	// Create comparison config
	compConfig := &comparator.ComparisonConfig{
		Name:                  "Benchmark Response Comparison",
		Description:           "Comparing JSON-RPC responses across clients from benchmark config",
		Clients:               clientsList,
		Methods:               methods,
		ValidateAgainstSchema: validateSchema,
		Concurrency:           concurrency,
		TimeoutSeconds:        timeout,
		OutputDir:             outputDir,
	}

	// Create comparator
	comp, err := comparator.NewComparator(compConfig)
	if err != nil {
		return fmt.Errorf("failed to create comparator: %w", err)
	}

	// Run comparison
	results, err := comp.Run()
	if err != nil {
		return fmt.Errorf("comparison failed: %w", err)
	}
	fmt.Printf("Completed comparison of %d methods\n", len(results))

	// Save results to JSON file
	jsonPath := filepath.Join(outputDir, "comparison-results.json")
	if err := comp.SaveResults(jsonPath); err != nil {
		return fmt.Errorf("failed to save comparison results: %w", err)
	}
	fmt.Printf("Comparison results saved to %s\n", jsonPath)

	// Generate HTML report
	htmlPath := filepath.Join(outputDir, "comparison-report.html")
	if err := comp.GenerateHTMLReport(htmlPath); err != nil {
		return fmt.Errorf("failed to generate comparison HTML report: %w", err)
	}
	fmt.Printf("Comparison HTML report generated at %s\n", htmlPath)

	return nil
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
	if err := generateHistoricAnalysisReport(summary, trendData, recentRuns, outputDir); err != nil {
		return fmt.Errorf("failed to generate historic analysis report: %w", err)
	}

	logger.Info("Historic analysis completed successfully")
	return nil
}

// generateHistoricAnalysisReport generates an HTML report for historic analysis
func generateHistoricAnalysisReport(summary *types.HistoricSummary, trends []*types.TrendData, recentRuns []*types.HistoricRun, outputDir string) error {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	reportPath := filepath.Join(outputDir, "historic-analysis.html")

	// Simple HTML template for historic analysis
	htmlContent := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>Historic Analysis Report - %s</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { background-color: #f4f4f4; padding: 20px; border-radius: 5px; }
        .section { margin: 20px 0; }
        .metrics { display: flex; flex-wrap: wrap; gap: 20px; }
        .metric-card { border: 1px solid #ddd; padding: 15px; border-radius: 5px; min-width: 200px; }
        .trend-improving { color: green; }
        .trend-degrading { color: red; }
        .trend-stable { color: orange; }
        table { border-collapse: collapse; width: 100%%; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #f2f2f2; }
    </style>
</head>
<body>
    <div class="header">
        <h1>Historic Analysis Report</h1>
        <h2>Test: %s</h2>
        <p><strong>Total Runs:</strong> %d</p>
        <p><strong>Period:</strong> %s to %s</p>
    </div>
    
    <div class="section">
        <h3>Performance Trends (Last 30 Days)</h3>
        <div class="metrics">
`, summary.TestName, summary.TestName, summary.TotalRuns,
		summary.FirstRun.Format("2006-01-02"), summary.LastRun.Format("2006-01-02"))

	// Add trend cards
	for i, trend := range trends {
		trendClass := "trend-" + trend.Direction
		htmlContent += fmt.Sprintf(`
            <div class="metric-card">
                <h4>Trend %d - %s</h4>
                <p class="%s"><strong>Direction:</strong> %s</p>
                <p><strong>Data Points:</strong> %d</p>
                <p><strong>Change:</strong> %.2f%%</p>
            </div>
`, i+1, trend.Period, trendClass, trend.Direction, len(trend.TrendPoints), trend.PercentChange)
	}

	htmlContent += `
        </div>
    </div>
    
    <div class="section">
        <h3>Recent Runs</h3>
        <table>
            <tr>
                <th>Run ID</th>
                <th>Timestamp</th>
                <th>Git Commit</th>
                <th>Best Client</th>
                <th>Avg Latency (ms)</th>
                <th>Error Rate (%)</th>
                <th>Total Requests</th>
            </tr>
`

	// Add recent runs
	for _, run := range recentRuns {
		htmlContent += fmt.Sprintf(`
            <tr>
                <td>%s</td>
                <td>%s</td>
                <td>%s</td>
                <td>%s</td>
                <td>%.2f</td>
                <td>%.2f</td>
                <td>%d</td>
            </tr>
`, run.ID, run.Timestamp.Format("2006-01-02 15:04:05"),
			run.GitCommit, run.BestClient, run.AvgLatencyMs,
			run.OverallErrorRate*100, run.TotalRequests)
	}

	htmlContent += `
        </table>
    </div>
    
    <div class="section">
        <h3>Best and Worst Performance</h3>
        <div class="metrics">
            <div class="metric-card">
                <h4>Best Run</h4>
                <p><strong>Run ID:</strong> ` + summary.BestRun.ID + `</p>
                <p><strong>Timestamp:</strong> ` + summary.BestRun.Timestamp.Format("2006-01-02 15:04:05") + `</p>
                <p><strong>Avg Latency:</strong> ` + fmt.Sprintf("%.2f ms", summary.BestRun.AvgLatency) + `</p>
                <p><strong>Error Rate:</strong> ` + fmt.Sprintf("%.2f%%", summary.BestRun.OverallErrorRate) + `</p>
            </div>
            <div class="metric-card">
                <h4>Worst Run</h4>
                <p><strong>Run ID:</strong> ` + summary.WorstRun.ID + `</p>
                <p><strong>Timestamp:</strong> ` + summary.WorstRun.Timestamp.Format("2006-01-02 15:04:05") + `</p>
                <p><strong>Avg Latency:</strong> ` + fmt.Sprintf("%.2f ms", summary.WorstRun.AvgLatency) + `</p>
                <p><strong>Error Rate:</strong> ` + fmt.Sprintf("%.2f%%", summary.WorstRun.OverallErrorRate) + `</p>
            </div>
        </div>
    </div>
    
    <div class="section">
        <p><em>Report generated on ` + time.Now().Format("2006-01-02 15:04:05") + `</em></p>
    </div>
</body>
</html>
`

	// Write the report
	if err := os.WriteFile(reportPath, []byte(htmlContent), 0644); err != nil {
		return fmt.Errorf("failed to write historic analysis report: %w", err)
	}

	fmt.Printf("Historic analysis report generated at: %s\n", reportPath)
	return nil
}

// runAPIServer runs the HTTP API server for serving historic data
func runAPIServer(storageConfigPath string, logger *logrus.Logger) error {
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
		return fmt.Errorf("failed to ping PostgreSQL database: %w", err)
	}

	// Run migrations
	if err := storage.RunMigrations(db); err != nil {
		return fmt.Errorf("failed to run database migrations: %w", err)
	}

	// Initialize historic storage
	historicStorage, err := storage.NewHistoricStorage(storageCfg)
	if err != nil {
		return fmt.Errorf("failed to create historic storage: %w", err)
	}

	// Initialize analysis components
	baselineManager := analysis.NewBaselineManager(*historicStorage, db, logger)
	trendAnalyzer := analysis.NewTrendAnalyzer(*historicStorage, db, logger)
	regressionDetector := analysis.NewRegressionDetector(*historicStorage, baselineManager, db, logger)

	// Create API server
	apiServer := api.NewServer(
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
		return fmt.Errorf("failed to start API server: %w", err)
	}

	logger.Info("API server started successfully on port 8081")
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
