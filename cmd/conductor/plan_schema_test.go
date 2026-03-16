package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
				"title": "Wire planner prompts",
				"type": "refactor",
				"target_module": "cmd/conductor",
				"reference_module": "internal/templates",
				"known_files": ["cmd/conductor/plan.go"],
				"acceptance_criteria": ["make test passes"],
				"constraints": ["Keep current YAML output"],
				"covers": ["REQ-1"],
				"depends_on": [],
				"why_now": "Prompt loading must exist before prompt rewrite",
				"size": "S"
			},
			{
				"title": "Persist plan metadata artifacts",
				"type": "new_feature",
				"target_module": "cmd/conductor",
				"reference_module": "cmd/conductor/plan.go",
				"known_files": ["cmd/conductor/plan.go"],
				"acceptance_criteria": ["Structured plan artifacts are written"],
				"constraints": ["Keep generated YAML compatible with current pipeline"],
				"covers": ["REQ-2"],
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
}

func TestPlanDocumentInheritMissingMetadata(t *testing.T) {
	original := &planDocument{
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Keep prompt config stable"},
		},
		NonGoals:         []string{"No verify changes"},
		ExistingSystem:   []string{"Planner prompt is hardcoded"},
		PlanningWarnings: []string{"Audit still returns legacy schema"},
		WorkOrders: []planWorkOrder{
			{
				Title:        "Wire planner prompts",
				Type:         "refactor",
				TargetModule: "cmd/conductor",
				Covers:       []string{"REQ-1"},
				DependsOn:    []string{"Bootstrap"},
				WhyNow:       "This must happen first",
				Size:         "S",
			},
		},
	}
	audited := &planDocument{
		WorkOrders: []planWorkOrder{
			{
				Title:              "Wire planner prompts",
				Type:               "refactor",
				TargetModule:       "cmd/conductor",
				KnownFiles:         []string{"cmd/conductor/plan.go"},
				AcceptanceCriteria: []string{"make test passes"},
			},
		},
	}

	audited.InheritMissingMetadata(original)

	if len(audited.Requirements) != 1 || audited.Requirements[0].ID != "REQ-1" {
		t.Fatalf("requirements were not inherited: %+v", audited.Requirements)
	}
	if len(audited.WorkOrders[0].Covers) != 1 || audited.WorkOrders[0].Covers[0] != "REQ-1" {
		t.Fatalf("covers were not inherited: %+v", audited.WorkOrders[0].Covers)
	}
	if audited.WorkOrders[0].WhyNow != "This must happen first" {
		t.Fatalf("WhyNow = %q, want inherited value", audited.WorkOrders[0].WhyNow)
	}
	if audited.WorkOrders[0].Size != "S" {
		t.Fatalf("Size = %q, want S", audited.WorkOrders[0].Size)
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
				Title:              "Persist plan metadata artifacts",
				Type:               "new_feature",
				TargetModule:       "cmd/conductor",
				AcceptanceCriteria: []string{"Structured plan artifacts are written"},
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
