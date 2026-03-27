package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/templates"
)

func TestMergeInvokeClaudeResults(t *testing.T) {
	base := &InvokeClaudeResult{
		Content:   "first",
		TokensIn:  10,
		TokensOut: 5,
		Model:     "model-a",
		CostUSD:   0.10,
		Duration:  2 * time.Second,
		SessionID: "sess-a",
		ToolCalls: map[string]int{"Read": 1},
	}
	retry := &InvokeClaudeResult{
		Content:   "second",
		TokensIn:  20,
		TokensOut: 8,
		Model:     "model-b",
		CostUSD:   0.15,
		Duration:  3 * time.Second,
		SessionID: "sess-b",
		ToolCalls: map[string]int{"Read": 2, "Bash": 1},
	}

	MergeInvokeClaudeResults(base, retry)

	if base.Content != "second" {
		t.Fatalf("Content = %q, want %q", base.Content, "second")
	}
	if base.TokensIn != 30 {
		t.Fatalf("TokensIn = %d, want 30", base.TokensIn)
	}
	if base.TokensOut != 13 {
		t.Fatalf("TokensOut = %d, want 13", base.TokensOut)
	}
	if base.CostUSD != 0.25 {
		t.Fatalf("CostUSD = %f, want 0.25", base.CostUSD)
	}
	if base.Duration != 5*time.Second {
		t.Fatalf("Duration = %s, want 5s", base.Duration)
	}
	if base.Model != "model-b" {
		t.Fatalf("Model = %q, want %q", base.Model, "model-b")
	}
	if base.SessionID != "sess-b" {
		t.Fatalf("SessionID = %q, want %q", base.SessionID, "sess-b")
	}
	if base.ToolCalls["Read"] != 3 {
		t.Fatalf("ToolCalls[Read] = %d, want 3", base.ToolCalls["Read"])
	}
	if base.ToolCalls["Bash"] != 1 {
		t.Fatalf("ToolCalls[Bash] = %d, want 1", base.ToolCalls["Bash"])
	}
}

