package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/models"
	"gopkg.in/yaml.v3"
)

func planTestBool(v bool) *bool {
	return &v
}

func TestParseEpicPlanResponseAcceptsCanonicalEpicOutput(t *testing.T) {
	raw := `{
		"requirements": [
			{"id": "REQ-1", "text": "Configurable planner prompts", "source": "spec"}
		],
		"non_goals": ["Do not change verify behavior yet"],
		"existing_system": ["Planner prompts are hardcoded in plan.go"],
		"planning_warnings": ["Audit prompt rewrite is separate work"],
		"epics": [
			{
				"epic_ref": "planner-prompts",
				"title": "Planner prompt plumbing",
				"description": "Load and route prompt templates through the planner.",
				"covers": ["REQ-1"],
				"depends_on_epics": []
			}
		]
	}`

	resp, err := parseEpicPlanResponse(raw)
	if err != nil {
		t.Fatalf("parseEpicPlanResponse() error = %v", err)
	}
	if len(resp.Epics) != 1 {
		t.Fatalf("len(Epics) = %d, want 1", len(resp.Epics))
	}
	if resp.Epics[0].EpicRef != "planner-prompts" {
		t.Fatalf("Epics[0].EpicRef = %q, want planner-prompts", resp.Epics[0].EpicRef)
	}
}

func TestParseTaskPlanResponseAcceptsCanonicalTaskOutput(t *testing.T) {
	raw := `{
		"tasks": [
			{
				"task_ref": "wire-planner-prompts",
				"schema_version": 2,
				"title": "Wire planner prompts",
				"type": "refactor",
				"target_module": "cmd/conductor",
				"reference_module": "internal/templates",
				"known_files": ["cmd/conductor/plan.go"],
				"requirements": [
					{"id": "REQ-1", "text": "Configurable planner prompts", "source": "spec"}
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
	}`

	resp, err := parseTaskPlanResponse(raw)
	if err != nil {
		t.Fatalf("parseTaskPlanResponse() error = %v", err)
	}
	if len(resp.Tasks) != 1 {
		t.Fatalf("len(Tasks) = %d, want 1", len(resp.Tasks))
	}
	if resp.Tasks[0].TaskRef != "wire-planner-prompts" {
		t.Fatalf("Tasks[0].TaskRef = %q, want wire-planner-prompts", resp.Tasks[0].TaskRef)
	}
}

