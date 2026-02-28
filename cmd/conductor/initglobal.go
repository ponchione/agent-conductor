package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/logging"
	"github.com/spf13/cobra"
)

var initGlobalCmd = &cobra.Command{
	Use:   "init-global",
	Short: "Create ~/.conductor/config.yaml with default machine-level settings",
	Long: `init-global creates ~/.conductor/config.yaml populated with default
machine-level settings (local_model, embed_model, git). These settings apply
to all projects on this machine and can be overridden by individual project.yaml files.

If the file already exists, the command prints its path and exits without modifying it.`,
	Args: cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		h := logging.NewHandler(os.Stdout, &slog.HandlerOptions{Level: level})
		slog.SetDefault(slog.New(h))
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not determine home directory: %w", err)
		}
		dir := filepath.Join(home, ".conductor")
		path := filepath.Join(dir, "config.yaml")

		if _, err := os.Stat(path); err == nil {
			fmt.Printf("Global config already exists: %s\n", path)
			return nil
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create ~/.conductor: %w", err)
		}
		if err := os.WriteFile(path, []byte(globalConfigTemplate), 0644); err != nil {
			return fmt.Errorf("failed to write global config: %w", err)
		}
		fmt.Printf("Created global config: %s\n", path)
		return nil
	},
}

const globalConfigTemplate = `# Global conductor configuration — applies to all projects on this machine.
# Values here are overridden by matching fields in project.yaml.

local_model:
  endpoint: "http://localhost:8080/v1"
  model_name: "deepseek-r1"
  temperature: 0.0
  timeout_seconds: 120

embed_model:
  endpoint: "http://localhost:8081/v1"
  model_name: "nomic-embed-text-v1.5"
  timeout_seconds: 30

git:
  branch_prefix: "feature/conducted"
  commit_author_name: "Agent Conductor"
  commit_author_email: "conductor@local"
`
