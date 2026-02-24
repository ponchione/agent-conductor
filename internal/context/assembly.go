package context

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/models"
)

// Assembler gathers and formats context for the LLM.
type Assembler struct {
	cfg *config.ProjectConfig
	git *git.GitManager
}

// NewAssembler creates a new context assembler.
func NewAssembler(cfg *config.ProjectConfig, gitMgr *git.GitManager) *Assembler {
	return &Assembler{
		cfg: cfg,
		git: gitMgr,
	}
}

// Assemble gathers context and returns a formatted string.
func (a *Assembler) Assemble(ctx context.Context, wo *models.WorkOrder) (string, error) {
	var sb bytes.Buffer

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
	sb.WriteString("Recent changes to related files (last 5 commits):\n")
	relFilter := filepath.Join(a.cfg.Conventions.ModulePath, wo.TargetModule)

	commits, err := a.git.GetRecentCommits(a.cfg.Project.Path, 5, relFilter)
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
