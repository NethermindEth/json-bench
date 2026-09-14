package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/engine"
)

var (
	reportCompare bool
	reportJSON    bool
	reportClient  string
	reportWarmup  bool
	reportOkOnly  bool
	reportPhases  bool
)

var reportCmd = &cobra.Command{
	Use:   "report <run-dir> [run-dir]",
	Short: "Re-analyse a finished run from its retained samples, or compare two runs",
	Long: `Rebuild a run's breakdown from the samples it retained, without re-running it.

With --compare and two run directories, diff them call by call: the median shift,
a rank-sum test, the waiting-time split, and the outcome counts. The comparison
refuses runs that counted errors differently and warns about the settings that
change what a difference means.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runReport,
}

func init() {
	reportCmd.Flags().BoolVar(&reportCompare, "compare", false, "Compare two runs rather than summarising one")
	reportCmd.Flags().BoolVar(&reportJSON, "json", false, "Emit JSON instead of a table")
	reportCmd.Flags().StringVar(&reportClient, "client", "", "Restrict to one client (default: every client in the run)")
	reportCmd.Flags().BoolVar(&reportWarmup, "include-warmup", false, "Include warmup requests, which a run excludes by design")
	reportCmd.Flags().BoolVar(&reportOkOnly, "ok-only", false, "Restrict to successful responses, so latency is not mixed with fast failures")
	reportCmd.Flags().BoolVar(&reportPhases, "phases", false, "Show the sending/waiting/receiving split per call")
	rootCmd.AddCommand(reportCmd)
}

func reportOptions() engine.ReportOptions {
	opts := engine.ReportOptions{Client: reportClient, Warmup: reportWarmup}
	if reportOkOnly {
		opts.Outcome = string(engine.OutcomeOK)
	}
	return opts
}

func runReport(cmd *cobra.Command, args []string) error {
	configureLogger()

	if reportCompare {
		if len(args) != 2 {
			return fmt.Errorf("--compare needs two run directories: report --compare <baseline> <current>")
		}
		return reportComparison(args[0], args[1])
	}
	if len(args) != 1 {
		return fmt.Errorf("pass one run directory, or --compare with two")
	}
	return reportSingle(args[0])
}

func reportSingle(dir string) error {
	run, err := engine.LoadRun(dir)
	if err != nil {
		return err
	}
	opts := reportOptions()

	if reportJSON {
		return writeJSON(map[string]any{
			"manifest": run.Manifest,
			"totals":   run.Totals(opts),
			"calls":    run.CallReport(opts),
		})
	}

	if run.Truncated {
		logger.Warnf("%s ends mid-record, so this covers only the %d requests that were written "+
			"before the run stopped", engine.SampleFilename, len(run.Samples))
	}

	m := run.Manifest
	fmt.Printf("%s — %s\n", m.TestName, dir)
	fmt.Printf("  engine %s v%s, seed %d, %s policy, %d rps target, %d concurrent, %s\n",
		m.Engine, m.EngineVersion, m.Seed, m.Saturation, m.TargetRPS, m.Concurrency, m.Duration)
	for _, c := range m.Clients {
		fmt.Printf("  %s: %s (chain %s, head %d)\n", c.Name, c.ClientVersion, c.ChainID, c.HeadBlock)
	}
	if clients := run.Clients(); len(clients) > 1 && reportClient == "" {
		fmt.Printf("  covering %d clients together; pass --client to separate them\n", len(clients))
	}
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	header := "CALL\tMETHOD\tCOUNT\tERRORS\tP50\tP90\tP95\tP99\tMAX\tAVG"
	if reportPhases {
		header += "\tSEND\tWAIT\tRECV"
	}
	fmt.Fprintln(w, header+"\t")

	rows := append([]*engine.CallStats(nil), run.CallReport(opts)...)
	rows = append(rows, run.Totals(opts))
	for _, c := range rows {
		d := c.Duration
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f",
			c.Name, c.Method, c.Count, c.Errors, d.P50, d.P90, d.P95, d.P99, d.Max, d.Avg)
		if reportPhases {
			fmt.Fprintf(w, "\t%.3f\t%.2f\t%.3f", c.Sending.Avg, c.Waiting.Avg, c.Receiving.Avg)
		}
		fmt.Fprintln(w, "\t")
	}
	w.Flush()

	fmt.Println("\nOutcomes")
	printOutcomes(run.Totals(opts).Outcomes)
	return nil
}

func printOutcomes(outcomes map[string]int64) {
	var total int64
	for _, n := range outcomes {
		total += n
	}
	names := make([]string, 0, len(outcomes))
	for name := range outcomes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		n := outcomes[name]
		fmt.Printf("  %-12s %8d  %5.2f%%\n", name, n, float64(n)/float64(total)*100)
	}
}

func reportComparison(baselineDir, currentDir string) error {
	baseline, err := engine.LoadRun(baselineDir)
	if err != nil {
		return err
	}
	current, err := engine.LoadRun(currentDir)
	if err != nil {
		return err
	}

	diff := engine.CompareRuns(baseline, current, reportOptions())

	if reportJSON {
		if err := writeJSON(diff); err != nil {
			return err
		}
		if !diff.Comparable {
			return fmt.Errorf("the runs are not comparable")
		}
		return nil
	}

	fmt.Printf("baseline  %s  (%s)\n", diff.Baseline, baseline.Manifest.TestName)
	fmt.Printf("current   %s  (%s)\n\n", diff.Current, current.Manifest.TestName)

	if !diff.Comparable {
		fmt.Printf("These runs cannot be compared: %s\n", diff.Refusal)
		return fmt.Errorf("the runs are not comparable")
	}
	for _, warning := range diff.Warnings {
		fmt.Printf("warning: %s\n", warning)
	}
	if len(diff.Warnings) > 0 {
		fmt.Println()
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "CALL\tCOUNT\tP50 BASE\tP50 CURR\tSHIFT\tP99 BASE\tP99 CURR\tWAIT BASE\tWAIT CURR\tERR\t")
	for _, c := range append(diff.Calls, diff.Totals) {
		if c.OnlyIn != "" {
			fmt.Fprintf(w, "%s\t\t\t\t\t\t\t\t\tonly in %s\t\n", c.Name, c.OnlyIn)
			continue
		}
		counts := fmt.Sprintf("%d", c.CurrentCount)
		if c.BaselineCount != c.CurrentCount {
			counts = fmt.Sprintf("%d->%d", c.BaselineCount, c.CurrentCount)
		}
		shift := fmt.Sprintf("%+.1f%%", c.MedianShiftPercent)
		if c.Significant {
			shift += "*"
		}
		errs := fmt.Sprintf("%d->%d", c.BaselineErrors, c.CurrentErrors)
		fmt.Fprintf(w, "%s\t%s\t%.2f\t%.2f\t%s\t%.2f\t%.2f\t%.2f\t%.2f\t%s\t\n",
			c.Name, counts, c.BaselineP50, c.CurrentP50, shift,
			c.BaselineP99, c.CurrentP99, c.BaselineWaiting, c.CurrentWaiting, errs)
	}
	w.Flush()
	fmt.Println("\n* the rank-sum test puts the shift beyond noise (p < 0.05). The shift itself is the")
	fmt.Println("  number to read: at these sample sizes almost any real difference is significant.")
	return nil
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
