package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/comparator"
	"github.com/jsonrpc-bench/runner/types"
)

var (
	openrpcSpecPath       string
	openrpcVariationsPath string
	openrpcClientsPath    string
	openrpcClientRefs     string
	openrpcFilter         string
	openrpcCurl           bool
	openrpcValidateSchema bool
	openrpcConcurrency    int
	openrpcTimeout        int
)

var compareOpenRPCCmd = &cobra.Command{
	Use:   "compare-openrpc",
	Short: "Cross-client comparison driven by an OpenRPC specification",
	RunE:  runCompareOpenRPC,
}

func init() {
	compareOpenRPCCmd.Flags().StringVar(&openrpcSpecPath, "spec", "", "Path or URL to an OpenRPC specification")
	compareOpenRPCCmd.Flags().StringVar(&openrpcVariationsPath, "variations", "", "Optional YAML file of parameter variations per method")
	compareOpenRPCCmd.Flags().StringVar(&openrpcClientsPath, "clients", "", "Path to clients.yaml")
	compareOpenRPCCmd.Flags().StringVar(&openrpcClientRefs, "client-refs", "", "Comma-separated client names from the registry (e.g. geth,nethermind)")
	compareOpenRPCCmd.Flags().StringVar(&openrpcFilter, "filter", "", "Comma-separated method whitelist (empty = all methods)")
	compareOpenRPCCmd.Flags().BoolVar(&openrpcCurl, "curl", false, "Log curl-equivalent commands for every request")
	compareOpenRPCCmd.Flags().BoolVar(&openrpcValidateSchema, "validate-schema", false, "Validate responses against the OpenRPC schema")
	compareOpenRPCCmd.Flags().IntVar(&openrpcConcurrency, "concurrency", 5, "Concurrent requests")
	compareOpenRPCCmd.Flags().IntVar(&openrpcTimeout, "timeout", 30, "Per-request timeout in seconds")
	_ = compareOpenRPCCmd.MarkFlagRequired("spec")
	_ = compareOpenRPCCmd.MarkFlagRequired("clients")
	_ = compareOpenRPCCmd.MarkFlagRequired("client-refs")
}

func runCompareOpenRPC(cmd *cobra.Command, args []string) error {
	configureLogger()

	registry, err := loadClientRegistry(openrpcClientsPath)
	if err != nil {
		return err
	}

	refs := splitCSV(openrpcClientRefs)
	if len(refs) == 0 {
		return fmt.Errorf("--client-refs must list at least one client name")
	}

	clients := make([]*types.ClientConfig, 0, len(refs))
	for _, name := range refs {
		client, ok := registry.Get(name)
		if !ok {
			return fmt.Errorf("unknown client %q in --client-refs (not in %s)", name, openrpcClientsPath)
		}
		clients = append(clients, client)
	}

	logger.Infof("Loading methods from OpenRPC specification: %s", openrpcSpecPath)
	if openrpcVariationsPath != "" {
		logger.Infof("Using parameter variations from: %s", openrpcVariationsPath)
	}

	cfg, err := comparator.LoadMethodsFromOpenRPC(openrpcSpecPath, openrpcVariationsPath)
	if err != nil {
		return fmt.Errorf("failed to load OpenRPC specification: %w", err)
	}

	cfg.Clients = clients
	cfg.ValidateAgainstSchema = openrpcValidateSchema
	cfg.Concurrency = openrpcConcurrency
	cfg.TimeoutSeconds = openrpcTimeout
	cfg.OutputDir = outputDir
	cfg.Verbose = openrpcCurl

	if openrpcFilter != "" {
		applyMethodFilter(cfg, splitCSV(openrpcFilter))
	}

	logger.Infof("Loaded %d methods (including variations) from OpenRPC specification", len(cfg.Methods))

	comp, err := comparator.NewComparator(cfg)
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

func applyMethodFilter(cfg *comparator.ComparisonConfig, methodsToInclude []string) {
	methodMap := make(map[string]bool, len(methodsToInclude))
	for _, m := range methodsToInclude {
		methodMap[m] = true
	}

	filteredMethods := make([]string, 0, len(cfg.Methods))
	filteredParams := make(map[string][]interface{})
	filteredRPCNames := make(map[string]string)
	for _, method := range cfg.Methods {
		// Match on both the wire-level RPC name (from MethodRPCNames) and the
		// identifier itself so `--filter eth_call` still selects
		// `eth_call_variant1`, `eth_call_variant2`, etc.
		baseName := method
		if name, ok := cfg.MethodRPCNames[method]; ok && name != "" {
			baseName = name
		}
		if !methodMap[baseName] && !methodMap[method] {
			continue
		}
		filteredMethods = append(filteredMethods, method)
		if params, ok := cfg.CustomParameters[method]; ok {
			filteredParams[method] = params
		}
		if name, ok := cfg.MethodRPCNames[method]; ok {
			filteredRPCNames[method] = name
		}
	}
	cfg.Methods = filteredMethods
	cfg.CustomParameters = filteredParams
	cfg.MethodRPCNames = filteredRPCNames
}
