package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/engine"
)

var (
	sweepConfigPath  string
	sweepClientsPath string
	sweepVary        string
	sweepRepeat      int
	sweepSettle      time.Duration
	sweepSkipPre     bool
	sweepCompression bool
	sweepNoConnReuse bool
	sweepHTTP2       bool
	sweepSaturation  string
)

var sweepCmd = &cobra.Command{
	Use:   "sweep",
	Short: "Run one config at several settings of one dimension and compare them",
	Long: `Run the same benchmark at several values of one setting and report them side by side.

Every run replays the same request sequence, pinned once up front, so the
comparison is of settings rather than of workloads. The sequence is sized for
whichever setting needs the most requests.

A single pass cannot separate a difference between settings from the target
drifting underneath them. --repeat runs the sweep again and reports each point's
spread, which is what tells you whether the differences are larger than the
noise.`,
	Example: `  runner sweep --config bench.yaml --vary batch_size=1,5,10,20
  runner sweep --config bench.yaml --vary rps=100,200,400 --repeat 3`,
	RunE: runSweep,
}

func init() {
	sweepCmd.Flags().StringVar(&sweepConfigPath, "config", "", "Path to YAML benchmark configuration file")
	sweepCmd.Flags().StringVar(&sweepClientsPath, "clients", "", "Path to clients configuration file (optional)")
	sweepCmd.Flags().StringVar(&sweepVary, "vary", "", "Setting and values to sweep, as name=v1,v2,... (rps, vus, batch_size, duration)")
	sweepCmd.Flags().IntVar(&sweepRepeat, "repeat", 1, "Run the whole sweep this many times and report each point's spread")
	sweepCmd.Flags().DurationVar(&sweepSettle, "settle", 5*time.Second, "Wait between runs so one run's queue does not spill into the next")
	sweepCmd.Flags().BoolVar(&sweepSkipPre, "skip-preflight", false, "Do not identify the target before the sweep")
	sweepCmd.Flags().StringVar(&sweepSaturation, "on-saturation", "queue", "What to do when every slot is busy: queue, drop or abort")
	sweepCmd.Flags().BoolVar(&sweepCompression, "accept-compression", false, "Send Accept-Encoding and decompress responses")
	sweepCmd.Flags().BoolVar(&sweepNoConnReuse, "no-connection-reuse", false, "Open a new connection per request")
	sweepCmd.Flags().BoolVar(&sweepHTTP2, "http2", false, "Allow HTTP/2")
	rootCmd.AddCommand(sweepCmd)
}

func parseVary(spec string) (engine.SweepDimension, []string, error) {
	name, rest, ok := strings.Cut(spec, "=")
	if !ok {
		return "", nil, fmt.Errorf("--vary takes name=v1,v2,... (for example --vary batch_size=1,5,10)")
	}
	dimension, err := engine.ParseSweepDimension(strings.TrimSpace(name))
	if err != nil {
		return "", nil, err
	}

	var values []string
	seen := map[string]struct{}{}
	for _, raw := range strings.Split(rest, ",") {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, dup := seen[value]; dup {
			return "", nil, fmt.Errorf("%s=%s is listed twice", dimension, value)
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	if len(values) < 2 {
		return "", nil, fmt.Errorf("a sweep needs at least two values to compare, got %d", len(values))
	}
	return dimension, values, nil
}

func runSweep(cmd *cobra.Command, args []string) error {
	configureLogger()

	if sweepConfigPath == "" {
		return fmt.Errorf("--config is required")
	}
	dimension, values, err := parseVary(sweepVary)
	if err != nil {
		return err
	}
	saturation, err := engine.ParseSaturation(sweepSaturation)
	if err != nil {
		return err
	}

	registry, err := loadClientRegistry(sweepClientsPath)
	if err != nil {
		return err
	}
	cfg, err := loadBenchmarkConfig(sweepConfigPath, sweepClientsPath, registry)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	opts := engine.DefaultOptions()
	opts.Logger = logger
	opts.Saturation = saturation
	opts.SkipPreflight = sweepSkipPre
	opts.Transport.AcceptCompression = sweepCompression
	opts.Transport.ReuseConnections = !sweepNoConnReuse
	opts.Transport.HTTP2 = sweepHTTP2

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Infof("Sweeping %s over %s, %d pass(es)", dimension, strings.Join(values, ", "), sweepRepeat)

	result, runErr := engine.Sweep(ctx, cfg, opts, engine.SweepOptions{
		Dimension: dimension,
		Values:    values,
		Repeat:    sweepRepeat,
		Settle:    sweepSettle,
		OutputDir: outputDir,
	})
	if result == nil {
		return runErr
	}

	path := filepath.Join(outputDir, "sweep.json")
	if err := writeSweepResult(path, result); err != nil {
		logger.WithError(err).Warn("Failed to write the sweep result")
	} else {
		logger.Infof("Sweep result written to %s", path)
	}

	logSweepResult(result)
	return runErr
}

func writeSweepResult(path string, result *engine.SweepResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func logSweepResult(result *engine.SweepResult) {
	for _, warning := range result.Warnings {
		logger.Warn(warning)
	}

	fmt.Printf("\n%s sweep", result.Dimension)
	if result.Client != "" {
		fmt.Printf(" against %s", result.Client)
	}
	if result.Repeat > 1 {
		fmt.Printf(", %d passes each", result.Repeat)
	}
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	header := "SETTING\tREQUESTS\tACHIEVED RPS\tDELIVERED\tP50\tP95\tP99\tERRORS\t"
	if result.Repeat > 1 {
		header += "P99 SPREAD\t"
	}
	fmt.Fprintln(w, header)
	for _, point := range result.Points {
		fmt.Fprintf(w, "%s\t%d\t%.1f\t%.1f%%\t%.2f\t%.2f\t%.2f\t%.2f%%\t",
			point.Label, point.Requests, point.AchievedRPS, point.DeliveredPct,
			point.P50Ms, point.P95Ms, point.P99Ms, point.ErrorRate)
		if result.Repeat > 1 {
			fmt.Fprintf(w, "%.1f%%\t", point.SpreadP99Pct)
		}
		fmt.Fprintln(w)
	}
	w.Flush()

	for _, point := range result.Points {
		if point.Inconclusive {
			fmt.Printf("\n%s is inconclusive: %s\n", point.Label, point.InconclusiveB)
		}
	}
	if result.Repeat > 1 {
		fmt.Println("\nP99 SPREAD is the gap between passes at the same setting. Treat a difference")
		fmt.Println("between settings as real only when it is larger than that.")
	}
}
