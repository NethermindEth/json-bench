package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/analyzer"
	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/engine"
	"github.com/jsonrpc-bench/runner/engine/promrw"
	"github.com/jsonrpc-bench/runner/exporter"
	"github.com/jsonrpc-bench/runner/generator"
	"github.com/jsonrpc-bench/runner/metrics"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

var (
	benchmarkConfigPath        string
	benchmarkClientsPath       string
	benchmarkPrometheusURL     string
	benchmarkPrometheusRWPath  string
	benchmarkPrometheusRWUser  string
	benchmarkPrometheusRWPass  string
	benchmarkEnableHistoric    bool
	benchmarkStorageConfigPath string
	benchmarkHTMLReport        bool
	benchmarkSaturation        string
	benchmarkCompression       bool
	benchmarkNoConnReuse       bool
	benchmarkHTTP2             bool
	benchmarkNoSamples         bool
	benchmarkFailOnThreshold   bool
	benchmarkPrometheusBearer  string
	benchmarkPrometheusHeaders []string
	benchmarkPushInterval      time.Duration
)

var benchmarkCmd = &cobra.Command{
	Use:   "benchmark",
	Short: "Run a benchmark against one or more JSON-RPC endpoints",
	RunE:  runBenchmark,
}

func init() {
	benchmarkCmd.Flags().StringVar(&benchmarkConfigPath, "config", "", "Path to YAML configuration file")
	benchmarkCmd.Flags().StringVar(&benchmarkClientsPath, "clients", "", "Path to clients configuration file (optional)")
	benchmarkCmd.Flags().StringVar(&benchmarkPrometheusURL, "prometheus", "", "Prometheus base URL (optional; the remote-write path is appended for publishing, and the base URL is probed before the run)")
	benchmarkCmd.Flags().StringVar(&benchmarkPrometheusRWPath, "prometheus-rw-path", "/api/v1/write", "Path appended to --prometheus to form the remote-write target")
	benchmarkCmd.Flags().StringVar(&benchmarkPrometheusRWUser, "prometheus-rw-user", "", "Prometheus basic-auth username (optional)")
	benchmarkCmd.Flags().StringVar(&benchmarkPrometheusRWPass, "prometheus-rw-pass", "", "Prometheus basic-auth password (optional)")
	benchmarkCmd.Flags().BoolVar(&benchmarkEnableHistoric, "historic", false, "Persist this run to historic storage")
	benchmarkCmd.Flags().StringVar(&benchmarkStorageConfigPath, "storage-config", "", "Path to storage configuration file (required with --historic)")
	benchmarkCmd.Flags().BoolVar(&benchmarkHTMLReport, "html-report", false, "Generate the HTML benchmark report in addition to JSON/CSV")
	benchmarkCmd.Flags().StringVar(&benchmarkSaturation, "on-saturation", string(engine.SaturationQueue),
		"What to do when the in-flight limit is reached at a request's scheduled time: queue (send late and record the delay), drop (discard and count), abort (fail the run)")
	benchmarkCmd.Flags().BoolVar(&benchmarkCompression, "accept-compression", false, "Send Accept-Encoding so the node may compress responses (off by default: it changes large-response latency and byte counts)")
	benchmarkCmd.Flags().BoolVar(&benchmarkNoConnReuse, "no-connection-reuse", false, "Open a fresh connection per request instead of reusing the pool")
	benchmarkCmd.Flags().BoolVar(&benchmarkHTTP2, "http2", false, "Allow an HTTP/2 upgrade over TLS")
	benchmarkCmd.Flags().BoolVar(&benchmarkNoSamples, "no-samples", false, "Skip writing the per-request sample file")
	benchmarkCmd.Flags().BoolVar(&benchmarkFailOnThreshold, "fail-on-threshold", false, "Exit non-zero when a configured threshold is breached")
	benchmarkCmd.Flags().StringVar(&benchmarkPrometheusBearer, "prometheus-rw-bearer", "", "Prometheus bearer token (optional; mutually exclusive with basic auth)")
	benchmarkCmd.Flags().StringArrayVar(&benchmarkPrometheusHeaders, "prometheus-rw-header", nil, "Extra remote-write header as Name=Value, repeatable (e.g. X-Scope-OrgID=team for Mimir)")
	benchmarkCmd.Flags().DurationVar(&benchmarkPushInterval, "prometheus-push-interval", promrw.DefaultPushInterval, "How often to publish metrics during the run")
}

