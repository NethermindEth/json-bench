package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jsonrpc-bench/runner/freshness/probe"
	"github.com/jsonrpc-bench/runner/freshness/review"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

var (
	freshnessProbeConfig  string
	freshnessClientsPath  string
	freshnessRecordAll    bool
	freshnessReviewConfig string
	freshnessOffline      bool
	freshnessRefresh      bool
)

var freshnessCmd = &cobra.Command{
	Use:   "freshness",
	Short: "Measure how soon each EL+CL pair serves correct state and logs for new blocks",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Usage()
		os.Exit(2)
	},
}

var freshnessProbeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Probe one pair live and write its raw freshness data",
	RunE:  runFreshnessProbe,
}

var freshnessReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Verify probe outputs against a reference node and compare pairs",
	RunE:  runFreshnessReview,
}

func init() {
	freshnessProbeCmd.Flags().StringVar(&freshnessProbeConfig, "config", "", "Path to a probe config (see config/freshness/probe.example.yaml)")
	freshnessProbeCmd.Flags().StringVar(&freshnessClientsPath, "clients", "", "Path to clients.yaml, needed when pair.el.client_ref is set")
	freshnessProbeCmd.Flags().BoolVar(&freshnessRecordAll, "record-all-attempts", false, "Write every poll to rpc-attempts.jsonl instead of outcome transitions only")
	_ = freshnessProbeCmd.MarkFlagRequired("config")

	freshnessReviewCmd.Flags().StringVar(&freshnessReviewConfig, "config", "", "Path to a review config (see config/freshness/review.example.yaml)")
	freshnessReviewCmd.Flags().BoolVar(&freshnessOffline, "offline", false, "Use only cached reference data; no network access")
	freshnessReviewCmd.Flags().BoolVar(&freshnessRefresh, "refresh-reference", false, "Re-fetch reference data even when it is cached")
	_ = freshnessReviewCmd.MarkFlagRequired("config")

	freshnessCmd.AddCommand(freshnessProbeCmd, freshnessReviewCmd)
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func runFreshnessProbe(cmd *cobra.Command, args []string) error {
	configureLogger()
	cfg, err := probe.LoadConfig(freshnessProbeConfig)
	if err != nil {
		return err
	}
	if cfg.Pair.EL.ClientRef != "" {
		if freshnessClientsPath == "" {
			return fmt.Errorf("pair.el.client_ref %q needs --clients", cfg.Pair.EL.ClientRef)
		}
		registry, err := loadClientRegistry(freshnessClientsPath)
		if err != nil {
			return err
		}
		if err := cfg.ResolveEL(registry); err != nil {
			return err
		}
	}
	if cmd.Flags().Changed("output") {
		cfg.OutputDirectory = outputDir
	}
	r, err := probe.New(cfg, probe.Options{RecordAllAttempts: freshnessRecordAll, Logger: logger})
	if err != nil {
		return err
	}
	ctx, stop := signalContext()
	defer stop()
	if err := r.Run(ctx); err != nil {
		return err
	}
	fmt.Println(r.Dir())
	return nil
}

func runFreshnessReview(cmd *cobra.Command, args []string) error {
	configureLogger()
	cfg, err := review.LoadConfig(freshnessReviewConfig)
	if err != nil {
		return err
	}
	if cmd.Flags().Changed("output") {
		cfg.OutputDirectory = outputDir
	}
	ctx, stop := signalContext()
	defer stop()
	res, err := review.Run(ctx, cfg, review.Options{Offline: freshnessOffline, Refresh: freshnessRefresh, Logger: logger})
	if err != nil {
		return err
	}
	for _, w := range res.Summary.Warnings {
		logger.Warn(w)
	}
	logger.Infof("reviewed %d block identities across %d probes", len(res.Blocks), len(res.Summary.Probes))
	fmt.Println(filepath.Join(res.Dir, schema.ReportFile))
	return nil
}
