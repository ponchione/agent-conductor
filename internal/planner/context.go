package planner

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/util"
)

const (
	planContextExcerptBytesPerFile = 2048
	planContextExcerptBytesTotal   = 6144
	planContextMaxDocFiles         = 2
)

// PlanContextBuilder assembles project context from config and filesystem
// for use in plan generation LLM prompts.
type PlanContextBuilder struct {
	cfg *config.ProjectConfig
}

// NewPlanContextBuilder creates a new PlanContextBuilder.
func NewPlanContextBuilder(cfg *config.ProjectConfig) *PlanContextBuilder {
	return &PlanContextBuilder{cfg: cfg}
}

// Build returns the full context string for epic decomposition.
func (b *PlanContextBuilder) Build(spec string) string {
	return b.BuildEpicDecomposition(spec)
}

// BuildEpicDecomposition assembles the context for the epic planning phase.
func (b *PlanContextBuilder) BuildEpicDecomposition(spec string) string {
	var sb strings.Builder

	sb.WriteString("=== SPECIFICATION ===\n")
	sb.WriteString(spec)
	sb.WriteString("\n")

	sb.WriteString("\n=== PROJECT CONTEXT ===\n")
	if projectFacts := b.buildProjectFacts(); projectFacts != "" {
		sb.WriteString("\n=== PROJECT FACTS ===\n")
		sb.WriteString(projectFacts)
	}
	if conventions := b.buildConventions(); conventions != "" {
		sb.WriteString("\n=== PROJECT CONVENTIONS ===\n")
		sb.WriteString(conventions)
	}
	if template := b.buildCanonicalTemplate(); template != "" {
		sb.WriteString("\n=== CANONICAL WORK ORDER TEMPLATE ===\n")
		sb.WriteString(template)
		if !strings.HasSuffix(template, "\n") {
			sb.WriteString("\n")
		}
	}
	if existing := b.buildExistingSystemContext(); existing != "" {
		sb.WriteString("\n=== CURATED EXISTING SYSTEM CONTEXT ===\n")
		sb.WriteString(existing)
	}

	treeStr, totalFiles := b.buildFileTree()
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

// BuildTaskDecomposition assembles the context for the task planning phase.
func (b *PlanContextBuilder) BuildTaskDecomposition(spec string, epic PlanEpic, priorEpics []PlanEpic) string {
	var sb strings.Builder

	sb.WriteString(b.BuildEpicDecomposition(spec))

	sb.WriteString("\n=== TARGET EPIC ===\n")
	sb.WriteString(mustMarshalPromptJSON(struct {
		ID             string   `json:"id"`
		EpicRef        string   `json:"epic_ref"`
		Title          string   `json:"title"`
		Description    string   `json:"description"`
		Covers         []string `json:"covers,omitempty"`
		DependsOnEpics []string `json:"depends_on_epics,omitempty"`
	}{
		ID:             epic.ID,
		EpicRef:        epic.EpicRef,
		Title:          epic.Title,
		Description:    epic.Description,
		Covers:         epic.Covers,
		DependsOnEpics: epic.DependsOnEpics,
	}))
	sb.WriteString("\n")

	sb.WriteString("\n=== PRIOR COMPLETED TASKS ===\n")
	priorTasks := FlattenPlanTasks(priorEpics)
	if len(priorTasks) == 0 {
		sb.WriteString("(none)\n")
		return sb.String()
	}

	type priorTaskContext struct {
		ID           string `json:"id,omitempty"`
		TaskRef      string `json:"task_ref"`
		Title        string `json:"title"`
		EpicID       string `json:"epic_id,omitempty"`
		EpicRef      string `json:"epic_ref,omitempty"`
		EpicTitle    string `json:"epic_title,omitempty"`
		TargetModule string `json:"target_module"`
	}

	epicsByID := make(map[string]PlanEpic, len(priorEpics)+1)
	epicsByID[epic.ID] = epic
	for _, priorEpic := range priorEpics {
		epicsByID[priorEpic.ID] = priorEpic
	}

	contextTasks := make([]priorTaskContext, 0, len(priorTasks))
	for _, task := range priorTasks {
		item := priorTaskContext{
			ID:           task.ID,
			TaskRef:      task.TaskRef,
			Title:        task.Title,
			EpicID:       task.EpicID,
			TargetModule: task.TargetModule,
		}
		if parent, ok := epicsByID[task.EpicID]; ok {
			item.EpicRef = parent.EpicRef
			item.EpicTitle = parent.Title
		}
		contextTasks = append(contextTasks, item)
	}

	sb.WriteString(mustMarshalPromptJSON(contextTasks))
	sb.WriteString("\n")
	return sb.String()
}

// BuildAuditContext assembles the context for the audit phase.
func (b *PlanContextBuilder) BuildAuditContext(spec string, planJSON []byte) string {
	var sb strings.Builder
	sb.WriteString("=== SPECIFICATION ===\n")
	sb.WriteString(spec)
	sb.WriteString("\n")

	sb.WriteString("\n=== PROJECT CONTEXT ===\n")
	if projectFacts := b.buildProjectFacts(); projectFacts != "" {
		sb.WriteString("\n=== PROJECT FACTS ===\n")
		sb.WriteString(projectFacts)
	}
	if convSection := b.buildConventions(); convSection != "" {
		sb.WriteString("\n=== PROJECT CONVENTIONS ===\n")
		sb.WriteString(convSection)
	}
	treeStr, totalFiles := b.buildFileTree()
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

	sb.WriteString("\n=== GENERATED PLAN ===\n")
	sb.Write(planJSON)
	sb.WriteString("\n")

	return sb.String()
}

func mustMarshalPromptJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("marshal prompt context: %v", err))
	}
	return string(data)
}

