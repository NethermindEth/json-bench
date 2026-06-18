package cmd

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	outputDir string
	logLevel  string

	logger = logrus.New()
)

var rootCmd = &cobra.Command{
	Use:           "runner",
	Short:         "JSON-RPC benchmark runner",
	SilenceUsage:  true,
	SilenceErrors: true,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Usage()
		os.Exit(2)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&outputDir, "output", "outputs", "Directory to store outputs")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "Log level: debug, info, warn, error (falls back to $LOG_LEVEL, then info)")
	rootCmd.AddCommand(benchmarkCmd, apiCmd, historicCmd, compareCmd, compareOpenRPCCmd)
}

func Execute() int {
	cmd, err := rootCmd.ExecuteC()
	if err != nil {
		logger.WithError(err).Error(cmd.Name() + " failed")
		return 1
	}
	return 0
}

func configureLogger() {
	level := logLevel
	if level == "" {
		level = os.Getenv("LOG_LEVEL")
	}
	switch level {
	case "debug":
		logger.SetLevel(logrus.DebugLevel)
	case "warn", "warning":
		logger.SetLevel(logrus.WarnLevel)
	case "error":
		logger.SetLevel(logrus.ErrorLevel)
	default:
		logger.SetLevel(logrus.InfoLevel)
	}
}
