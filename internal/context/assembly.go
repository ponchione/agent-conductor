package context

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/models"
)

// RAGSearcher performs semantic search and returns a pre-formatted context
// block ready for injection. Returning an empty string means no results.
// Defined here so the context package stays free of CGo dependencies.
type RAGSearcher interface {
	SearchFormatted(ctx context.Context, query string, topK int) (string, error)
}

// Assembler gathers and formats context for the LLM.
type Assembler struct {
	cfg      *config.ProjectConfig
	git      *git.GitManager
	searcher RAGSearcher // nil if RAG not configured
}

// NewAssembler creates a new context assembler. searcher may be nil.
func NewAssembler(cfg *config.ProjectConfig, gitMgr *git.GitManager, searcher RAGSearcher) *Assembler {
	return &Assembler{
		cfg:      cfg,
		git:      gitMgr,
		searcher: searcher,
	}
}

// AssemblyHints controls per-type context gathering behaviour.
type AssemblyHints struct {
	ExtraDirScans []string // repo-relative dirs to list as additional context
	ExpandHistory int      // if > 0, override default 5-commit git history
	BroadGrep     bool     // if true, git history without module path filter
}

// HintsForType returns AssemblyHints appropriate for the given work order type.
func HintsForType(woType string, conv config.Conventions) AssemblyHints {
	switch woType {
	case "schema_change":
		var dirs []string
		if conv.SQLPath != "" {
			dirs = []string{conv.SQLPath}
		}
		return AssemblyHints{ExtraDirScans: dirs}
	case "bug_fix":
		return AssemblyHints{ExpandHistory: 10, BroadGrep: true}
	case "refactor":
		var dirs []string
		if conv.SharedPath != "" {
			dirs = []string{conv.SharedPath}
		}
		return AssemblyHints{ExtraDirScans: dirs}
	case "docs":
		var dirs []string
		if conv.DocsPath != "" {
			dirs = []string{conv.DocsPath}
		}
		return AssemblyHints{ExtraDirScans: dirs}
	default: // new_feature and unknown
		return AssemblyHints{}
	}
}

