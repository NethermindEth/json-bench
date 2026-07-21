package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/comparator"
	"github.com/jsonrpc-bench/runner/types"
)

var (
	compareConfigPath     string
	compareClientsPath    string
	compareClientRefs     string
	compareValidateSchema bool
	compareConcurrency    int
	compareTimeout        int

	compareRulesPath        string
	compareDiffOnly         bool
	compareKeepBodies       bool
	compareOmitMatching     bool
	compareMaxResponseBytes int
	compareMaxRetries       int
	compareRetryBaseDelay   time.Duration
	compareFailOnDiff       bool
	compareFailOnEnv        bool
	compareSkipAboveHead    bool
	compareBlockOverride    string
	compareFromJSONL        string
	compareSample           int
	compareSampleSeed       int64
)

var compareCmd = &cobra.Command{
	Use:   "compare",
	Short: "One-shot cross-client JSON-RPC response comparison",
	RunE:  runCompare,
}

func init() {
	compareCmd.Flags().StringVar(&compareConfigPath, "config", "", "Path to compare YAML config (see config/compare/example.yaml)")
	compareCmd.Flags().StringVar(&compareClientsPath, "clients", "", "Path to clients.yaml")
	compareCmd.Flags().StringVar(&compareClientRefs, "client-refs", "", "Comma-separated client names from the registry (e.g. geth,nethermind)")
	compareCmd.Flags().BoolVar(&compareValidateSchema, "validate-schema", false, "Validate responses against the OpenRPC schema")
	compareCmd.Flags().IntVar(&compareConcurrency, "concurrency", 5, "Concurrent requests")
	compareCmd.Flags().IntVar(&compareTimeout, "timeout", 30, "Per-request timeout in seconds")

	compareCmd.Flags().StringVar(&compareRulesPath, "rules", "", "Path to a YAML file with a comparison: block (rules + block_override) to merge in; works with --config and --from-jsonl")
	compareCmd.Flags().BoolVar(&compareDiffOnly, "diff-only", false, "Exclude identical calls from the output (caps response bodies unless --keep-response-bodies)")
	compareCmd.Flags().BoolVar(&compareKeepBodies, "keep-response-bodies", false, "With --diff-only, keep full response bodies instead of truncating them")
	compareCmd.Flags().BoolVar(&compareOmitMatching, "omit-matching-responses", false, "Drop full responses; keep only diff entries")
	compareCmd.Flags().IntVar(&compareMaxResponseBytes, "max-response-bytes", 0, "Truncate embedded response bodies larger than N bytes (0 = no limit)")
	compareCmd.Flags().IntVar(&compareMaxRetries, "max-retries", 0, "Max transport attempts per request (0 = use the client's max_retries from clients.yaml, or 5 if unset)")
	compareCmd.Flags().DurationVar(&compareRetryBaseDelay, "retry-base-delay", 0, "Base backoff between transport retries (0 = 200ms)")
	compareCmd.Flags().BoolVar(&compareFailOnDiff, "fail-on-diff", false, "Exit non-zero when real (non-environment) differences remain")
	compareCmd.Flags().BoolVar(&compareFailOnEnv, "fail-on-env-diff", false, "Also exit non-zero on environment/capability differences (compose with --fail-on-diff for strict mode)")
	compareCmd.Flags().BoolVar(&compareSkipAboveHead, "skip-above-head", false, "Skip calls pinned to a block above the lowest client head")
	compareCmd.Flags().StringVar(&compareBlockOverride, "block-override", "", "Rewrite latest/pending block tags to this static block (overrides config and --rules)")
	compareCmd.Flags().StringVar(&compareFromJSONL, "from-jsonl", "", "Build the config from a corpus directory (recurses; reads *.jsonl and *.json arrays) instead of --config")
	compareCmd.Flags().IntVar(&compareSample, "sample", 0, "With --from-jsonl, sample at most N calls per method (0 = all)")
	compareCmd.Flags().Int64Var(&compareSampleSeed, "sample-seed", 42, "Deterministic seed for --sample")

	_ = compareCmd.MarkFlagRequired("clients")
	_ = compareCmd.MarkFlagRequired("client-refs")
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

	if (compareConfigPath == "") == (compareFromJSONL == "") {
		return fmt.Errorf("exactly one of --config or --from-jsonl is required")
	}

	// Load the optional --rules file first so its block_override can inform
	// corpus loading (e.g. keeping pinnable methods like eth_feeHistory).
	var fileRules []comparator.ComparisonRule
	var fileBlockOverride string
	if compareRulesPath != "" {
		fileRules, fileBlockOverride, err = comparator.LoadComparisonRules(compareRulesPath)
		if err != nil {
			return fmt.Errorf("failed to load rules file: %w", err)
		}
	}

	// Effective block override, highest precedence first: --block-override flag,
	// then the --rules file. (A --config file's own block_override applies in
	// config mode and is layered below both.)
	effectiveBlockOverride := fileBlockOverride
	if compareBlockOverride != "" {
		effectiveBlockOverride = compareBlockOverride
	}

	var cfg *comparator.ComparisonConfig
	if compareFromJSONL != "" {
		cfg, err = comparator.LoadCorpusConfig(compareFromJSONL, compareSample, compareSampleSeed, effectiveBlockOverride)
		if err != nil {
			return fmt.Errorf("failed to build config from corpus: %w", err)
		}
	} else {
		cfg, err = comparator.LoadCompareConfig(compareConfigPath)
		if err != nil {
			return fmt.Errorf("failed to load compare config: %w", err)
		}
	}

	cfg.Clients = clients
	cfg.ValidateAgainstSchema = compareValidateSchema
	cfg.Concurrency = compareConcurrency
	cfg.TimeoutSeconds = compareTimeout
	cfg.OutputDir = outputDir
	cfg.DiffOnly = compareDiffOnly
	cfg.OmitMatchingResponses = compareOmitMatching
	cfg.MaxResponseBytes = compareMaxResponseBytes
	cfg.MaxRetries = compareMaxRetries
	cfg.RetryBaseDelayMs = int(compareRetryBaseDelay.Milliseconds())
	cfg.SkipAboveHead = compareSkipAboveHead

	// Layer the --rules file on top of any rules from --config, then apply the
	// block-override precedence (rules file over config, flag over everything).
	cfg.MergeComparisonRules(fileRules)
	if fileBlockOverride != "" {
		cfg.BlockOverride = fileBlockOverride
	}
	if compareBlockOverride != "" {
		cfg.BlockOverride = compareBlockOverride
	}

	applyDiffOnlyDefaults(cfg, compareKeepBodies)

	comp, err := comparator.NewComparator(cfg)
	if err != nil {
		return fmt.Errorf("failed to create comparator: %w", err)
	}

	results, err := comp.Run()
	if err != nil {
		return fmt.Errorf("comparison failed: %w", err)
	}
	logger.Infof("Completed comparison of %d calls", len(results))

	return finishComparison(comp, compareFailOnDiff, compareFailOnEnv)
}

