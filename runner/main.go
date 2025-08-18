package main

import (
	"flag"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/generator"
	"github.com/jsonrpc-bench/runner/metrics"
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

	// // Historic storage flags
	// enableHistoric := flag.Bool("historic", false, "Enable historic data storage and analysis")
	// storageConfig := flag.String("storage-config", "", "Path to storage configuration file")
	// historicMode := flag.Bool("historic-mode", false, "Run in historic analysis mode (no new benchmark)")

	// // API server flags
	// apiMode := flag.Bool("api", false, "Run in API server mode (HTTP server + WebSocket)")

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

	// // API server mode
	// if *apiMode {
	// 	if err := api.RunAPIServer(*storageConfig, logger); err != nil {
	// 		logger.WithError(err).Fatal("api server failed")
	// 	}
	// 	return
	// }

	if *configPath == "" {
		logger.Fatal("please provide a configuration file path using -config flag")
	}

	// Initialize client registry
	clientRegistry := config.NewClientRegistry()

	// Load clients configuration if provided
	if *clientsPath != "" {
		if err := clientRegistry.LoadFromFile(*clientsPath); err != nil {
			logger.WithError(err).Fatal("failed to load clients configuration")
		}
		logger.Info("loaded clients configuration from ", *clientsPath)
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
		logger.Fatal("no metrics outputs configured")
	}

	// // Initialize historic storage if enabled
	// var historicStorage *storage.HistoricStorage
	// if *enableHistoric || *historicMode {
	// 	if *storageConfig == "" {
	// 		logger.Fatal("storage configuration path is required when historic mode is enabled. Use -storage-config flag.")
	// 	}

	// 	storageCfg, err := config.LoadStorageConfig(*storageConfig, logger)
	// 	if err != nil {
	// 		logger.WithError(err).Fatal("failed to load storage configuration")
	// 	}

	// 	if storageCfg.EnableHistoric {
	// 		// Initialize PostgreSQL storage
	// 		db, err := sql.Open("postgres", storageCfg.PostgreSQL.ConnectionString())
	// 		if err != nil {
	// 			logger.WithError(err).Fatal("failed to connect to postgres database")
	// 		}
	// 		defer db.Close()

	// 		// Test connection
	// 		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	// 		defer cancel()
	// 		if err := db.PingContext(ctx); err != nil {
	// 			logger.WithError(err).Fatal("failed to ping postgres database")
	// 		}

	// 		// Run migrations
	// 		if err := storage.RunMigrations(db); err != nil {
	// 			logger.WithError(err).Fatal("failed to run database migrations")
	// 		}

	// 		// Initialize historic storage
	// 		historicStorage, err = storage.NewHistoricStorage(storageCfg)
	// 		if err != nil {
	// 			logger.WithError(err).Fatal("failed to create historic storage")
	// 		}

	// 		logger.Info("historic storage initialized successfully")
	// 	} else {
	// 		logger.Fatal("historic storage must be enabled in configuration")
	// 	}
	// }

	// // Handle historic mode (analysis only, no new benchmark)
	// if *historicMode {
	// 	if err := generator.RunHistoricAnalysis(cfg, historicStorage, *outputDir, logger); err != nil {
	// 		logger.WithError(err).Fatal("historic analysis failed")
	// 	}
	// 	return
	// }

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		logger.WithError(err).Fatal("failed to create output directory")
	}

	// Generate k6 script

	k6Cmd, summaryPath, err := generator.GenerateK6(cfg, *outputDir)
	if err != nil {
		logger.WithError(err).Fatal("failed to generate k6 command")
	}

	// Start system metrics collection
	systemCollector, err := metrics.NewSystemCollector(1 * time.Second)
	if err != nil {
		logger.WithError(err).Warn("failed to create system collector")
	} else {
		systemCollector.Start()
		defer systemCollector.Stop()
	}

	// Run k6 benchmark
	logger.Info("running benchmark")
	err = k6Cmd.Run()
	if err != nil {
		logger.WithError(err).Warn("k6 command execution completed with errors")
	} else {
		logger.WithField("summary_path", summaryPath).Info("k6 command executed successfully")
	}

	// // Add system metrics to results if available
	// if systemCollector != nil {
	// 	avgMetrics := systemCollector.GetAverageMetrics()
	// 	// Add system metrics to each client
	// 	for _, client := range results.ClientMetrics {
	// 		client.SystemMetrics = []types.SystemMetrics{avgMetrics}
	// 	}
	// }

	// // Add environment info
	// results.Environment = metrics.GetEnvironmentInfo()

	// // Perform performance analysis
	// performanceAnalyzer := analyzer.NewPerformanceAnalyzer()
	// performanceAnalyzer.AnalyzeResults(results)

	// // Save to historic storage if enabled
	// if *enableHistoric && historicStorage != nil {
	// 	savedRun, err := historicStorage.SaveRun(results, cfg)
	// 	if err != nil {
	// 		logger.WithError(err).Error("Failed to save historic run")
	// 	} else {
	// 		logger.WithField("run_id", savedRun.ID).Info("Historic run saved successfully")
	// 	}
	// }

	logger.Info("benchmark completed")

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
