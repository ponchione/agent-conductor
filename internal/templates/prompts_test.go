package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
)

func TestLoadPromptsFallsBackToEmbeddedHierarchicalPlannerPrompts(t *testing.T) {
	cfg := &config.ProjectConfig{}
	cfg.Project.Path = t.TempDir()

	prompts, err := LoadPrompts(cfg)
	if err != nil {
		t.Fatalf("LoadPrompts() error = %v", err)
	}

	if prompts.PlanEpic != DefaultPlanEpicPrompt {
		t.Fatalf("PlanEpic prompt did not use embedded default fallback")
	}
	if prompts.PlanTask != DefaultPlanTaskPrompt {
		t.Fatalf("PlanTask prompt did not use embedded default fallback")
	}
	if prompts.PlanAudit != DefaultPlanAuditPrompt {
		t.Fatalf("PlanAudit prompt did not use embedded default fallback")
	}
}

func TestLoadPromptsLoadsConfiguredHierarchicalPlannerPromptPaths(t *testing.T) {
	projectDir := t.TempDir()
	planEpicPath := filepath.Join(projectDir, "templates", "plan-epic.md")
	planTaskPath := filepath.Join(projectDir, "templates", "plan-task.md")
	planAuditPath := filepath.Join(projectDir, "templates", "plan-audit.md")
	if err := os.MkdirAll(filepath.Dir(planEpicPath), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	wantPlanEpic := "custom plan epic prompt"
	wantPlanTask := "custom plan task prompt"
	wantPlanAudit := "custom plan audit prompt"
	if err := os.WriteFile(planEpicPath, []byte(wantPlanEpic), 0644); err != nil {
		t.Fatalf("WriteFile(plan epic) error = %v", err)
	}
	if err := os.WriteFile(planTaskPath, []byte(wantPlanTask), 0644); err != nil {
		t.Fatalf("WriteFile(plan task) error = %v", err)
	}
	if err := os.WriteFile(planAuditPath, []byte(wantPlanAudit), 0644); err != nil {
		t.Fatalf("WriteFile(plan audit) error = %v", err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Prompts: config.Prompts{
			PlanEpic:  "templates/plan-epic.md",
			PlanTask:  "templates/plan-task.md",
			PlanAudit: "templates/plan-audit.md",
		},
	}

	prompts, err := LoadPrompts(cfg)
	if err != nil {
		t.Fatalf("LoadPrompts() error = %v", err)
	}

	if prompts.PlanEpic != wantPlanEpic {
		t.Fatalf("PlanEpic prompt = %q, want %q", prompts.PlanEpic, wantPlanEpic)
	}
	if prompts.PlanTask != wantPlanTask {
		t.Fatalf("PlanTask prompt = %q, want %q", prompts.PlanTask, wantPlanTask)
	}
	if prompts.PlanAudit != wantPlanAudit {
		t.Fatalf("PlanAudit prompt = %q, want %q", prompts.PlanAudit, wantPlanAudit)
	}
}

func TestLoadPromptsPrefersDotPromptsHierarchicalPlannerOverrides(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".prompts"), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	wantPlanEpic := "override plan epic prompt"
	wantPlanTask := "override plan task prompt"
	wantPlanAudit := "override plan audit prompt"
	if err := os.WriteFile(filepath.Join(projectDir, ".prompts", "plan_epic-prompt.md"), []byte(wantPlanEpic), 0644); err != nil {
		t.Fatalf("WriteFile(plan_epic override) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".prompts", "plan_task-prompt.md"), []byte(wantPlanTask), 0644); err != nil {
		t.Fatalf("WriteFile(plan_task override) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".prompts", "plan_audit-prompt.md"), []byte(wantPlanAudit), 0644); err != nil {
		t.Fatalf("WriteFile(plan_audit override) error = %v", err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Prompts: config.Prompts{
			PlanEpic:  "templates/ignored-plan-epic.md",
			PlanTask:  "templates/ignored-plan-task.md",
			PlanAudit: "templates/ignored-plan-audit.md",
		},
	}

	prompts, err := LoadPrompts(cfg)
	if err != nil {
		t.Fatalf("LoadPrompts() error = %v", err)
	}

	if prompts.PlanEpic != wantPlanEpic {
		t.Fatalf("PlanEpic prompt = %q, want %q", prompts.PlanEpic, wantPlanEpic)
	}
	if prompts.PlanTask != wantPlanTask {
		t.Fatalf("PlanTask prompt = %q, want %q", prompts.PlanTask, wantPlanTask)
	}
	if prompts.PlanAudit != wantPlanAudit {
		t.Fatalf("PlanAudit prompt = %q, want %q", prompts.PlanAudit, wantPlanAudit)
	}
}

func TestLoadPromptsReturnsHelpfulErrorForMissingConfiguredPlanEpicPrompt(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: t.TempDir()},
		Prompts: config.Prompts{
			PlanEpic: "templates/missing-plan-epic.md",
		},
	}

	_, err := LoadPrompts(cfg)
	if err == nil {
		t.Fatal("LoadPrompts() error = nil, want missing prompt error")
	}
	if !strings.Contains(err.Error(), "plan_epic prompt: prompt file not found") {
		t.Fatalf("LoadPrompts() error = %q, want plan_epic prompt file not found", err)
	}
}

func TestLoadPromptsForPlanIgnoresUnrelatedPromptConfig(t *testing.T) {
	projectDir := t.TempDir()
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Prompts: config.Prompts{
			Build: "templates/missing-build.md",
		},
	}

	prompts, err := LoadPromptsForPlan(cfg)
	if err != nil {
		t.Fatalf("LoadPromptsForPlan() error = %v", err)
	}
	if prompts.PlanEpic != DefaultPlanEpicPrompt {
		t.Fatalf("PlanEpic prompt did not use embedded default fallback")
	}
	if prompts.PlanTask != DefaultPlanTaskPrompt {
		t.Fatalf("PlanTask prompt did not use embedded default fallback")
	}
	if prompts.PlanAudit != DefaultPlanAuditPrompt {
		t.Fatalf("PlanAudit prompt did not use embedded default fallback")
	}
}

func TestDefaultPlanEpicPromptMatchesCanonicalPlanContract(t *testing.T) {
	requiredSnippets := []string{
		"\"epics\": [",
		"\"epic_ref\": \"server-foundation-api\"",
		"\"depends_on_epics\": [\"another-epic-ref\"]",
		"Do not invent speculative work streams",
		"Do not rewrite or soften settled specification decisions.",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(DefaultPlanEpicPrompt, snippet) {
			t.Fatalf("DefaultPlanEpicPrompt missing required snippet %q", snippet)
		}
	}
}

func TestDefaultPlanTaskPromptMatchesCanonicalTaskContract(t *testing.T) {
	requiredSnippets := []string{
		"\"tasks\": [",
		"\"task_ref\": \"http-server-scaffold\"",
		"\"schema_version\": 2",
		"\"acceptance_criteria\": [",
		"`acceptance_criteria` MUST be typed objects, not legacy string arrays.",
		"`depends_on` entries must reference prior `task_ref` values only.",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(DefaultPlanTaskPrompt, snippet) {
			t.Fatalf("DefaultPlanTaskPrompt missing required snippet %q", snippet)
		}
	}
}

func TestDefaultPlanAuditPromptMatchesCanonicalAuditContract(t *testing.T) {
	requiredSnippets := []string{
		"Requirement IDs must stay stable.",
		"`acceptance_criteria` MUST remain typed objects. Do not emit legacy string",
		"You may add tasks or modify task fields",
		"You may not add, delete, rename, reorder, merge, or split epics.",
		"\"audit_action\": \"added | modified | unchanged\"",
		"Do not invent speculative work streams",
		"Do not rewrite or soften settled specification decisions.",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(DefaultPlanAuditPrompt, snippet) {
			t.Fatalf("DefaultPlanAuditPrompt missing required snippet %q", snippet)
		}
	}
}

func TestDefaultHierarchicalPlannerPromptsDoNotReferenceFlatPlannerContract(t *testing.T) {
	disallowedSnippets := []string{
		`"work_orders": [`,
		"single-pass flat planner",
		"numbered task yaml",
	}

	for _, prompt := range []struct {
		name string
		body string
	}{
		{name: "PlanEpic", body: DefaultPlanEpicPrompt},
		{name: "PlanTask", body: DefaultPlanTaskPrompt},
		{name: "PlanAudit", body: DefaultPlanAuditPrompt},
	} {
		for _, snippet := range disallowedSnippets {
			if strings.Contains(prompt.body, snippet) {
				t.Fatalf("%s prompt unexpectedly contains stale flat-planning snippet %q", prompt.name, snippet)
			}
		}
	}
}