func TestParsePlanResponseAcceptsHierarchicalManifest(t *testing.T) {
	raw := `{
		"version": 1,
		"requirements": [
			{"id": "REQ-1", "text": "Configurable planner prompts", "source": "spec"},
			{"id": "REQ-2", "text": "Persist plan metadata", "source": "spec"}
		],
		"non_goals": ["Do not change verify behavior yet"],
		"existing_system": ["Planner prompts are hardcoded in plan.go"],
		"planning_warnings": ["Audit prompt rewrite is separate work"],
		"epics": [
			{
				"id": "epic-001",
				"epic_ref": "planner-prompts",
				"title": "Planner prompt plumbing",
				"description": "Load and route prompt templates through the planner.",
				"covers": ["REQ-1"],
				"depends_on_epics": [],
				"tasks": [
					{
						"id": "task-001",
						"task_ref": "wire-planner-prompts",
						"schema_version": 2,
						"epic_id": "epic-001",
						"title": "Wire planner prompts",
						"type": "refactor",
						"target_module": "cmd/conductor",
						"reference_module": "internal/templates",
						"known_files": ["cmd/conductor/plan.go"],
						"requirements": [
							{"id": "REQ-1", "text": "Configurable planner prompts", "source": "spec"}
						],
						"acceptance_criteria": [
							{
								"id": "AC-1",
								"description": "Configured planner prompts load successfully",
								"requirement_ids": ["REQ-1"],
								"required": true,
								"verification": {"kind": "diff_review", "focus": ["cmd/conductor/plan.go", "internal/templates/prompts.go"]}
							}
						],
						"constraints": ["Keep current YAML output"],
						"depends_on": [],
						"size": "S"
					}
				]
			},
			{
				"id": "epic-002",
				"epic_ref": "plan-metadata",
				"title": "Plan metadata persistence",
				"description": "Persist plan metadata and related artifacts.",
				"covers": ["REQ-2"],
				"depends_on_epics": ["epic-001"],
				"tasks": [
					{
						"id": "task-002",
						"task_ref": "persist-plan-metadata",
						"schema_version": 2,
						"epic_id": "epic-002",
						"title": "Persist plan metadata artifacts",
						"type": "new_feature",
						"target_module": "cmd/conductor",
						"reference_module": "cmd/conductor/plan.go",
						"known_files": ["cmd/conductor/plan.go"],
						"requirements": [
							{"id": "REQ-2", "text": "Persist plan metadata", "source": "spec"}
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
						"depends_on": ["task-001"],
						"size": "M"
					}
				]
			}
		]
	}`

	doc, err := parsePlanResponse(raw)
	if err != nil {
		t.Fatalf("parsePlanResponse() error = %v", err)
	}
	if len(doc.Requirements) != 2 {
		t.Fatalf("len(Requirements) = %d, want 2", len(doc.Requirements))
	}
	if len(doc.Epics) != 2 {
		t.Fatalf("len(Epics) = %d, want 2", len(doc.Epics))
	}
	if doc.Epics[0].EpicRef != "planner-prompts" {
		t.Fatalf("Epics[0].EpicRef = %q, want planner-prompts", doc.Epics[0].EpicRef)
	}
	if doc.Epics[1].Tasks[0].Size != "M" {
		t.Fatalf("Epics[1].Tasks[0].Size = %q, want M", doc.Epics[1].Tasks[0].Size)
	}

	selected, err := selectPlanTask(doc, "task-002")
	if err != nil {
		t.Fatalf("selectPlanTask() error = %v", err)
	}
	if selected.Epic.ID != "epic-002" {
		t.Fatalf("selected.Epic.ID = %q, want epic-002", selected.Epic.ID)
	}
	if selected.Task.ID != "task-002" {
		t.Fatalf("selected.Task.ID = %q, want task-002", selected.Task.ID)
	}
	if len(selected.Task.AcceptanceCriteria) != 1 {
		t.Fatalf("len(selected.Task.AcceptanceCriteria) = %d, want 1", len(selected.Task.AcceptanceCriteria))
	}
}

func TestParsePlanManifestYAMLRejectsLegacyStringAcceptanceCriteria(t *testing.T) {
	raw := []byte(`version: 1
requirements:
  - id: REQ-1
    text: Configurable planner prompts
epics:
  - id: epic-001
    epic_ref: planner-prompts
    title: Planner prompt plumbing
    description: Load and route prompt templates through the planner.
    covers: [REQ-1]
    tasks:
      - id: task-001
        task_ref: wire-planner-prompts
        schema_version: 2
        epic_id: epic-001
        title: Wire planner prompts
        type: refactor
        target_module: cmd/conductor
        known_files: [cmd/conductor/plan.go]
        requirements:
          - id: REQ-1
            text: Configurable planner prompts
        acceptance_criteria:
          - "legacy string criteria are forbidden"
`)

	_, err := parsePlanManifestYAML(raw)
	if err == nil {
		t.Fatal("parsePlanManifestYAML() error = nil, want typed acceptance criteria failure")
	}
	if !strings.Contains(err.Error(), "cannot unmarshal") && !strings.Contains(err.Error(), "typed_acceptance_criteria") {
		t.Fatalf("parsePlanManifestYAML() error = %v, want legacy acceptance criteria failure", err)
	}
}

