package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/engine"
)

var (
	maxRPSConfigPath     string
	maxRPSClientsPath    string
	maxRPSSLOP99         float64
	maxRPSSLOErrorRate   float64
	maxRPSMinDelivered   float64
	maxRPSMin            int
	maxRPSMax            int
	maxRPSProbeDuration  time.Duration
	maxRPSTolerance      int
	maxRPSSettle         time.Duration
	maxRPSSkipPreflight  bool
	maxRPSCompression    bool
	maxRPSNoConnReuse    bool
	maxRPSHTTP2          bool
	maxRPSFailOnGenLimit bool
)

var maxRPSCmd = &cobra.Command{
	Use:   "find-max-rps",
	Short: "Search for the highest request rate a target sustains within an SLO",
	Long: `Search for the highest request rate one JSON-RPC endpoint sustains while
holding a service level: a p99 ceiling and an error ceiling.

The search doubles the rate until the SLO breaks, then bisects. Each probe is a
full run at that rate, and a probe the generator could not actually offer is
reported as inconclusive rather than counted as a limit of the endpoint —
measuring the generator's ceiling and calling it the node's capacity is the
failure this is built to avoid. When that happens, raise vus.`,
	RunE: runMaxRPS,
}

func init() {
	maxRPSCmd.Flags().StringVar(&maxRPSConfigPath, "config", "", "Path to YAML benchmark configuration file (its rps and duration are replaced per probe)")
	maxRPSCmd.Flags().StringVar(&maxRPSClientsPath, "clients", "", "Path to clients configuration file")
	maxRPSCmd.Flags().Float64Var(&maxRPSSLOP99, "slo-p99", 1000, "Latency ceiling: p99 in milliseconds")
	maxRPSCmd.Flags().Float64Var(&maxRPSSLOErrorRate, "slo-error-rate", 1, "Error ceiling as a percentage, counting JSON-RPC errors")
	maxRPSCmd.Flags().Float64Var(&maxRPSMinDelivered, "slo-min-delivered", 99, "How much of the offered load must have been sent for a probe to count at all, as a percentage")
	maxRPSCmd.Flags().IntVar(&maxRPSMin, "min-rps", 10, "Rate the search starts from")
	maxRPSCmd.Flags().IntVar(&maxRPSMax, "max-rps", 0, "Ceiling for the search (0 = keep doubling until the SLO breaks)")
	maxRPSCmd.Flags().DurationVar(&maxRPSProbeDuration, "probe-duration", 30*time.Second, "How long to hold each rate")
	maxRPSCmd.Flags().IntVar(&maxRPSTolerance, "tolerance", 10, "Stop bisecting once the answer is bracketed this tightly, in rps")
	maxRPSCmd.Flags().DurationVar(&maxRPSSettle, "settle", 5*time.Second, "Pause between probes so the target returns to rest")
	maxRPSCmd.Flags().BoolVar(&maxRPSSkipPreflight, "skip-preflight", false, "Do not identify the target before searching")
	maxRPSCmd.Flags().BoolVar(&maxRPSCompression, "accept-compression", false, "Send Accept-Encoding so the node may compress responses")
	maxRPSCmd.Flags().BoolVar(&maxRPSNoConnReuse, "no-connection-reuse", false, "Open a fresh connection per request")
	maxRPSCmd.Flags().BoolVar(&maxRPSHTTP2, "http2", false, "Allow an HTTP/2 upgrade over TLS")
	maxRPSCmd.Flags().BoolVar(&maxRPSFailOnGenLimit, "fail-on-generator-limit", false, "Exit non-zero when the generator, not the endpoint, bounded the search")
}

func runMaxRPS(cmd *cobra.Command, args []string) error {
	configureLogger()

	if maxRPSConfigPath == "" {
		return fmt.Errorf("--config is required")
	}

	registry, err := loadClientRegistry(maxRPSClientsPath)
	if err != nil {
		return err
	}
	cfg, err := loadBenchmarkConfig(maxRPSConfigPath, maxRPSClientsPath, registry)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	opts := engine.DefaultOptions()
	opts.Logger = logger
	opts.SkipPreflight = maxRPSSkipPreflight
	opts.Transport.AcceptCompression = maxRPSCompression
	opts.Transport.ReuseConnections = !maxRPSNoConnReuse
	opts.Transport.HTTP2 = maxRPSHTTP2

	search := engine.SearchOptions{
		SLO: engine.SLO{
			P99Ms:               maxRPSSLOP99,
			ErrorRatePercent:    maxRPSSLOErrorRate,
			MinDeliveredPercent: maxRPSMinDelivered,
		},
		MinRPS:        maxRPSMin,
		MaxRPS:        maxRPSMax,
		ProbeDuration: maxRPSProbeDuration,
		Tolerance:     maxRPSTolerance,
		Settle:        maxRPSSettle,
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Infof("Searching for the highest rate %s sustains at p99 <= %.0fms and errors <= %.2f%%, %s per probe",
		cfg.ResolvedClients[0].Name, maxRPSSLOP99, maxRPSSLOErrorRate, maxRPSProbeDuration)

	result, runErr := engine.FindMaxRPS(ctx, cfg, opts, search)
	if result == nil {
		return runErr
	}

	resultPath := filepath.Join(outputDir, "max-rps.json")
	if err := writeSearchResult(resultPath, result); err != nil {
		logger.WithError(err).Warn("Failed to write the search result")
	} else {
		logger.Infof("Search result written to %s", resultPath)
	}

	logSearchResult(result)

	if runErr != nil {
		return fmt.Errorf("the search did not complete: %w", runErr)
	}
	if maxRPSFailOnGenLimit && !result.Conclusive() {
		return fmt.Errorf("the generator bounded the search, so the result is not a property of the endpoint (--fail-on-generator-limit)")
	}
	return nil
}

func writeSearchResult(path string, result *engine.SearchResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func logSearchResult(result *engine.SearchResult) {
	logger.Info("Probes:")
	for _, probe := range result.Probes {
		logger.Infof("  %6d rps  %-17s delivered %6.1f%%  p99 %8.1fms  errors %6.2f%%",
			probe.RPS, probe.Verdict, probe.DeliveredPct, probe.P99Ms, probe.ErrorRatePct)
	}

	answer := result.Explain()
	if result.Conclusive() {
		logger.Infof("Result for %s: %s", result.Client, answer)
		return
	}
	// A generator-bounded search says nothing about the endpoint, so it is not
	// reported as though it did.
	logger.Warnf("Result for %s is inconclusive: %s", result.Client, answer)
}
