package main

import (
	"log/slog"
	"os"

	"github.com/ponchione/agent-conductor/internal/logging"
	"github.com/spf13/cobra"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "conductor",
	Short: "Agent Conductor — orchestrate AI coding agents",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		h := logging.NewHandler(os.Stdout, &slog.HandlerOptions{Level: level})
		slog.SetDefault(slog.New(h))
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")
}

func main() {
	rootCmd.AddCommand(runCmd, approveCmd, rejectCmd, statusCmd, statsCmd, indexCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