func TestParsePlanManifestYAMLAndSelectPlanTask(t *testing.T) {
	doc := &planDocument{
		Version: 1,
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Persist hierarchical planner metadata"},
		},
		Epics: []planEpic{
			{
				ID:          "epic-001",
				EpicRef:     "planner-observability",
				Title:       "Planner observability",
				Description: "Persist hierarchical planner metadata.",
				Covers:      []string{"REQ-1"},
				Tasks: []planTask{
					{
						ID:            "task-001",
						TaskRef:       "persist-plan-metadata",
						SchemaVersion: 2,
						EpicID:        "epic-001",
						Title:         "Persist plan metadata",
						Type:          "new_feature",
						TargetModule:  "cmd/conductor",
						KnownFiles:    []string{"cmd/conductor/plan.go"},
						Requirements: []models.WorkOrderRequirement{
							{ID: "REQ-1", Text: "Persist hierarchical planner metadata"},
						},
						AcceptanceCriteria: []models.TypedAcceptanceCriterion{
							{
								ID:             "AC-1",
								Description:    "Plan metadata is persisted",
								RequirementIDs: []string{"REQ-1"},
								Required:       planTestBool(true),
								Verification: models.AcceptanceVerification{
									Kind:  "diff_review",
									Focus: []string{"cmd/conductor/plan.go"},
								},
							},
						},
						Size: "S",
					},
				},
			},
		},
	}

	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	parsed, err := parsePlanManifestYAML(data)
	if err != nil {
		t.Fatalf("parsePlanManifestYAML() error = %v", err)
	}

	selected, err := selectPlanTask(parsed, "task-001")
	if err != nil {
		t.Fatalf("selectPlanTask() error = %v", err)
	}
	if selected.Epic.EpicRef != "planner-observability" {
		t.Fatalf("selected.Epic.EpicRef = %q, want planner-observability", selected.Epic.EpicRef)
	}
	if selected.Task.TaskRef != "persist-plan-metadata" {
		t.Fatalf("selected.Task.TaskRef = %q, want persist-plan-metadata", selected.Task.TaskRef)
	}
}

