package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/logging"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/templates"
	"github.com/ponchione/agent-conductor/internal/util"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planCmd = &cobra.Command{
	Use:   "plan <spec-file>",
	Short: "Generate work orders from a spec file via Claude Code",
	Long: `plan reads a freeform specification file, sends it to Claude Code with a
planning system prompt, and generates individual work order YAML files in
the output directory. This enables rapid decomposition of feature specs into
the work order format the conductor pipeline consumes.`,
	Args: cobra.ExactArgs(1),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		h := logging.NewHandler(os.Stdout, &slog.HandlerOptions{Level: level})
		slog.SetDefault(slog.New(h))

		loaded, err := config.Load(projectPath)
		if err != nil {
			slog.Debug("no project config loaded, running in greenfield mode", "error", err)
			cfg = nil
		} else {
			cfg = loaded
		}
		return nil
	},
	RunE: runPlan,
}

var (
	planOutputDir string
	planTimeout   int
)

func init() {
	planCmd.Flags().StringVar(&planOutputDir, "output", "./work-orders/", "Output directory for generated work order files")
	planCmd.Flags().IntVar(&planTimeout, "timeout", 300, "Timeout in seconds for the Claude invocation")
}

func runPlan(cmd *cobra.Command, args []string) error {
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

	userMsg := buildPlanUserMessage(string(specData), cfg)
	timeout := time.Duration(planTimeout) * time.Second

	slog.Info("invoking Claude for planning", "timeout", timeout)
	raw, err := invokeClaude(claudePath, templates.DefaultPlanPrompt, userMsg, timeout)
	if err != nil {
		return fmt.Errorf("claude invocation failed: %w", err)
	}

	workOrders, parseErr := parsePlanResponse(raw)
	if parseErr != nil {
		slog.Warn("first parse attempt failed, retrying with correction", "error", parseErr)

		correctionMsg := fmt.Sprintf(
			"%s\n\n=== CORRECTION ===\nYour previous response could not be parsed: %s\n\nPrevious response:\n%s\n\nPlease respond with ONLY a valid JSON object matching the schema. No markdown, no commentary.",
			userMsg, parseErr.Error(), raw,
		)

		raw, err = invokeClaude(claudePath, templates.DefaultPlanPrompt, correctionMsg, timeout)
		if err != nil {
			return fmt.Errorf("claude retry invocation failed: %w", err)
		}

		workOrders, parseErr = parsePlanResponse(raw)
		if parseErr != nil {
			if mkErr := os.MkdirAll(planOutputDir, 0755); mkErr != nil {
				return fmt.Errorf("parse failed and could not create output dir: parse=%w, mkdir=%v", parseErr, mkErr)
			}
			rawPath := filepath.Join(planOutputDir, "raw-plan-output.txt")
			if wErr := os.WriteFile(rawPath, []byte(raw), 0644); wErr != nil {
				return fmt.Errorf("parse failed and could not write raw output: parse=%w, write=%v", parseErr, wErr)
			}
			return fmt.Errorf("could not parse plan response after retry: %w\nRaw output saved to: %s", parseErr, rawPath)
		}
	}

	if err := writeWorkOrderFiles(workOrders, planOutputDir); err != nil {
		return fmt.Errorf("failed to write work order files: %w", err)
	}

	fmt.Printf("\nGenerated %d work order(s) in %s:\n\n", len(workOrders), planOutputDir)
	for i, wo := range workOrders {
		fmt.Printf("  %03d  %-50s  [%s]\n", i+1, wo.Title, wo.Type)
	}
	fmt.Println()

	return nil
}

