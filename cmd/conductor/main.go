package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "conductor",
	Short: "Agent Conductor — orchestrate AI coding agents",
}

func main() {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))

	rootCmd.AddCommand(runCmd, approveCmd, rejectCmd, statusCmd, statsCmd, indexCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
