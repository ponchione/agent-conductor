package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
)

func TestPlanContextBuilderBuildIncludesStructuredSections(t *testing.T) {
	projectDir := t.TempDir()
	writePlanContextFile(t, projectDir, "work-order.template.yaml", "title: Example\n")
	writePlanContextFile(t, projectDir, "README.md", "# Repo\n")
	writePlanContextFile(t, projectDir, "CLAUDE.md", "Follow repo rules.\n")
	writePlanContextFile(t, projectDir, "docs/architecture.md", "Architecture notes.\n")
	writePlanContextFile(t, projectDir, "internal/app/main.go", "package app\n")

	cfg := &config.ProjectConfig{
		Project: config.Project{
			Name:      "agent-conductor",
			Path:      projectDir,
			Language:  "go",
			Framework: "cobra",
		},
		Index: config.Index{
			Include: []string{"**/*.go", "**/*.md", "**/*.yaml"},
		},
		Conventions: config.Conventions{
			ModulePath:      "internal",
			ModuleStructure: []string{"cmd/conductor", "internal"},
			DocsPath:        "docs",
		},
	}

	got := newPlanContextBuilder(cfg).Build("Add planner context builder")

	assertContains(t, got, "=== SPECIFICATION ===\nAdd planner context builder\n")
	assertContains(t, got, "=== PROJECT FACTS ===\nProject name: agent-conductor\nLanguage: go\nFramework: cobra\n")
	assertContains(t, got, "=== PROJECT CONVENTIONS ===\nModule path: internal\nModule structure: cmd/conductor, internal\nDocs path: docs\n")
	assertContains(t, got, "=== CANONICAL WORK ORDER TEMPLATE ===\ntitle: Example\n")
	assertContains(t, got, "=== CURATED EXISTING SYSTEM CONTEXT ===\n--- README.md ---\n# Repo\n")
	assertContains(t, got, "--- CLAUDE.md ---\nFollow repo rules.\n")
	assertContains(t, got, "--- docs/architecture.md ---\nArchitecture notes.\n")
	assertContains(t, got, "=== PROJECT FILE TREE ===\n")
	assertContains(t, got, "work-order.template.yaml\n")
	assertContains(t, got, "README.md\n")
	assertContains(t, got, "CLAUDE.md\n")
	assertContains(t, got, "docs/\n  architecture.md\n")
	assertContains(t, got, "internal/\n  app/\n    main.go\n")
}

func TestPlanContextBuilderBuildIsDeterministic(t *testing.T) {
	projectDir := t.TempDir()
	writePlanContextFile(t, projectDir, "work-order.template.yaml", "title: Example\n")
	writePlanContextFile(t, projectDir, "README.md", "alpha\n")
	writePlanContextFile(t, projectDir, "docs/a.md", "A\n")
	writePlanContextFile(t, projectDir, "docs/b.md", "B\n")
	writePlanContextFile(t, projectDir, "internal/app/main.go", "package app\n")

	cfg := &config.ProjectConfig{
		Project: config.Project{
			Name: "agent-conductor",
			Path: projectDir,
		},
		Index: config.Index{
			Include: []string{"**/*.go", "**/*.md", "**/*.yaml"},
		},
		Conventions: config.Conventions{
			DocsPath: "docs",
		},
	}

	builder := newPlanContextBuilder(cfg)
	first := builder.Build("spec")
	second := builder.Build("spec")

	if first != second {
		t.Fatalf("Build output was not deterministic\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestPlanContextBuilderBoundsCuratedContext(t *testing.T) {
	projectDir := t.TempDir()
	writePlanContextFile(t, projectDir, "work-order.template.yaml", "title: Example\n")
	writePlanContextFile(t, projectDir, "README.md", strings.Repeat("R", planContextExcerptBytesPerFile+128))
	writePlanContextFile(t, projectDir, "CLAUDE.md", strings.Repeat("C", planContextExcerptBytesPerFile+128))
	writePlanContextFile(t, projectDir, "docs/architecture.md", strings.Repeat("D", planContextExcerptBytesPerFile+128))

	cfg := &config.ProjectConfig{
		Project: config.Project{
			Path: projectDir,
		},
		Index: config.Index{
			Include: []string{"**/*.md", "**/*.yaml"},
		},
		Conventions: config.Conventions{
			DocsPath: "docs",
		},
	}

	got := newPlanContextBuilder(cfg).Build("spec")

	if count := strings.Count(got, "[truncated]"); count == 0 {
		t.Fatalf("expected curated context truncation marker in output:\n%s", got)
	}
	if strings.Contains(got, strings.Repeat("D", planContextExcerptBytesPerFile+128)) {
		t.Fatalf("expected docs excerpt to be bounded in output")
	}
}

func TestPlanContextBuilderBuildTaskDecompositionIncludesTargetEpicAndPriorTasks(t *testing.T) {
	projectDir := t.TempDir()
	writePlanContextFile(t, projectDir, "work-order.template.yaml", "title: Example\n")
	writePlanContextFile(t, projectDir, "README.md", "# Repo\n")
	writePlanContextFile(t, projectDir, "internal/app/main.go", "package app\n")

	cfg := &config.ProjectConfig{
		Project: config.Project{
			Name: "agent-conductor",
			Path: projectDir,
		},
		Index: config.Index{
			Include: []string{"**/*.go", "**/*.md", "**/*.yaml"},
		},
	}

	targetEpic := planEpic{
		ID:          "epic-002",
		EpicRef:     "task-decomposition",
		Title:       "Task decomposition",
		Description: "Break the epic into executable tasks.",
		Covers:      []string{"REQ-2"},
	}
	priorEpics := []planEpic{
		{
			ID:          "epic-001",
			EpicRef:     "epic-plumbing",
			Title:       "Epic prompt plumbing",
			Description: "Load plan prompts and epic context.",
			Tasks: []planTask{
				{
					ID:           "task-001",
					TaskRef:      "load-plan-epic-prompt",
					Title:        "Load plan epic prompt",
					EpicID:       "epic-001",
					TargetModule: "internal/templates",
				},
			},
		},
	}

	got := newPlanContextBuilder(cfg).BuildTaskDecomposition("spec", targetEpic, priorEpics)

	assertContains(t, got, "=== TARGET EPIC ===\n")
	assertContains(t, got, `"epic_ref": "task-decomposition"`)
	assertContains(t, got, `"title": "Task decomposition"`)
	assertContains(t, got, "=== PRIOR COMPLETED TASKS ===\n")
	assertContains(t, got, `"task_ref": "load-plan-epic-prompt"`)
	assertContains(t, got, `"id": "task-001"`)
	assertContains(t, got, `"epic_ref": "epic-plumbing"`)
	assertContains(t, got, `"epic_title": "Epic prompt plumbing"`)
	assertContains(t, got, `"target_module": "internal/templates"`)
}

func writePlanContextFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	absPath := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", absPath, err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", absPath, err)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q\nfull output:\n%s", want, got)
	}
}