// buildPlanUserMessage assembles the user message sent to Claude for planning.
func buildPlanUserMessage(spec string, cfg *config.ProjectConfig) string {
	var sb strings.Builder

	sb.WriteString("=== SPECIFICATION ===\n")
	sb.WriteString(spec)
	sb.WriteString("\n")

	if cfg == nil {
		sb.WriteString("\n=== NOTE ===\n")
		sb.WriteString("No existing project configuration was found. This appears to be a greenfield project.\n")
		sb.WriteString("Generate work orders based solely on the specification above.\n")
		return sb.String()
	}

	sb.WriteString("\n=== PROJECT CONTEXT ===\n")

	if convSection := planBuildConventions(cfg.Conventions); convSection != "" {
		sb.WriteString("\n=== PROJECT CONVENTIONS ===\n")
		sb.WriteString(convSection)
	}

	treeStr, totalFiles := planBuildFileTree(cfg)
	sb.WriteString("\n=== PROJECT FILE TREE ===\n")
	if treeStr != "" {
		sb.WriteString(treeStr)
		treeLines := strings.Count(treeStr, "\n")
		if totalFiles > treeLines {
			fmt.Fprintf(&sb, "... (%d more files not shown)\n", totalFiles-treeLines)
		}
	} else {
		sb.WriteString("(no matching files)\n")
	}

	return sb.String()
}

// planBuildConventions formats project conventions for the plan prompt.
// Reimplements the unexported buildConventions from internal/context/assembly.go.
func planBuildConventions(conv config.Conventions) string {
	if conv.ModulePath == "" && len(conv.ModuleStructure) == 0 &&
		conv.SharedPath == "" && conv.SQLPath == "" && conv.DocsPath == "" {
		return ""
	}

	var sb strings.Builder
	if conv.ModulePath != "" {
		fmt.Fprintf(&sb, "Module path: %s\n", conv.ModulePath)
	}
	if len(conv.ModuleStructure) > 0 {
		fmt.Fprintf(&sb, "Module structure: %s\n", strings.Join(conv.ModuleStructure, ", "))
	}
	if conv.SharedPath != "" {
		fmt.Fprintf(&sb, "Shared path: %s\n", conv.SharedPath)
	}
	if conv.SQLPath != "" {
		fmt.Fprintf(&sb, "SQL path: %s\n", conv.SQLPath)
	}
	if conv.DocsPath != "" {
		fmt.Fprintf(&sb, "Docs path: %s\n", conv.DocsPath)
	}
	return sb.String()
}

// planBuildFileTree walks the project directory and returns an indented file tree.
// Reimplements the unexported buildFileTree from internal/context/assembly.go.
func planBuildFileTree(cfg *config.ProjectConfig) (string, int) {
	root := cfg.Project.Path
	if _, err := os.Stat(root); err != nil {
		slog.Warn("project path not accessible for file tree", "path", root, "error", err)
		return "", 0
	}

	var matched []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		if !util.MatchesAnyGlob(cfg.Index.Include, relPath) {
			return nil
		}
		if util.MatchesAnyGlob(cfg.Index.Exclude, relPath) {
			return nil
		}
		matched = append(matched, relPath)
		return nil
	})

	sort.Strings(matched)
	totalFiles := len(matched)
	if totalFiles == 0 {
		return "", 0
	}

	cap := cfg.Index.MaxTreeLines
	if cap <= 0 {
		cap = 200
	}

	var sb strings.Builder
	emittedDirs := make(map[string]bool)
	fileLines := 0

	for _, rel := range matched {
		if fileLines >= cap {
			break
		}

		parts := strings.Split(rel, "/")
		for depth := 0; depth < len(parts)-1; depth++ {
			dirKey := strings.Join(parts[:depth+1], "/")
			if !emittedDirs[dirKey] {
				emittedDirs[dirKey] = true
				indent := strings.Repeat("  ", depth)
				fmt.Fprintf(&sb, "%s%s/\n", indent, parts[depth])
			}
		}

		indent := strings.Repeat("  ", len(parts)-1)
		fmt.Fprintf(&sb, "%s%s\n", indent, parts[len(parts)-1])
		fileLines++
	}

	return sb.String(), totalFiles
}