func TestAssemblePlanDocumentAssignsCanonicalIDsAndRemapsDependencies(t *testing.T) {
	epicResp := &rawEpicPlanResponse{
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Configurable planner prompts"},
			{ID: "REQ-2", Text: "Persist plan metadata"},
		},
		Epics: []rawPlanEpic{
			{
				EpicRef:        "planner-prompts",
				Title:          "Planner prompt plumbing",
				Description:    "Load and route prompt templates through the planner.",
				Covers:         []string{"REQ-1"},
				DependsOnEpics: nil,
			},
			{
				EpicRef:        "plan-metadata",
				Title:          "Plan metadata persistence",
				Description:    "Persist plan metadata and related artifacts.",
				Covers:         []string{"REQ-2"},
				DependsOnEpics: []string{"planner-prompts"},
			},
		},
	}
	taskResponses := []*rawTaskPlanResponse{
		{
			Tasks: []rawPlanTask{
				{
					TaskRef:       "wire-planner-prompts",
					SchemaVersion: 2,
					Title:         "Wire planner prompts",
					Type:          "refactor",
					TargetModule:  "cmd/conductor",
					KnownFiles:    []string{"cmd/conductor/plan.go"},
					Requirements: []models.WorkOrderRequirement{
						{ID: "REQ-1", Text: "Configurable planner prompts"},
					},
					AcceptanceCriteria: []models.TypedAcceptanceCriterion{
						{
							ID:             "AC-1",
							Description:    "Configured planner prompts load successfully",
							RequirementIDs: []string{"REQ-1"},
							Required:       planTestBool(true),
							Verification:   models.AcceptanceVerification{Kind: "diff_review"},
						},
					},
				},
			},
		},
		{
			Tasks: []rawPlanTask{
				{
					TaskRef:       "persist-plan-metadata",
					SchemaVersion: 2,
					Title:         "Persist plan metadata artifacts",
					Type:          "new_feature",
					TargetModule:  "cmd/conductor",
					KnownFiles:    []string{"cmd/conductor/plan.go"},
					Requirements: []models.WorkOrderRequirement{
						{ID: "REQ-2", Text: "Persist plan metadata"},
					},
					AcceptanceCriteria: []models.TypedAcceptanceCriterion{
						{
							ID:             "AC-2",
							Description:    "Structured plan artifacts are written",
							RequirementIDs: []string{"REQ-2"},
							Required:       planTestBool(true),
							Verification:   models.AcceptanceVerification{Kind: "diff_review"},
						},
					},
					DependsOn: []string{"wire-planner-prompts"},
				},
			},
		},
	}

	doc, err := assemblePlanDocument(planManifestMetadata{
		SpecFile:        "spec.md",
		SpecFingerprint: "fingerprint",
		CreatedAt:       "2026-03-19T12:00:00Z",
		SessionID:       "session-123",
		GenerationModel: "claude-sonnet",
	}, epicResp, taskResponses)
	if err != nil {
		t.Fatalf("assemblePlanDocument() error = %v", err)
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

func TestBuildRefMapsAndRemapDependencyRefs(t *testing.T) {
	epics := []planEpic{
		{ID: canonicalEpicID(1), EpicRef: "planner-prompts"},
		{ID: canonicalEpicID(2), EpicRef: "plan-metadata"},
	}

	epicIDsByRef, err := buildEpicIDMap(epics)
	if err != nil {
		t.Fatalf("buildEpicIDMap() error = %v", err)
	}
	remappedEpics, err := remapDependencyRefs([]string{"planner-prompts"}, epicIDsByRef, "depends_on_epics")
	if err != nil {
		t.Fatalf("remapDependencyRefs() error = %v", err)
	}
	if len(remappedEpics) != 1 || remappedEpics[0] != "epic-001" {
		t.Fatalf("remapped epic deps = %v, want [epic-001]", remappedEpics)
	}

	tasks := []planTask{
		{ID: canonicalTaskID(1), TaskRef: "wire-planner-prompts"},
		{ID: canonicalTaskID(2), TaskRef: "persist-plan-metadata"},
	}
	taskIDsByRef, err := buildTaskIDMap(tasks)
	if err != nil {
		t.Fatalf("buildTaskIDMap() error = %v", err)
	}
	remappedTasks, err := remapDependencyRefs([]string{"wire-planner-prompts"}, taskIDsByRef, "depends_on")
	if err != nil {
		t.Fatalf("remapDependencyRefs() error = %v", err)
	}
	if len(remappedTasks) != 1 || remappedTasks[0] != "task-001" {
		t.Fatalf("remapped task deps = %v, want [task-001]", remappedTasks)
	}
}

func TestAuditResponseToPlanDocumentPreservesAuditSourceForChangedTasks(t *testing.T) {
	resp := &auditResponse{
		Version: 1,
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Keep prompt config stable"},
		},
		Epics: []auditPlanEpic{
			{
				ID:          "epic-001",
				EpicRef:     "planner-prompts",
				Title:       "Planner prompts",
				Description: "Keep planner prompt wiring stable.",
				Tasks: []auditPlanTask{
					{
						planTask: planTask{
							ID:            "task-001",
							TaskRef:       "wire-planner-prompts",
							SchemaVersion: 2,
							EpicID:        "epic-001",
							Title:         "Wire planner prompts",
							Type:          "refactor",
							TargetModule:  "cmd/conductor",
							Requirements: []models.WorkOrderRequirement{
								{ID: "REQ-1", Text: "Keep prompt config stable"},
							},
							AcceptanceCriteria: []models.TypedAcceptanceCriterion{
								{
									ID:             "AC-1",
									Description:    "Configured planner prompts load successfully",
									RequirementIDs: []string{"REQ-1"},
									Required:       planTestBool(true),
									Verification: models.AcceptanceVerification{
										Kind:  "diff_review",
										Focus: []string{"cmd/conductor/plan.go"},
									},
								},
							},
						},
						AuditAction: "modified",
					},
					{
						planTask: planTask{
							ID:            "task-002",
							TaskRef:       "leave-unchanged",
							SchemaVersion: 2,
							EpicID:        "epic-001",
							Title:         "Leave unchanged",
							Type:          "docs",
							TargetModule:  "README.md",
							Requirements: []models.WorkOrderRequirement{
								{ID: "REQ-1", Text: "Keep prompt config stable"},
							},
							AcceptanceCriteria: []models.TypedAcceptanceCriterion{
								{
									ID:             "AC-2",
									Description:    "README remains aligned",
									RequirementIDs: []string{"REQ-1"},
									Required:       planTestBool(true),
									Verification:   models.AcceptanceVerification{Kind: "diff_review"},
								},
							},
						},
						AuditAction: "unchanged",
					},
				},
			},
		},
	}

	doc := resp.toPlanDocument()
	if doc.Version != 1 {
		t.Fatalf("Version = %d, want 1", doc.Version)
	}
	if doc.Epics[0].Tasks[0].AuditSource != "modified" {
		t.Fatalf("AuditSource[0] = %q, want modified", doc.Epics[0].Tasks[0].AuditSource)
	}
	if doc.Epics[0].Tasks[1].AuditSource != "" {
		t.Fatalf("AuditSource[1] = %q, want empty for unchanged task", doc.Epics[0].Tasks[1].AuditSource)
	}
}

