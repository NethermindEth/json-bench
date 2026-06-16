package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/comparator"
	"github.com/jsonrpc-bench/runner/types"
)

var (
	compareClientsPath    string
	compareClientRefs     string
	compareMethods        string
	compareName           string
	compareDescription    string
	compareValidateSchema bool
	compareConcurrency    int
	compareTimeout        int
)

var compareCmd = &cobra.Command{
	Use:   "compare",
	Short: "One-shot cross-client JSON-RPC response comparison",
	RunE:  runCompare,
}

func init() {
	compareCmd.Flags().StringVar(&compareClientsPath, "clients", "", "Path to clients.yaml")
	compareCmd.Flags().StringVar(&compareClientRefs, "client-refs", "", "Comma-separated client names from the registry (e.g. geth,nethermind)")
	compareCmd.Flags().StringVar(&compareMethods, "methods", "", "Comma-separated JSON-RPC method names")
	compareCmd.Flags().StringVar(&compareName, "name", "JSON-RPC Comparison", "Cosmetic name for the run")
	compareCmd.Flags().StringVar(&compareDescription, "description", "", "Cosmetic description")
	compareCmd.Flags().BoolVar(&compareValidateSchema, "validate-schema", false, "Validate responses against the OpenRPC schema")
	compareCmd.Flags().IntVar(&compareConcurrency, "concurrency", 5, "Concurrent requests")
	compareCmd.Flags().IntVar(&compareTimeout, "timeout", 30, "Per-request timeout in seconds")
	_ = compareCmd.MarkFlagRequired("clients")
	_ = compareCmd.MarkFlagRequired("client-refs")
	_ = compareCmd.MarkFlagRequired("methods")
}

func runCompare(cmd *cobra.Command, args []string) error {
	configureLogger()

	registry, err := loadClientRegistry(compareClientsPath)
	if err != nil {
		return err
	}

	refs := splitCSV(compareClientRefs)
	if len(refs) == 0 {
		return fmt.Errorf("--client-refs must list at least one client name")
	}

	clients := make([]*types.ClientConfig, 0, len(refs))
	for _, name := range refs {
		client, ok := registry.Get(name)
		if !ok {
			return fmt.Errorf("unknown client %q in --client-refs (not in %s)", name, compareClientsPath)
		}
		clients = append(clients, client)
	}

	methods := splitCSV(compareMethods)
	if len(methods) == 0 {
		return fmt.Errorf("--methods must list at least one JSON-RPC method")
	}

	compConfig := &comparator.ComparisonConfig{
		Name:                  compareName,
		Description:           compareDescription,
		Methods:               methods,
		Clients:               clients,
		ValidateAgainstSchema: compareValidateSchema,
		OutputDir:             outputDir,
		TimeoutSeconds:        compareTimeout,
		Concurrency:           compareConcurrency,
	}

	comp, err := comparator.NewComparator(compConfig)
	if err != nil {
		return fmt.Errorf("failed to create comparator: %w", err)
	}

	results, err := comp.Run()
	if err != nil {
		return fmt.Errorf("comparison failed: %w", err)
	}
	logger.Infof("Completed comparison of %d methods", len(results))

	jsonPath := filepath.Join(outputDir, "comparison-results.json")
	if err := comp.SaveResults(jsonPath); err != nil {
		return fmt.Errorf("failed to save comparison results: %w", err)
	}
	logger.Infof("Comparison results saved to %s", jsonPath)

	htmlPath := filepath.Join(outputDir, "comparison-report.html")
	if err := comp.GenerateHTMLReport(htmlPath); err != nil {
		return fmt.Errorf("failed to generate comparison HTML report: %w", err)
	}
	logger.Infof("Comparison HTML report generated at %s", htmlPath)

	return nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