func runBenchmark(cmd *cobra.Command, args []string) error {
	configureLogger()

	if benchmarkConfigPath == "" {
		return fmt.Errorf("--config is required")
	}
	if benchmarkEnableHistoric && benchmarkStorageConfigPath == "" {
		return fmt.Errorf("--storage-config is required when --historic is set")
	}

	saturation, err := engine.ParseSaturation(benchmarkSaturation)
	if err != nil {
		return err
	}

	registry, err := loadClientRegistry(benchmarkClientsPath)
	if err != nil {
		return err
	}

	cfg, err := loadBenchmarkConfig(benchmarkConfigPath, benchmarkClientsPath, registry)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if cfg.UsesCallsFile() {
		logger.Infof("Using pre-generated requests from %s: %d distinct RPC methods, which is what the per-method breakdown is keyed on", cfg.CallsFile, len(cfg.CallsFileMethods))
	}

	cfg.Outputs = &config.Outputs{}
	if benchmarkPrometheusURL != "" {
		queryURL := strings.TrimRight(benchmarkPrometheusURL, "/")
		rwPath := benchmarkPrometheusRWPath
		if !strings.HasPrefix(rwPath, "/") {
			rwPath = "/" + rwPath
		}
		cfg.Outputs.PrometheusRW = &config.PrometheusRW{
			Endpoint: queryURL + rwPath,
			QueryURL: queryURL,
			BasicAuth: config.BasicAuth{
				Username: benchmarkPrometheusRWUser,
				Password: benchmarkPrometheusRWPass,
			},
		}
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	var historic *storage.HistoricStorage
	if benchmarkEnableHistoric {
		h, db, err := openHistoricStorage(benchmarkStorageConfigPath)
		if err != nil {
			return err
		}
		defer db.Close()
		historic = h
		logger.Info("Historic storage initialized successfully")
	}

	if err := metrics.CheckPrometheus(cfg); err != nil {
		logger.WithError(err).Warnf("Prometheus at %s is not reachable; the run will proceed but no time series will be recorded", benchmarkPrometheusURL)
	}

	systemCollector, err := metrics.NewSystemCollector(1 * time.Second)
	if err != nil {
		logger.WithError(err).Warn("Failed to create system collector")
	} else {
		systemCollector.Start()
		defer systemCollector.Stop()
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sink, err := buildMetricsSink(cfg)
	if err != nil {
		return err
	}

	opts := engine.DefaultOptions()
	opts.OutputDir = outputDir
	opts.Sink = sink
	opts.PushInterval = benchmarkPushInterval
	opts.Saturation = saturation
	opts.WriteSamples = !benchmarkNoSamples
	opts.Logger = logger
	opts.Transport.AcceptCompression = benchmarkCompression
	opts.Transport.ReuseConnections = !benchmarkNoConnReuse
	opts.Transport.HTTP2 = benchmarkHTTP2

	benchmarkResults, breaches, runErr := engine.Run(ctx, cfg, opts)
	if benchmarkResults == nil {
		return runErr
	}

	if systemCollector != nil {
		// These describe the load generator's own host, not the node under
		// test, which is the wrong host whenever the run is remote.
		avgMetrics := systemCollector.GetAverageMetrics()
		for _, client := range benchmarkResults.ClientMetrics {
			client.SystemMetrics = []types.SystemMetrics{avgMetrics}
		}
	}

	benchmarkResults.Environment = metrics.GetEnvironmentInfo()

	analyzer.NewPerformanceAnalyzer().AnalyzeResults(benchmarkResults)

	if historic != nil {
		savedRun, err := historic.SaveRun(benchmarkResults, cfg)
		if err != nil {
			logger.WithError(err).Error("Failed to save historic run")
		} else {
			logger.WithField("run_id", savedRun.ID).Info("Historic run saved successfully")
		}
	}

	if benchmarkHTMLReport {
		reportPath := filepath.Join(outputDir, "report.html")
		if err := generator.GenerateUltimateHTMLReport(cfg, benchmarkResults, reportPath); err != nil {
			logger.Warnf("Ultimate report generation failed, falling back to enhanced report: %v", err)
			if err := generator.GenerateEnhancedHTMLReport(cfg, benchmarkResults, reportPath); err != nil {
				return fmt.Errorf("failed to generate HTML report: %w", err)
			}
		}
		logger.Infof("Generated HTML report at: %s", reportPath)
	}

	dataExporter := exporter.NewDataExporter(outputDir)
	if err := dataExporter.ExportAll(benchmarkResults); err != nil {
		logger.Warnf("Failed to export data: %v", err)
	} else {
		logger.Info("Exported data to CSV and JSON formats")
	}

	logOutcomes(benchmarkResults)

	for _, breach := range breaches {
		logger.Warnf("Threshold breached: %s", breach)
	}

	// The reports are written either way, because a run that failed is exactly
	// the one worth inspecting. Only the exit code distinguishes them, and it
	// has to: the previous pipeline logged k6's threshold failure as a warning
	// and returned success, so nothing downstream could tell.
	if runErr != nil {
		return fmt.Errorf("the run did not complete: %w", runErr)
	}
	if len(breaches) > 0 && benchmarkFailOnThreshold {
		return fmt.Errorf("%d threshold(s) breached (--fail-on-threshold)", len(breaches))
	}

	logger.Info("Benchmark completed")
	return nil
}

// buildMetricsSink wires remote write when --prometheus was given. It returns a
// nil sink otherwise, which disables export rather than pointing it at nothing.
func buildMetricsSink(cfg *config.Config) (engine.Sink, error) {
	if cfg.Outputs == nil || cfg.Outputs.PrometheusRW == nil {
		return nil, nil
	}

	headers, err := parseHeaderFlags(benchmarkPrometheusHeaders)
	if err != nil {
		return nil, err
	}

	client, err := promrw.New(promrw.Config{
		Endpoint:    cfg.Outputs.PrometheusRW.Endpoint,
		Username:    cfg.Outputs.PrometheusRW.BasicAuth.Username,
		Password:    cfg.Outputs.PrometheusRW.BasicAuth.Password,
		BearerToken: benchmarkPrometheusBearer,
		Headers:     headers,
		UserAgent:   "jsonrpc-bench/" + engine.Version,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to configure remote write: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"endpoint": cfg.Outputs.PrometheusRW.Endpoint,
		"interval": benchmarkPushInterval,
	}).Infof("Publishing %s_* metrics to Prometheus", engine.Namespace)

	return engine.NewPromSink(client), nil
}

func parseHeaderFlags(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for _, raw := range values {
		name, value, ok := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid --prometheus-rw-header %q (want Name=Value)", raw)
		}
		out[name] = strings.TrimSpace(value)
	}
	return out, nil
}

// logOutcomes prints the per-client outcome breakdown. It is the answer to "did
// the node actually serve these requests", which an aggregate error rate built
// from HTTP status alone could not give.
func logOutcomes(result *types.BenchmarkResult) {
	for name, client := range result.ClientMetrics {
		if client.TotalRequests == 0 {
			logger.Warnf("%s recorded no requests", name)
			continue
		}

		parts := make([]string, 0, len(client.ErrorTypes)+1)
		parts = append(parts, fmt.Sprintf("%d ok", client.TotalRequests-client.TotalErrors))
		for _, outcome := range engine.Outcomes {
			if !outcome.IsError() {
				continue
			}
			if n := client.ErrorTypes[string(outcome)]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, outcome))
			}
		}

		logger.Infof("%s: %d requests — %s (error rate %.2f%%, p99 %.1fms)",
			name, client.TotalRequests, strings.Join(parts, ", "), client.ErrorRate, client.Latency.P99)
	}
}
