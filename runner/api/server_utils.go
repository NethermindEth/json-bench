package api

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/sirupsen/logrus"
)

// runAPIServer runs the HTTP API server for serving historic data
func RunAPIServer(storageConfigPath string, logger *logrus.Logger) error {
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
	apiServer := NewServer(
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
