package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/logging"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/templates"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init <spec-file>",
	Short: "Generate a project.yaml and bootstrap work order from a spec file",
	Long: `init reads a freeform specification file, sends it to Claude Code with a
dedicated init system prompt, and generates a project.yaml configuration file
and a bootstrap work order in the output directory. This is intended for
greenfield projects that don't yet have a conductor configuration.`,
	Args: cobra.ExactArgs(1),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		h := logging.NewHandler(os.Stdout, &slog.HandlerOptions{Level: level})
		slog.SetDefault(slog.New(h))
		return nil
	},
	RunE: runInit,
}

var (
	initOutputDir string
	initTimeout   int
)

func init() {
	initCmd.Flags().StringVar(&initOutputDir, "output", ".", "Output directory for generated files")
	initCmd.Flags().IntVar(&initTimeout, "timeout", 300, "Timeout in seconds for the Claude invocation")
}

// initProjectConfig mirrors the project_config JSON from Claude's init response.
type initProjectConfig struct {
	ProjectName      string   `json:"project_name"`
	ProjectLanguage  string   `json:"project_language"`
	ProjectFramework string   `json:"project_framework"`
	IndexInclude     []string `json:"index_include"`
	IndexExclude     []string `json:"index_exclude"`
	ModulePath       string   `json:"module_path"`
	ModuleStructure  []string `json:"module_structure"`
	SharedPath       string   `json:"shared_path"`
	SQLPath          string   `json:"sql_path"`
}