func (b *PlanContextBuilder) buildProjectFacts() string {
	project := b.cfg.Project
	var sb strings.Builder

	if project.Name != "" {
		fmt.Fprintf(&sb, "Project name: %s\n", project.Name)
	}
	if project.Language != "" {
		fmt.Fprintf(&sb, "Language: %s\n", project.Language)
	}
	if project.Framework != "" {
		fmt.Fprintf(&sb, "Framework: %s\n", project.Framework)
	}
	if project.Path != "" {
		fmt.Fprintf(&sb, "Project path: %s\n", project.Path)
	}

	return sb.String()
}

func (b *PlanContextBuilder) buildConventions() string {
	conv := b.cfg.Conventions
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

func (b *PlanContextBuilder) buildCanonicalTemplate() string {
	templatePath := filepath.Join(b.cfg.Project.Path, "work-order.template.yaml")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read work order template for planning context", "path", templatePath, "error", err)
		}
		return ""
	}
	return string(data)
}

func (b *PlanContextBuilder) buildExistingSystemContext() string {
	paths := b.curatedContextPaths()
	if len(paths) == 0 {
		return ""
	}

	var sb strings.Builder
	totalBytes := 0

	for _, relPath := range paths {
		if totalBytes >= planContextExcerptBytesTotal {
			break
		}

		absPath := filepath.Join(b.cfg.Project.Path, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			if !os.IsNotExist(err) {
				slog.Warn("failed to read planning context file", "path", absPath, "error", err)
			}
			continue
		}

		remaining := planContextExcerptBytesTotal - totalBytes
		limit := planContextExcerptBytesPerFile
		if remaining < limit {
			limit = remaining
		}

		excerpt, truncated := TruncateForPrompt(data, limit)
		if excerpt == "" {
			continue
		}

		fmt.Fprintf(&sb, "--- %s ---\n", relPath)
		sb.WriteString(excerpt)
		if !strings.HasSuffix(excerpt, "\n") {
			sb.WriteString("\n")
		}
		if truncated {
			sb.WriteString("[truncated]\n")
		}
		sb.WriteString("\n")
		totalBytes += len(excerpt)
	}

	return strings.TrimRight(sb.String(), "\n")
}

func (b *PlanContextBuilder) curatedContextPaths() []string {
	seen := make(map[string]bool)
	var paths []string

	add := func(relPath string) {
		if relPath == "" || seen[relPath] {
			return
		}
		absPath := filepath.Join(b.cfg.Project.Path, relPath)
		info, err := os.Stat(absPath)
		if err != nil || info.IsDir() {
			return
		}
		seen[relPath] = true
		paths = append(paths, relPath)
	}

	add("README.md")
	add("CLAUDE.md")
	for _, docPath := range b.curatedDocsPaths() {
		add(docPath)
	}

	return paths
}

func (b *PlanContextBuilder) curatedDocsPaths() []string {
	docsPath := strings.TrimSpace(b.cfg.Conventions.DocsPath)
	if docsPath == "" {
		return nil
	}

	absPath := filepath.Join(b.cfg.Project.Path, docsPath)
	info, err := os.Stat(absPath)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		return []string{filepath.ToSlash(docsPath)}
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		slog.Warn("failed to read docs path for planning context", "path", absPath, "error", err)
		return nil
	}

	var docs []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".md" && ext != ".txt" {
			continue
		}
		docs = append(docs, filepath.ToSlash(filepath.Join(docsPath, name)))
	}

	sort.Strings(docs)
	if len(docs) > planContextMaxDocFiles {
		docs = docs[:planContextMaxDocFiles]
	}
	return docs
}

func (b *PlanContextBuilder) buildFileTree() (string, int) {
	root := b.cfg.Project.Path
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

		if !util.MatchesAnyGlob(b.cfg.Index.Include, relPath) {
			return nil
		}
		if util.MatchesAnyGlob(b.cfg.Index.Exclude, relPath) {
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

	cap := b.cfg.Index.MaxTreeLines
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

// TruncateForPrompt truncates data to a limit, returning whether truncation occurred.
func TruncateForPrompt(data []byte, limit int) (string, bool) {
	if limit <= 0 || len(data) == 0 {
		return "", false
	}
	if len(data) <= limit {
		return string(data), false
	}

	trimmed := strings.TrimRight(string(data[:limit]), "\n")
	return trimmed, true
}