// invokeClaude runs the claude binary with --print mode and returns stdout.
func invokeClaude(claudePath, systemPrompt, userMsg string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, claudePath,
		"--print",
		"--dangerously-skip-permissions",
		"--system-prompt", systemPrompt,
		userMsg,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("claude timed out after %s", timeout)
		}
		return "", fmt.Errorf("claude exited with error: %w\nstderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// planWorkOrder is the JSON deserialization type for a single work order from Claude's response.
// Separate from models.WorkOrder which uses yaml tags.
type planWorkOrder struct {
	Title              string   `json:"title"`
	Type               string   `json:"type"`
	TargetModule       string   `json:"target_module"`
	ReferenceModule    string   `json:"reference_module"`
	KnownFiles         []string `json:"known_files"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Constraints        []string `json:"constraints"`
}

// planResponse is the top-level JSON envelope returned by Claude.
type planResponse struct {
	WorkOrders []planWorkOrder `json:"work_orders"`
}

// parsePlanResponse extracts and validates work orders from Claude's raw response.
func parsePlanResponse(raw string) ([]models.WorkOrder, error) {
	cleaned := llm.CleanLLMResponse(raw)

	var resp planResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %w", err)
	}

	if len(resp.WorkOrders) == 0 {
		return nil, fmt.Errorf("response contained no work orders")
	}

	workOrders := make([]models.WorkOrder, len(resp.WorkOrders))
	for i, pw := range resp.WorkOrders {
		wo := models.WorkOrder{
			Title:              pw.Title,
			Type:               pw.Type,
			TargetModule:       pw.TargetModule,
			ReferenceModule:    pw.ReferenceModule,
			KnownFiles:         pw.KnownFiles,
			AcceptanceCriteria: pw.AcceptanceCriteria,
			Constraints:        pw.Constraints,
		}
		if err := wo.Validate(); err != nil {
			return nil, fmt.Errorf("work order %d (%q): %w", i+1, pw.Title, err)
		}
		workOrders[i] = wo
	}

	return workOrders, nil
}

// orderedWorkOrder controls YAML field order for output files.
// The field order matches the canonical work order format:
// title, type, target_module, reference_module, known_files, acceptance_criteria, constraints.
type orderedWorkOrder struct {
	Title              string   `yaml:"title"`
	Type               string   `yaml:"type"`
	TargetModule       string   `yaml:"target_module"`
	ReferenceModule    string   `yaml:"reference_module,omitempty"`
	KnownFiles         []string `yaml:"known_files,omitempty"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria,omitempty"`
	Constraints        []string `yaml:"constraints,omitempty"`
}

// writeWorkOrderFiles writes each work order to a numbered YAML file.
func writeWorkOrderFiles(workOrders []models.WorkOrder, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %q: %w", outputDir, err)
	}

	for i, wo := range workOrders {
		ordered := orderedWorkOrder{
			Title:              wo.Title,
			Type:               wo.Type,
			TargetModule:       wo.TargetModule,
			ReferenceModule:    wo.ReferenceModule,
			KnownFiles:         wo.KnownFiles,
			AcceptanceCriteria: wo.AcceptanceCriteria,
			Constraints:        wo.Constraints,
		}

		data, err := yaml.Marshal(ordered)
		if err != nil {
			return fmt.Errorf("failed to marshal work order %d: %w", i+1, err)
		}

		slug := slugify(wo.Title)
		filename := fmt.Sprintf("%03d-%s.yaml", i+1, slug)
		path := filepath.Join(outputDir, filename)

		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		slog.Debug("wrote work order file", "path", path)
	}

	return nil
}

var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a title into a filename-safe slug.
// Lowercase, replace non-alphanumeric runs with hyphens, trim, truncate at word boundary within 50 chars.
func slugify(title string) string {
	s := strings.ToLower(title)
	s = nonAlphanumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	if len(s) <= 50 {
		return s
	}

	// Truncate at word boundary (hyphen) within 50 chars.
	truncated := s[:50]
	if lastHyphen := strings.LastIndex(truncated, "-"); lastHyphen > 0 {
		truncated = truncated[:lastHyphen]
	}
	return strings.TrimRightFunc(truncated, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