// initBootstrapWO mirrors the bootstrap work order JSON from Claude's init response.
type initBootstrapWO struct {
	Title              string   `json:"title"`
	Type               string   `json:"type"`
	TargetModule       string   `json:"target_module"`
	ReferenceModule    string   `json:"reference_module"`
	KnownFiles         []string `json:"known_files"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Constraints        []string `json:"constraints"`
}

// initResponse is the top-level JSON envelope from Claude's init response.
type initResponse struct {
	ProjectConfig initProjectConfig `json:"project_config"`
	Bootstrap     initBootstrapWO   `json:"bootstrap"`
}

func runInit(cmd *cobra.Command, args []string) error {
	specPath := args[0]
	specData, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("failed to read spec file %q: %w", specPath, err)
	}

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude binary not found in PATH: %w", err)
	}
	slog.Debug("resolved claude binary", "path", claudePath)

	userMsg := fmt.Sprintf("=== SPECIFICATION ===\n%s\n", string(specData))
	timeout := time.Duration(initTimeout) * time.Second

	slog.Info("invoking Claude for init", "timeout", timeout)
	raw, err := invokeClaude(claudePath, templates.DefaultInitPrompt, userMsg, timeout, "")
	if err != nil {
		return fmt.Errorf("claude invocation failed: %w", err)
	}

	resp, parseErr := parseInitResponse(raw)
	if parseErr != nil {
		slog.Warn("first parse attempt failed, retrying with correction", "error", parseErr)

		correctionMsg := fmt.Sprintf(
			"%s\n\n=== CORRECTION ===\nYour previous response could not be parsed: %s\n\nPrevious response:\n%s\n\nPlease respond with ONLY a valid JSON object matching the schema. No markdown, no commentary.",
			userMsg, parseErr.Error(), raw,
		)

		raw, err = invokeClaude(claudePath, templates.DefaultInitPrompt, correctionMsg, timeout, "")
		if err != nil {
			return fmt.Errorf("claude retry invocation failed: %w", err)
		}

		resp, parseErr = parseInitResponse(raw)
		if parseErr != nil {
			if mkErr := os.MkdirAll(initOutputDir, 0755); mkErr != nil {
				return fmt.Errorf("parse failed and could not create output dir: parse=%w, mkdir=%v", parseErr, mkErr)
			}
			rawPath := filepath.Join(initOutputDir, "raw-init-output.txt")
			if wErr := os.WriteFile(rawPath, []byte(raw), 0644); wErr != nil {
				return fmt.Errorf("parse failed and could not write raw output: parse=%w, write=%v", parseErr, wErr)
			}
			return fmt.Errorf("could not parse init response after retry: %w\nRaw output saved to: %s", parseErr, rawPath)
		}
	}

	if err := os.MkdirAll(initOutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := writeInitProjectYAML(&resp.ProjectConfig, initOutputDir); err != nil {
		return fmt.Errorf("failed to write project.yaml: %w", err)
	}

	if err := writeBootstrapYAML(&resp.Bootstrap, initOutputDir); err != nil {
		return fmt.Errorf("failed to write bootstrap.yaml: %w", err)
	}

	projectYAMLPath := filepath.Join(initOutputDir, "project.yaml")
	bootstrapYAMLPath := filepath.Join(initOutputDir, "bootstrap.yaml")
	fmt.Printf("\nGenerated files:\n")
	fmt.Printf("  %s\n", projectYAMLPath)
	fmt.Printf("  %s\n", bootstrapYAMLPath)
	fmt.Printf("\nReminder: update project.path in %s to your actual project path.\n", projectYAMLPath)

	return nil
}

func parseInitResponse(raw string) (*initResponse, error) {
	cleaned := llm.CleanLLMResponse(raw)

	var resp initResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %w", err)
	}

	if resp.ProjectConfig.ProjectName == "" {
		return nil, fmt.Errorf("response missing project_config.project_name")
	}
	if resp.Bootstrap.Title == "" {
		return nil, fmt.Errorf("response missing bootstrap.title")
	}

	// Force bootstrap type
	resp.Bootstrap.Type = "bootstrap"

	// Validate the bootstrap WO
	wo := models.WorkOrder{
		Title:              resp.Bootstrap.Title,
		Type:               resp.Bootstrap.Type,
		TargetModule:       resp.Bootstrap.TargetModule,
		ReferenceModule:    resp.Bootstrap.ReferenceModule,
		KnownFiles:         resp.Bootstrap.KnownFiles,
		AcceptanceCriteria: resp.Bootstrap.AcceptanceCriteria,
		Constraints:        resp.Bootstrap.Constraints,
	}
	if err := wo.Validate(); err != nil {
		return nil, fmt.Errorf("bootstrap work order: %w", err)
	}

	return &resp, nil
}

// writeInitProjectYAML writes a project.yaml from the init response.
// Uses string building (not yaml.Marshal) to include comments.
func writeInitProjectYAML(pc *initProjectConfig, outputDir string) error {
	var sb strings.Builder
	sb.WriteString("# Generated by conductor init — update project.path before running\n")
	sb.WriteString("project:\n")
	fmt.Fprintf(&sb, "  name: %q\n", pc.ProjectName)
	sb.WriteString("  path: \"/path/to/your/project\"  # UPDATE THIS to your actual project path\n")
	fmt.Fprintf(&sb, "  language: %q\n", pc.ProjectLanguage)
	if pc.ProjectFramework != "" {
		fmt.Fprintf(&sb, "  framework: %q\n", pc.ProjectFramework)
	}

	sb.WriteString("\nindex:\n")
	sb.WriteString("  include:\n")
	for _, g := range pc.IndexInclude {
		fmt.Fprintf(&sb, "    - %q\n", g)
	}
	sb.WriteString("  exclude:\n")
	for _, g := range pc.IndexExclude {
		fmt.Fprintf(&sb, "    - %q\n", g)
	}
	sb.WriteString("  max_rag_results: 30\n")
	sb.WriteString("  max_tree_lines: 200\n")
	sb.WriteString("  auto_reindex: true\n")

	sb.WriteString("\nconventions:\n")
	if pc.ModulePath != "" {
		fmt.Fprintf(&sb, "  module_path: %q\n", pc.ModulePath)
	}
	if len(pc.ModuleStructure) > 0 {
		sb.WriteString("  module_structure:\n")
		for _, m := range pc.ModuleStructure {
			fmt.Fprintf(&sb, "    - %q\n", m)
		}
	}
	if pc.SharedPath != "" {
		fmt.Fprintf(&sb, "  shared_path: %q\n", pc.SharedPath)
	}
	if pc.SQLPath != "" {
		fmt.Fprintf(&sb, "  sql_path: %q\n", pc.SQLPath)
	}

	sb.WriteString("\nprompts:\n")
	sb.WriteString("  scope: templates/scope-prompt.md\n")
	sb.WriteString("  verify: templates/verify-prompt.md\n")
	sb.WriteString("  build: templates/build-prompt.md\n")

	sb.WriteString("\nexecutor:\n")
	sb.WriteString("  tool: claude-code\n")
	sb.WriteString("  timeout_minutes: 30\n")

	sb.WriteString("\nsafety:\n")
	sb.WriteString("  max_files_changed: 50\n")
	sb.WriteString("  max_duration_mins: 60\n")

	sb.WriteString("\nguardrails:\n")
	sb.WriteString("  max_investigation_targets: 6\n")
	sb.WriteString("  max_sub_calls_total: 12\n")
	sb.WriteString("  phase_timeout_seconds: 300\n")
	sb.WriteString("  max_cost_per_phase_usd: 0.50\n")
	sb.WriteString("  warn_cost_per_phase_usd: 0.10\n")

	path := filepath.Join(outputDir, "project.yaml")
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	slog.Debug("wrote project.yaml", "path", path)
	return nil
}

// writeBootstrapYAML writes the bootstrap work order as bootstrap.yaml.
func writeBootstrapYAML(bwo *initBootstrapWO, outputDir string) error {
	ordered := orderedWorkOrder{
		Title:              bwo.Title,
		Type:               "bootstrap",
		TargetModule:       bwo.TargetModule,
		ReferenceModule:    bwo.ReferenceModule,
		KnownFiles:         bwo.KnownFiles,
		AcceptanceCriteria: bwo.AcceptanceCriteria,
		Constraints:        bwo.Constraints,
	}

	data, err := yaml.Marshal(ordered)
	if err != nil {
		return fmt.Errorf("failed to marshal bootstrap work order: %w", err)
	}

	path := filepath.Join(outputDir, "bootstrap.yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	slog.Debug("wrote bootstrap.yaml", "path", path)
	return nil
}
