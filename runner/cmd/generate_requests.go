package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/generator"
)

var (
	genRequestsConfigPath  string
	genRequestsClientsPath string
	genRequestsOutPath     string
)

var generateRequestsCmd = &cobra.Command{
	Use:   "generate-requests",
	Short: "Pre-generate the k6 requests CSV for a benchmark config without running k6",
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

	requestsPath, err := generator.GenerateK6Requests(cfg, outputDir)
	if err != nil {
		return fmt.Errorf("failed to generate requests: %w", err)
	}

	if genRequestsOutPath != "" {
		if err := os.MkdirAll(filepath.Dir(genRequestsOutPath), 0o755); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}
		if err := os.Rename(requestsPath, genRequestsOutPath); err != nil {
			return fmt.Errorf("failed to move requests file to %s: %w", genRequestsOutPath, err)
		}
		requestsPath = genRequestsOutPath
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