func TestGeneratePlanDocumentAssemblesHierarchicalManifest(t *testing.T) {
	projectDir := t.TempDir()
	for _, relPath := range []string{
		"cmd/conductor/plan.go",
		"internal/templates/prompts.go",
	} {
		absPath := filepath.Join(projectDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatalf("MkdirAll(%q) error: %v", absPath, err)
		}
		if err := os.WriteFile(absPath, []byte("package stub\n"), 0644); err != nil {
			t.Fatalf("WriteFile(%q) error: %v", absPath, err)
		}
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Index: config.Index{
			Include: []string{"**/*.go", "**/*.md", "**/*.yaml"},
		},
	}
	prompts := &templates.LoadedPrompts{
		PlanEpic: "epic prompt",
		PlanTask: "task prompt",
	}

	var phases []string
	invoker := func(phase, systemPrompt, userMsg string) (*InvokeClaudeResult, error) {
		phases = append(phases, phase)
		switch phase {
		case "planning_epic":
			return &InvokeClaudeResult{
				Content: `{
					"requirements": [
						{"id": "REQ-1", "text": "Configurable planner prompts"},
						{"id": "REQ-2", "text": "Persist plan metadata"}
					],
					"epics": [
						{
							"epic_ref": "planner-prompts",
							"title": "Planner prompt plumbing",
							"description": "Load and route prompt templates through the planner.",
							"covers": ["REQ-1"],
							"depends_on_epics": []
						},
						{
							"epic_ref": "plan-metadata",
							"title": "Plan metadata persistence",
							"description": "Persist plan metadata and related artifacts.",
							"covers": ["REQ-2"],
							"depends_on_epics": ["planner-prompts"]
						}
					]
				}`,
				TokensIn:  100,
				TokensOut: 25,
				Model:     "claude-sonnet",
				CostUSD:   0.25,
				Duration:  2 * time.Second,
			}, nil
		case "planning_task_epic-001":
			if !strings.Contains(userMsg, `"epic_ref": "planner-prompts"`) {
				t.Fatalf("epic-001 user message missing target epic context:\n%s", userMsg)
			}
			return &InvokeClaudeResult{
				Content: `{
					"tasks": [
						{
							"task_ref": "wire-planner-prompts",
							"schema_version": 2,
							"title": "Wire planner prompts",
							"type": "refactor",
							"target_module": "cmd/conductor",
							"reference_module": "internal/templates",
							"known_files": ["cmd/conductor/plan.go", "internal/templates/prompts.go"],
							"requirements": [
								{"id": "REQ-1", "text": "Configurable planner prompts"}
							],
							"acceptance_criteria": [
								{
									"id": "AC-1",
									"description": "Configured planner prompts load successfully",
									"requirement_ids": ["REQ-1"],
									"required": true,
									"verification": {"kind": "diff_review", "focus": ["cmd/conductor/plan.go"]}
								}
							],
							"constraints": ["Keep current YAML output"],
							"depends_on": [],
							"size": "S"
						}
					]
				}`,
				TokensIn:  50,
				TokensOut: 20,
				Model:     "claude-sonnet",
				CostUSD:   0.10,
				Duration:  time.Second,
			}, nil
		case "planning_task_epic-002":
			if !strings.Contains(userMsg, `"task_ref": "wire-planner-prompts"`) {
				t.Fatalf("epic-002 user message missing prior task context:\n%s", userMsg)
			}
			if !strings.Contains(userMsg, `"id": "task-001"`) {
				t.Fatalf("epic-002 user message missing canonical prior task id:\n%s", userMsg)
			}
			return &InvokeClaudeResult{
				Content: `{
					"tasks": [
						{
							"task_ref": "persist-plan-metadata",
							"schema_version": 2,
							"title": "Persist plan metadata artifacts",
							"type": "new_feature",
							"target_module": "cmd/conductor",
							"reference_module": "cmd/conductor/plan.go",
							"known_files": ["cmd/conductor/plan.go"],
							"requirements": [
								{"id": "REQ-2", "text": "Persist plan metadata"}
							],
							"acceptance_criteria": [
								{
									"id": "AC-2",
									"description": "Structured plan artifacts are written",
									"requirement_ids": ["REQ-2"],
									"required": true,
									"verification": {"kind": "diff_review", "focus": ["cmd/conductor/plan.go"]}
								}
							],
							"constraints": ["Keep generated YAML compatible with current pipeline"],
							"depends_on": ["wire-planner-prompts"],
							"size": "M"
						}
					]
				}`,
				TokensIn:  60,
				TokensOut: 30,
				Model:     "claude-sonnet",
				CostUSD:   0.12,
				Duration:  1500 * time.Millisecond,
			}, nil
		default:
			t.Fatalf("unexpected phase %q", phase)
			return nil, nil
		}
	}

	noopProgress := func(state, phase string) {}
	doc, trace, retryCount, err := generatePlanDocument("spec.md", []byte("spec body"), "session-123", cfg, prompts, invoker, noopProgress)
	if err != nil {
		t.Fatalf("generatePlanDocument() error = %v", err)
	}
	if retryCount != 0 {
		t.Fatalf("retryCount = %d, want 0", retryCount)
	}
	if got, want := phases, []string{"planning_epic", "planning_task_epic-001", "planning_task_epic-002"}; len(got) != len(want) || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("phases = %v, want %v", got, want)
	}
	if trace == nil {
		t.Fatal("trace = nil, want planning trace")
	}
	if !strings.Contains(trace.AggregateRaw, "=== EPIC GENERATION ===") || !strings.Contains(trace.AggregateRaw, "=== TASK GENERATION epic-002 ===") {
		t.Fatalf("trace.AggregateRaw missing expected sections:\n%s", trace.AggregateRaw)
	}
	if trace.AggregateResult == nil {
		t.Fatal("trace.AggregateResult = nil, want aggregate result")
	}
	if trace.AggregateResult.TokensIn != 210 || trace.AggregateResult.TokensOut != 75 {
		t.Fatalf("aggregate tokens = (%d,%d), want (210,75)", trace.AggregateResult.TokensIn, trace.AggregateResult.TokensOut)
	}
	if len(trace.TaskTraces) != 2 {
		t.Fatalf("len(trace.TaskTraces) = %d, want 2", len(trace.TaskTraces))
	}
	if doc.Version != 1 || doc.SpecFile != "spec.md" || doc.SessionID != "session-123" {
		t.Fatalf("doc metadata = %#v, want version/spec/session set", doc)
	}
	if len(doc.Epics) != 2 {
		t.Fatalf("len(Epics) = %d, want 2", len(doc.Epics))
	}
	if doc.Epics[0].ID != "epic-001" || doc.Epics[1].ID != "epic-002" {
		t.Fatalf("epic IDs = (%q,%q), want (epic-001, epic-002)", doc.Epics[0].ID, doc.Epics[1].ID)
	}
	if got := doc.Epics[1].DependsOnEpics; len(got) != 1 || got[0] != "epic-001" {
		t.Fatalf("DependsOnEpics = %v, want [epic-001]", got)
	}
	if doc.Epics[0].Tasks[0].ID != "task-001" || doc.Epics[1].Tasks[0].ID != "task-002" {
		t.Fatalf("task IDs = (%q,%q), want (task-001, task-002)", doc.Epics[0].Tasks[0].ID, doc.Epics[1].Tasks[0].ID)
	}
	if got := doc.Epics[1].Tasks[0].DependsOn; len(got) != 1 || got[0] != "task-001" {
		t.Fatalf("task DependsOn = %v, want [task-001]", got)
	}
}

