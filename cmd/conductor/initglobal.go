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
	Short: "Create ~/source/.conductor/config.yaml with default machine-level settings",
	Long: `init-global creates ~/source/.conductor/config.yaml populated with default
machine-level settings (models, embed_model, git). These settings apply
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
		dir := filepath.Join(home, "source", ".conductor")
		path := filepath.Join(dir, "config.yaml")

		if _, err := os.Stat(path); err == nil {
			fmt.Printf("Global config already exists: %s\n", path)
			return nil
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create ~/source/.conductor: %w", err)
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

embed_model:
  endpoint: "http://localhost:8081/v1"
  model_name: "nomic-embed-text-v1.5"
  timeout_seconds: 30

git:
  branch_prefix: "feature/conducted"
  commit_author_name: "Agent Conductor"
  commit_author_email: "conductor@local"

# Provider types:
#   "openai"     — any OpenAI-compatible endpoint (local or remote)
#   "claude-cli" — shells out to the claude binary (requires claude CLI installed)
models:
  providers:
    local:
      type: openai
      endpoint: "http://localhost:8080/v1"
      model: "deepseek-r1"
      timeout_seconds: 120
      temperature: 0.0
    claude-sonnet:
      type: claude-cli
      model: "claude-sonnet-4-6"
      timeout_seconds: 300

  # Role mappings — each orchestrator phase uses the named provider.
  # Switch any role to "claude-sonnet" for higher quality at higher cost.
  roles:
    decompose: local
    analyze: local
    crosscut: local
    synthesize: local
    describe: local
    verify_analyze: local
    verify_synthesize: local
`