// Assemble gathers context and returns a formatted string.
func (a *Assembler) Assemble(ctx context.Context, wo *models.WorkOrder) (string, error) {
	var sb bytes.Buffer

	hints := HintsForType(wo.Type, a.cfg.Conventions)

	// 1. Work Order Header
	sb.WriteString("=== WORK ORDER ===\n")
	sb.WriteString(fmt.Sprintf("Title: %s\n", wo.Title))
	sb.WriteString(fmt.Sprintf("Target module: %s\n", wo.TargetModule))
	if wo.ReferenceModule != "" {
		sb.WriteString(fmt.Sprintf("Reference module: %s\n", wo.ReferenceModule))
	}
	sb.WriteString(fmt.Sprintf("Type: %s\n", wo.Type))
	sb.WriteString("\n")

	sb.WriteString("=== REPOSITORY CONTEXT ===\n\n")

	// 2. Directory Tree (Target Module)
	targetPath := filepath.Join(a.cfg.Project.Path, a.cfg.Conventions.ModulePath, wo.TargetModule)
	sb.WriteString(fmt.Sprintf("Directory tree (%s):\n", filepath.Join(a.cfg.Conventions.ModulePath, wo.TargetModule)))

	files, err := a.listFiles(targetPath, a.cfg.Project.Path)
	if err == nil {
		for _, f := range files {
			sb.WriteString(fmt.Sprintf("  %s (%d lines)\n", f.Name, f.Lines))
		}
	} else {
		sb.WriteString(fmt.Sprintf("  (Module not found or empty: %s)\n", err))
	}
	sb.WriteString("\n")

	// 3. Reference Module Files
	if wo.ReferenceModule != "" {
		refPath := filepath.Join(a.cfg.Project.Path, a.cfg.Conventions.ModulePath, wo.ReferenceModule)
		sb.WriteString(fmt.Sprintf("Reference module files (%s):\n", wo.ReferenceModule))

		files, err := a.listFiles(refPath, a.cfg.Project.Path)
		if err == nil {
			for _, f := range files {
				sb.WriteString(fmt.Sprintf("  %s (%d lines)\n", f.Name, f.Lines))
			}
		} else {
			sb.WriteString(fmt.Sprintf("  (Reference module not found: %s)\n", err))
		}
		sb.WriteString("\n")
	}

	// 4. SQL Schema
	sqlPath := filepath.Join(a.cfg.Project.Path, a.cfg.Conventions.SQLPath)
	sb.WriteString("SQL schema files:\n")
	sqlFiles, err := a.listFiles(sqlPath, a.cfg.Project.Path)
	if err == nil {
		for _, f := range sqlFiles {
			if strings.HasSuffix(f.Name, ".sql") {
				sb.WriteString(fmt.Sprintf("  %s\n", f.Name))
			}
		}
	} else {
		sb.WriteString("  (No schema files found)\n")
	}
	sb.WriteString("\n")

	// 5. Git History
	commitCount := 5
	if hints.ExpandHistory > 0 {
		commitCount = hints.ExpandHistory
	}
	relFilter := filepath.Join(a.cfg.Conventions.ModulePath, wo.TargetModule)
	if hints.BroadGrep {
		relFilter = ""
	}
	sb.WriteString(fmt.Sprintf("Recent changes to related files (last %d commits):\n", commitCount))
	commits, err := a.git.GetRecentCommits(a.cfg.Project.Path, commitCount, relFilter)
	if err == nil && len(commits) > 0 {
		for _, c := range commits {
			sb.WriteString(fmt.Sprintf("  %s - %s (%s)\n", c.Hash, c.Message, c.Author))
		}
	} else {
		sb.WriteString("  (No recent commits found)\n")
	}
	sb.WriteString("\n")

	// 6. Known Files (Explicitly listed)
	if len(wo.KnownFiles) > 0 {
		sb.WriteString("Explicitly listed known files:\n")
		for _, kf := range wo.KnownFiles {
			sb.WriteString(fmt.Sprintf("  %s\n", kf))
		}
		sb.WriteString("\n")
	}

	// 7. Extra directory scans (per-type hints)
	for _, dir := range hints.ExtraDirScans {
		absDir := filepath.Join(a.cfg.Project.Path, dir)
		sb.WriteString(fmt.Sprintf("Additional context (%s):\n", dir))
		files, err := a.listFiles(absDir, a.cfg.Project.Path)
		if err == nil {
			for _, f := range files {
				sb.WriteString(fmt.Sprintf("  %s (%d lines)\n", f.Name, f.Lines))
			}
		} else {
			sb.WriteString(fmt.Sprintf("  (Directory not found: %s)\n", err))
		}
		sb.WriteString("\n")
	}

	// 8. RAG results
	if a.searcher != nil {
		block, err := a.searcher.SearchFormatted(ctx, wo.Title, 10)
		if err != nil {
			slog.Warn("RAG search failed, continuing without", "error", err)
		} else if block != "" {
			sb.WriteString(block)
		}
	}

	return sb.String(), nil
}

type fileInfo struct {
	Name  string
	Lines int
}

func (a *Assembler) listFiles(dirPath, root string) ([]fileInfo, error) {
	var files []fileInfo
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}
		// Skip large files?
		if info.Size() > 1024*1024 { // 1MB limit for context listing?
			continue
		}

		lines, err := countLines(filepath.Join(dirPath, e.Name()))
		if err != nil {
			lines = 0
		}
		relName, err := filepath.Rel(root, filepath.Join(dirPath, e.Name()))
		if err != nil {
			relName = e.Name() // fallback to basename on error
		}
		files = append(files, fileInfo{Name: relName, Lines: lines})
	}
	return files, nil
}

func countLines(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return bytes.Count(content, []byte{'\n'}) + 1, nil
}
