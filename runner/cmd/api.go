package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/api"
)

var (
	apiAddr              string
	apiStorageConfigPath string
)

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Start the HTTP API server",
	RunE:  runAPI,
}

func init() {
	apiCmd.Flags().StringVar(&apiAddr, "api-addr", ":8081", "Address to bind the API server to")
	apiCmd.Flags().StringVar(&apiStorageConfigPath, "storage-config", "", "Path to storage configuration file")
	_ = apiCmd.MarkFlagRequired("storage-config")
}

func runAPI(cmd *cobra.Command, args []string) error {
	configureLogger()

	historic, db, err := openHistoricStorage(apiStorageConfigPath)
	if err != nil {
		return err
	}
	defer db.Close()

	baselineManager := analysis.NewBaselineManager(*historic, db, logger)
	trendAnalyzer := analysis.NewTrendAnalyzer(*historic, db, logger)
	regressionDetector := analysis.NewRegressionDetector(*historic, baselineManager, db, logger)

	apiServer := api.NewServer(
		apiAddr,
		*historic,
		baselineManager,
		trendAnalyzer,
		regressionDetector,
		db,
		logger,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := apiServer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start api server: %w", err)
	}
	logger.Info("Press Ctrl+C to stop the server")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
		logger.Info("Received shutdown signal, shutting down API server...")
		return apiServer.Stop()
	case <-ctx.Done():
		logger.Info("Context cancelled, shutting down API server...")
		return apiServer.Stop()
	}
}