func TestWritePlanDocumentArtifactWritesStructuredJSON(t *testing.T) {
	tmpDir := t.TempDir()
	doc := &planDocument{
		Version: 1,
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Persist metadata"},
		},
		Epics: []planEpic{
			{
				ID:          "epic-001",
				EpicRef:     "metadata",
				Title:       "Metadata persistence",
				Description: "Persist plan metadata artifacts.",
				Covers:      []string{"REQ-1"},
				Tasks: []planTask{
					{
						ID:            "task-001",
						TaskRef:       "persist-plan-metadata",
						SchemaVersion: 2,
						EpicID:        "epic-001",
						Title:         "Persist plan metadata artifacts",
						Type:          "new_feature",
						TargetModule:  "cmd/conductor",
						Requirements: []models.WorkOrderRequirement{
							{ID: "REQ-1", Text: "Persist metadata"},
						},
						AcceptanceCriteria: []models.TypedAcceptanceCriterion{
							{
								ID:             "AC-1",
								Description:    "Structured plan artifacts are written",
								RequirementIDs: []string{"REQ-1"},
								Required:       planTestBool(true),
								Verification: models.AcceptanceVerification{
									Kind:  "diff_review",
									Focus: []string{"cmd/conductor/plan.go"},
								},
							},
						},
					},
				},
			},
		},
	}

	path, err := writePlanDocumentArtifact(tmpDir, "session-123", "generation-structured.json", doc)
	if err != nil {
		t.Fatalf("writePlanDocumentArtifact() error = %v", err)
	}
	if filepath.Base(path) != "generation-structured.json" {
		t.Fatalf("artifact path = %q, want generation-structured.json basename", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"requirements": [`) {
		t.Fatalf("artifact missing requirements JSON:\n%s", content)
	}
	if !strings.Contains(content, `"epics": [`) {
		t.Fatalf("artifact missing epics JSON:\n%s", content)
	}
	if !strings.Contains(content, `"tasks": [`) {
		t.Fatalf("artifact missing tasks JSON:\n%s", content)
	}
}

func TestWritePlanManifestEmitsCanonicalYAML(t *testing.T) {
	tmpDir := t.TempDir()
	doc := &planDocument{
		Version:         1,
		SpecFile:        "spec.md",
		SpecFingerprint: "fingerprint",
		SessionID:       "session-123",
		GenerationModel: "claude-sonnet",
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Persist metadata"},
		},
		Epics: []planEpic{
			{
				ID:          "epic-001",
				EpicRef:     "metadata",
				Title:       "Metadata persistence",
				Description: "Persist plan metadata artifacts.",
				Covers:      []string{"REQ-1"},
				Tasks: []planTask{
					{
						ID:            "task-001",
						TaskRef:       "persist-plan-metadata",
						SchemaVersion: 2,
						EpicID:        "epic-001",
						Title:         "Persist plan metadata artifacts",
						Type:          "new_feature",
						TargetModule:  "cmd/conductor",
						KnownFiles:    []string{"cmd/conductor/plan.go"},
						Requirements: []models.WorkOrderRequirement{
							{ID: "REQ-1", Text: "Persist metadata"},
						},
						AcceptanceCriteria: []models.TypedAcceptanceCriterion{
							{
								ID:             "AC-1",
								Description:    "Structured plan artifacts are written",
								RequirementIDs: []string{"REQ-1"},
								Required:       planTestBool(true),
								Verification: models.AcceptanceVerification{
									Kind:  "diff_review",
									Focus: []string{"cmd/conductor/plan.go"},
								},
							},
						},
						Constraints: []string{"No new external dependencies"},
					},
				},
			},
		},
	}

	path, err := writePlanManifest(doc, tmpDir)
	if err != nil {
		t.Fatalf("writePlanManifest() error = %v", err)
	}
	if filepath.Base(path) != "plan.yaml" {
		t.Fatalf("path = %q, want plan.yaml basename", path)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "plan.yaml" {
		t.Fatalf("output dir entries = %v, want only plan.yaml", entries)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	content := string(data)
	for _, snippet := range []string{
		"version: 1",
		"spec_file: spec.md",
		"epic_ref: metadata",
		"task_ref: persist-plan-metadata",
		"requirements:",
		"acceptance_criteria:",
		"requirement_ids:",
		"kind: diff_review",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("canonical YAML missing %q:\n%s", snippet, content)
		}
	}
}
