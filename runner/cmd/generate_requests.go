package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/engine"
)

var (
	genRequestsConfigPath  string
	genRequestsClientsPath string
	genRequestsOutPath     string
)

var generateRequestsCmd = &cobra.Command{
	Use:   "generate-requests",
	Short: "Pre-generate the requests CSV for a benchmark config without running the benchmark",
	RunE:  runGenerateRequests,
}

func init() {
	generateRequestsCmd.Flags().StringVar(&genRequestsConfigPath, "config", "", "Path to YAML benchmark configuration file")
	generateRequestsCmd.Flags().StringVar(&genRequestsClientsPath, "clients", "", "Path to clients configuration file (optional)")
	generateRequestsCmd.Flags().StringVar(&genRequestsOutPath, "out", "", "Destination path for the generated requests CSV (defaults to <output>/requests.csv)")
	rootCmd.AddCommand(generateRequestsCmd)
}

func runGenerateRequests(cmd *cobra.Command, args []string) error {
	configureLogger()

	if genRequestsConfigPath == "" {
		return fmt.Errorf("--config is required")
	}

	registry, err := loadClientRegistry(genRequestsClientsPath)
	if err != nil {
		return err
	}

	cfg, err := loadBenchmarkConfig(genRequestsConfigPath, genRequestsClientsPath, registry)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	requests, err := engine.BuildSequence(cfg)
	if err != nil {
		return fmt.Errorf("failed to generate requests: %w", err)
	}

	requestsPath := genRequestsOutPath
	if requestsPath == "" {
		requestsPath = filepath.Join(outputDir, "requests.csv")
	}
	if err := os.MkdirAll(filepath.Dir(requestsPath), 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}
	file, err := os.Create(requestsPath)
	if err != nil {
		return fmt.Errorf("failed to create requests file: %w", err)
	}
	if err := engine.WriteSequenceCSV(file, requests); err != nil {
		file.Close()
		return fmt.Errorf("failed to write requests: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close requests file: %w", err)
	}

	info, statErr := os.Stat(requestsPath)
	if statErr == nil {
		logger.WithField("path", requestsPath).WithField("bytes", info.Size()).Info("Generated requests file")
	} else {
		logger.WithField("path", requestsPath).Info("Generated requests file")
	}
	fmt.Println(requestsPath)
	return nil
}
