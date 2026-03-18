package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/models"
)

func planTestBool(v bool) *bool {
	return &v
}

func TestParsePlanResponseAcceptsRichSchema(t *testing.T) {
	raw := `{
		"requirements": [
			{"id": "REQ-1", "text": "Configurable planner prompts", "source": "spec"},
			{"id": "REQ-2", "text": "Persist plan metadata", "source": "spec"}
		],
		"non_goals": ["Do not change verify behavior yet"],
		"existing_system": ["Planner prompts are hardcoded in plan.go"],
		"planning_warnings": ["Audit prompt rewrite is separate work"],
		"work_orders": [
			{
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
						"verification": {"kind": "diff_review", "focus": ["cmd/conductor/plan.go", "internal/templates/prompts.go"]}
					}
				],
				"constraints": ["Keep current YAML output"],
				"depends_on": [],
				"why_now": "Prompt loading must exist before prompt rewrite",
				"size": "S"
			},
			{
				"schema_version": 2,
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
				"depends_on": ["Wire planner prompts"],
				"why_now": "Persistence depends on richer parser output",
				"size": "M"
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
	if doc.Requirements[0].ID != "REQ-1" {
		t.Fatalf("Requirements[0].ID = %q, want REQ-1", doc.Requirements[0].ID)
	}
	if len(doc.WorkOrders) != 2 {
		t.Fatalf("len(WorkOrders) = %d, want 2", len(doc.WorkOrders))
	}
	if doc.WorkOrders[0].WhyNow == "" {
		t.Fatal("expected WhyNow to be parsed")
	}
	if doc.WorkOrders[1].Size != "M" {
		t.Fatalf("WorkOrders[1].Size = %q, want M", doc.WorkOrders[1].Size)
	}

	workOrders, err := doc.ToWorkOrders()
	if err != nil {
		t.Fatalf("ToWorkOrders() error = %v", err)
	}
	if len(workOrders) != 2 {
		t.Fatalf("len(ToWorkOrders()) = %d, want 2", len(workOrders))
	}
	if workOrders[0].Title != "Wire planner prompts" {
		t.Fatalf("workOrders[0].Title = %q, want %q", workOrders[0].Title, "Wire planner prompts")
	}
	if workOrders[0].SchemaVersion != 2 {
		t.Fatalf("workOrders[0].SchemaVersion = %d, want 2", workOrders[0].SchemaVersion)
	}
	if len(workOrders[0].TypedAcceptanceCriteria) != 1 {
		t.Fatalf("len(workOrders[0].TypedAcceptanceCriteria) = %d, want 1", len(workOrders[0].TypedAcceptanceCriteria))
	}
}

func TestAuditResponseToPlanDocumentPreservesAuditSourceForChangedWorkOrders(t *testing.T) {
	resp := &auditResponse{
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Keep prompt config stable"},
		},
		WorkOrders: []auditPlanWorkOrder{
			{
				planWorkOrder: planWorkOrder{
					SchemaVersion: 2,
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
				planWorkOrder: planWorkOrder{
					SchemaVersion: 2,
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
	}

	doc := resp.toPlanDocument()
	if doc.WorkOrders[0].AuditSource != "modified" {
		t.Fatalf("AuditSource[0] = %q, want modified", doc.WorkOrders[0].AuditSource)
	}
	if doc.WorkOrders[1].AuditSource != "" {
		t.Fatalf("AuditSource[1] = %q, want empty for unchanged work order", doc.WorkOrders[1].AuditSource)
	}
}

func TestWritePlanDocumentArtifactWritesStructuredJSON(t *testing.T) {
	tmpDir := t.TempDir()
	doc := &planDocument{
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Persist metadata"},
		},
		WorkOrders: []planWorkOrder{
			{
				SchemaVersion: 2,
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
	if !strings.Contains(content, `"work_orders": [`) {
		t.Fatalf("artifact missing work_orders JSON:\n%s", content)
	}
}

func TestWriteWorkOrderFilesEmitsCanonicalVersionTwoYAML(t *testing.T) {
	tmpDir := t.TempDir()
	workOrders := []models.WorkOrder{
		{
			SchemaVersion: 2,
			Title:         "Persist plan metadata artifacts",
			Type:          "new_feature",
			TargetModule:  "cmd/conductor",
			KnownFiles:    []string{"cmd/conductor/plan.go"},
			Requirements: []models.WorkOrderRequirement{
				{ID: "REQ-1", Text: "Persist metadata"},
			},
			TypedAcceptanceCriteria: []models.TypedAcceptanceCriterion{
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
	}

	paths, err := writeWorkOrderFiles(workOrders, tmpDir)
	if err != nil {
		t.Fatalf("writeWorkOrderFiles() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("len(paths) = %d, want 1", len(paths))
	}

	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", paths[0], err)
	}
	content := string(data)
	for _, snippet := range []string{
		"schema_version: 2",
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
