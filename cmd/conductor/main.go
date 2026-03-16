package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/logging"
	"github.com/spf13/cobra"
)

var (
	verbose     bool
	projectPath string
	cfg         *config.ProjectConfig
)

var rootCmd = &cobra.Command{
	Use:   "conductor",
	Short: "Agent Conductor — orchestrate AI coding agents",
	Long: `conductor orchestrates AI coding agents against a project repository.

It reads a project.yaml config, accepts work orders (YAML files), and drives
an agent through scope → build → verify → human_review phases.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		configureLogging()
		return loadProjectConfig()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")
	rootCmd.PersistentFlags().StringVar(&projectPath, "project", "project.yaml", "Path to project config file")
}

func configureLogging() {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	h := logging.NewHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}

func loadProjectConfig() error {
	var err error
	cfg, err = config.Load(projectPath)
	if err != nil {
		return fmt.Errorf("could not load project config %q: %w\n  Hint: pass --project <path>", projectPath, err)
	}
	return nil
}

func main() {
	rootCmd.AddCommand(runCmd, approveCmd, rejectCmd, statusCmd, statsCmd, indexCmd, listCmd, sessionsCmd, sessionCmd, serveCmd, initGlobalCmd, initCmd, planCmd, ragDumpCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