func TestGeneratePlanDocumentFailsBeforeAuditOnInvalidAssembledManifest(t *testing.T) {
	projectDir := t.TempDir()
	absPath := filepath.Join(projectDir, "cmd", "conductor", "plan.go")
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatalf("MkdirAll(%q) error: %v", absPath, err)
	}
	if err := os.WriteFile(absPath, []byte("package stub\n"), 0644); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", absPath, err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Index: config.Index{
			Include: []string{"**/*.go", "**/*.md", "**/*.yaml"},
		},
	}
	prompts := &templates.LoadedPrompts{
		PlanEpic: "epic prompt",
		PlanTask: "task prompt",
	}

	invoker := func(phase, systemPrompt, userMsg string) (*InvokeClaudeResult, error) {
		switch phase {
		case "planning_epic":
			return &InvokeClaudeResult{
				Content: `{
					"requirements": [{"id": "REQ-1", "text": "Configurable planner prompts"}],
					"epics": [
						{
							"epic_ref": "planner-prompts",
							"title": "Planner prompt plumbing",
							"description": "Load and route prompt templates through the planner.",
							"covers": ["REQ-1"],
							"depends_on_epics": []
						}
					]
				}`,
			}, nil
		case "planning_task_epic-001":
			return &InvokeClaudeResult{
				Content: `{
					"tasks": [
						{
							"task_ref": "wire-planner-prompts",
							"schema_version": 2,
							"title": "Wire planner prompts",
							"type": "refactor",
							"target_module": "cmd/conductor",
							"reference_module": "internal/templates",
							"known_files": ["does/not/exist.go"],
							"requirements": [{"id": "REQ-1", "text": "Configurable planner prompts"}],
							"acceptance_criteria": [
								{
									"id": "AC-1",
									"description": "Configured planner prompts load successfully",
									"requirement_ids": ["REQ-1"],
									"required": true,
									"verification": {"kind": "diff_review", "focus": ["cmd/conductor/plan.go"]}
								}
							],
							"constraints": ["Keep current YAML output"],
							"depends_on": [],
							"size": "S"
						}
					]
				}`,
			}, nil
		default:
			t.Fatalf("unexpected phase %q", phase)
			return nil, nil
		}
	}

	noopProgress := func(state, phase string) {}
	_, _, _, err := generatePlanDocument("spec.md", []byte("spec body"), "session-123", cfg, prompts, invoker, noopProgress)
	if err == nil || !strings.Contains(err.Error(), "planner output validation failed") {
		t.Fatalf("generatePlanDocument() error = %v, want pre-audit validation failure", err)
	}
}

func TestBuildAuditUserMessageUsesGeneratedManifest(t *testing.T) {
	projectDir := t.TempDir()
	for _, relPath := range []string{
		"cmd/conductor/plan.go",
		"internal/templates/prompts.go",
	} {
		absPath := filepath.Join(projectDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatalf("MkdirAll(%q) error: %v", absPath, err)
		}
		if err := os.WriteFile(absPath, []byte("package stub\n"), 0644); err != nil {
			t.Fatalf("WriteFile(%q) error: %v", absPath, err)
		}
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Index: config.Index{
			Include: []string{"**/*.go", "**/*.md", "**/*.yaml"},
		},
	}
	planDoc := &PlanDocument{
		Version:  1,
		SpecFile: "spec.md",
		Epics: []PlanEpic{
			{
				ID:          "epic-001",
				EpicRef:     "planner-observability",
				Title:       "Planner observability",
				Description: "Persist artifacts and metrics for hierarchical planning",
				Tasks: []PlanTask{
					{
						ID:            "task-001",
						TaskRef:       "persist-artifacts",
						SchemaVersion: 2,
						Title:         "Persist hierarchical artifacts",
						Type:          "new_feature",
						TargetModule:  "cmd/conductor",
						AcceptanceCriteria: []models.TypedAcceptanceCriterion{
							{
								ID:             "AC-1",
								Description:    "Audit uses the manifest",
								RequirementIDs: []string{"REQ-1"},
								Required:       plannerTestBoolPtr(true),
								Verification: models.AcceptanceVerification{
									Kind:  "diff_review",
									Focus: []string{"cmd/conductor/plan.go"},
								},
							},
						},
						Requirements: []models.WorkOrderRequirement{
							{ID: "REQ-1", Text: "Keep audit manifest-based"},
						},
						Size: "S",
					},
				},
			},
		},
	}

	userMsg, err := buildAuditUserMessage("spec body", planDoc, cfg)
	if err != nil {
		t.Fatalf("buildAuditUserMessage() error = %v", err)
	}

	if !strings.Contains(userMsg, "=== GENERATED PLAN ===") {
		t.Fatalf("audit user message missing generated plan section:\n%s", userMsg)
	}
	if !strings.Contains(userMsg, `"id": "epic-001"`) {
		t.Fatalf("audit user message missing canonical epic id:\n%s", userMsg)
	}
	if !strings.Contains(userMsg, `"id": "task-001"`) {
		t.Fatalf("audit user message missing canonical task id:\n%s", userMsg)
	}
	if !strings.Contains(userMsg, "spec body") {
		t.Fatalf("audit user message missing spec body:\n%s", userMsg)
	}
}

func plannerTestBoolPtr(v bool) *bool {
	return &v
}
