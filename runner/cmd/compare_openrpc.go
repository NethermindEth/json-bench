package cmd

import (
	"fmt"
	"time"

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

	openrpcDiffOnly         bool
	openrpcKeepBodies       bool
	openrpcOmitMatching     bool
	openrpcMaxResponseBytes int
	openrpcMaxRetries       int
	openrpcRetryBaseDelay   time.Duration
	openrpcFailOnDiff       bool
	openrpcFailOnEnv        bool
	openrpcSkipAboveHead    bool
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

	compareOpenRPCCmd.Flags().BoolVar(&openrpcDiffOnly, "diff-only", false, "Exclude identical calls from the output (caps response bodies unless --keep-response-bodies)")
	compareOpenRPCCmd.Flags().BoolVar(&openrpcKeepBodies, "keep-response-bodies", false, "With --diff-only, keep full response bodies instead of truncating them")
	compareOpenRPCCmd.Flags().BoolVar(&openrpcOmitMatching, "omit-matching-responses", false, "Drop full responses; keep only diff entries")
	compareOpenRPCCmd.Flags().IntVar(&openrpcMaxResponseBytes, "max-response-bytes", 0, "Truncate embedded response bodies larger than N bytes (0 = no limit)")
	compareOpenRPCCmd.Flags().IntVar(&openrpcMaxRetries, "max-retries", 0, "Max transport attempts per request (0 = use the client's max_retries from clients.yaml, or 5 if unset)")
	compareOpenRPCCmd.Flags().DurationVar(&openrpcRetryBaseDelay, "retry-base-delay", 0, "Base backoff between transport retries (0 = 200ms)")
	compareOpenRPCCmd.Flags().BoolVar(&openrpcFailOnDiff, "fail-on-diff", false, "Exit non-zero when real (non-environment) differences remain")
	compareOpenRPCCmd.Flags().BoolVar(&openrpcFailOnEnv, "fail-on-env-diff", false, "Also exit non-zero on environment/capability differences (compose with --fail-on-diff for strict mode)")
	compareOpenRPCCmd.Flags().BoolVar(&openrpcSkipAboveHead, "skip-above-head", false, "Skip calls pinned to a block above the lowest client head")

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
	cfg.DiffOnly = openrpcDiffOnly
	cfg.OmitMatchingResponses = openrpcOmitMatching
	cfg.MaxResponseBytes = openrpcMaxResponseBytes
	cfg.MaxRetries = openrpcMaxRetries
	cfg.RetryBaseDelayMs = int(openrpcRetryBaseDelay.Milliseconds())
	cfg.SkipAboveHead = openrpcSkipAboveHead
	applyDiffOnlyDefaults(cfg, openrpcKeepBodies)

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

	return finishComparison(comp, openrpcFailOnDiff, openrpcFailOnEnv)
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