// applyDiffOnlyDefaults makes --diff-only the obvious "small report" switch: if
// no body-trimming flag was given (and bodies weren't explicitly kept), it caps
// response bodies so the report doesn't balloon on large differing responses.
func applyDiffOnlyDefaults(cfg *comparator.ComparisonConfig, keepBodies bool) {
	if cfg.DiffOnly && !keepBodies && !cfg.OmitMatchingResponses && cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = comparator.DefaultDiffOnlyMaxResponseBytes
		logger.Warnf("--diff-only without a body-trimming flag: truncating response bodies to %d bytes to keep the report small; pass --keep-response-bodies to retain them or --omit-matching-responses to drop them entirely", comparator.DefaultDiffOnlyMaxResponseBytes)
	}
}

// finishComparison writes the results, provenance sidecar, and HTML report,
// prints the outcome summary, and returns a non-zero (error) result when
// failOnDiff is set and post-filter differences remain.
func finishComparison(comp *comparator.Comparator, failOnDiff, failOnEnv bool) error {
	jsonPath := filepath.Join(outputDir, "comparison-results.json")
	if err := comp.SaveResults(jsonPath); err != nil {
		return fmt.Errorf("failed to save comparison results: %w", err)
	}
	logger.Infof("Comparison results saved to %s", jsonPath)

	provPath := filepath.Join(outputDir, "comparison-provenance.json")
	if err := comp.SaveProvenance(provPath); err != nil {
		return fmt.Errorf("failed to save comparison provenance: %w", err)
	}

	htmlPath := filepath.Join(outputDir, "comparison-report.html")
	if err := comp.GenerateHTMLReport(htmlPath); err != nil {
		return fmt.Errorf("failed to generate comparison HTML report: %w", err)
	}
	logger.Infof("Comparison HTML report generated at %s", htmlPath)

	printComparisonSummary(comp.Summarize())

	realFail := failOnDiff && comp.HasRealDifferences()
	envFail := failOnEnv && comp.HasEnvDifferences()
	if realFail {
		return fmt.Errorf("real differences found (--fail-on-diff)")
	}
	if envFail {
		return fmt.Errorf("environment/expected differences found (--fail-on-env-diff)")
	}
	return nil
}

// printComparisonSummary logs a one-screen tally of the run's outcomes.
func printComparisonSummary(s comparator.Summary) {
	logger.Infof("Summary: %d calls — %d identical, %d differ (real), %d differ (env/expected), %d transport-error, %d schema-error, %d skipped",
		s.Total, s.Identical, s.Differ, s.DifferEnv, s.TransportError, s.SchemaError, s.Skipped)
	for class, n := range s.EnvError {
		logger.Infof("  env/capability errors [%s]: %d", class, n)
	}
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
