package planner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/models"
)

func validationBool(v bool) *bool {
	return &v
}

func validationCriterion(reqID string) []models.TypedAcceptanceCriterion {
	return []models.TypedAcceptanceCriterion{
		{
			ID:             "AC-1",
			Description:    "make test passes",
			RequirementIDs: []string{reqID},
			Required:       validationBool(true),
			Verification: models.AcceptanceVerification{
				Kind:  "diff_review",
				Focus: []string{"cmd/conductor/plan.go"},
			},
		},
	}
}

func validationTask(id, ref, epicID, title, targetModule, knownFile, reqID, reqText string) PlanTask {
	return PlanTask{
		ID:            id,
		TaskRef:       ref,
		SchemaVersion: 2,
		EpicID:        epicID,
		Title:         title,
		Type:          "refactor",
		TargetModule:  targetModule,
		KnownFiles:    []string{knownFile},
		Requirements: []models.WorkOrderRequirement{
			{ID: reqID, Text: reqText},
		},
		AcceptanceCriteria: validationCriterion(reqID),
	}
}

func validationDoc(epics ...PlanEpic) *PlanDocument {
	return &PlanDocument{
		Version: 1,
		Epics:   epics,
	}
}

func TestValidatePlanDocumentRejectsMissingRequirementCoverage(t *testing.T) {
	projectDir := t.TempDir()
	writePlanValidationFile(t, projectDir, "cmd/conductor/plan.go")

	doc := validationDoc(PlanEpic{
		ID:          "epic-001",
		EpicRef:     "planner-prompts",
		Title:       "Planner prompts",
		Description: "Wire planner prompts into the command.",
		Covers:      []string{"REQ-1"},
		Tasks: []PlanTask{
			validationTask("task-001", "wire-prompts", "epic-001", "Wire planner prompts", "cmd/conductor", "cmd/conductor/plan.go", "REQ-1", "Covered"),
		},
	})
	doc.Requirements = []PlanRequirement{
		{ID: "REQ-1", Text: "Covered"},
		{ID: "REQ-2", Text: "Uncovered"},
	}

	err := ValidatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), `requirement "REQ-2" is not covered`) {
		t.Fatalf("ValidatePlanDocument() error = %v, want missing requirement coverage", err)
	}
}

func TestValidatePlanDocumentRejectsInvalidDependencyOrdering(t *testing.T) {
	projectDir := t.TempDir()
	writePlanValidationFile(t, projectDir, "cmd/conductor/plan.go")
	writePlanValidationFile(t, projectDir, "internal/config/config.go")

	first := validationTask("task-001", "second-step", "epic-001", "Second step", "cmd/conductor", "cmd/conductor/plan.go", "REQ-1", "Run second")
	first.DependsOn = []string{"task-002"}
	second := validationTask("task-002", "third-step", "epic-001", "Third step", "internal/config", "internal/config/config.go", "REQ-2", "Run third")

	doc := validationDoc(PlanEpic{
		ID:          "epic-001",
		EpicRef:     "planner-prompts",
		Title:       "Planner prompts",
		Description: "Wire planner prompts into the command.",
		Covers:      []string{"REQ-1", "REQ-2"},
		Tasks:       []PlanTask{first, second},
	})

	err := ValidatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), "depends_on references task") {
		t.Fatalf("ValidatePlanDocument() error = %v, want dependency ordering failure", err)
	}
}

func TestValidatePlanDocumentRejectsUnknownDependencyRef(t *testing.T) {
	projectDir := t.TempDir()
	writePlanValidationFile(t, projectDir, "cmd/conductor/plan.go")

	task := validationTask("task-001", "broken-deps", "epic-001", "Broken deps", "cmd/conductor", "cmd/conductor/plan.go", "REQ-1", "Task with unknown dependency")
	task.DependsOn = []string{"task-999"}

	doc := validationDoc(PlanEpic{
		ID:          "epic-001",
		EpicRef:     "planner-prompts",
		Title:       "Planner prompts",
		Description: "Wire planner prompts into the command.",
		Covers:      []string{"REQ-1"},
		Tasks:       []PlanTask{task},
	})
	doc.Requirements = []PlanRequirement{{ID: "REQ-1", Text: "Task with unknown dependency"}}

	err := ValidatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), `unknown task id "task-999"`) {
		t.Fatalf("ValidatePlanDocument() error = %v, want unknown dependency failure", err)
	}
}

func TestValidatePlanDocumentRejectsTaskRefDependencyInsteadOfCanonicalTaskID(t *testing.T) {
	projectDir := t.TempDir()
	writePlanValidationFile(t, projectDir, "cmd/conductor/plan.go")
	writePlanValidationFile(t, projectDir, "internal/config/config.go")

	first := validationTask("task-001", "wire-prompts", "epic-001", "Wire prompts", "cmd/conductor", "cmd/conductor/plan.go", "REQ-1", "Wire prompts")
	second := validationTask("task-002", "persist-metadata", "epic-001", "Persist metadata", "internal/config", "internal/config/config.go", "REQ-2", "Persist metadata")
	second.DependsOn = []string{"wire-prompts"}

	doc := validationDoc(PlanEpic{
		ID:          "epic-001",
		EpicRef:     "planner-prompts",
		Title:       "Planner prompts",
		Description: "Wire planner prompts into the command.",
		Covers:      []string{"REQ-1", "REQ-2"},
		Tasks:       []PlanTask{first, second},
	})
	doc.Requirements = []PlanRequirement{
		{ID: "REQ-1", Text: "Wire prompts"},
		{ID: "REQ-2", Text: "Persist metadata"},
	}

	err := ValidatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), `unknown task id "wire-prompts"`) {
		t.Fatalf("ValidatePlanDocument() error = %v, want canonical task id dependency failure", err)
	}
}

func TestValidatePlanDocumentRejectsDuplicateEpicRefs(t *testing.T) {
	projectDir := t.TempDir()
	writePlanValidationFile(t, projectDir, "cmd/conductor/plan.go")

	doc := validationDoc(
		PlanEpic{
			ID:          "epic-001",
			EpicRef:     "shared-ref",
			Title:       "First epic",
			Description: "First epic description.",
			Covers:      []string{"REQ-1"},
			Tasks: []PlanTask{
				validationTask("task-001", "first-task", "epic-001", "First task", "cmd/conductor", "cmd/conductor/plan.go", "REQ-1", "First requirement"),
			},
		},
		PlanEpic{
			ID:          "epic-002",
			EpicRef:     "shared-ref",
			Title:       "Second epic",
			Description: "Second epic description.",
			Covers:      []string{"REQ-2"},
			Tasks: []PlanTask{
				validationTask("task-002", "second-task", "epic-002", "Second task", "cmd/conductor", "cmd/conductor/plan.go", "REQ-2", "Second requirement"),
			},
		},
	)
	doc.Requirements = []PlanRequirement{
		{ID: "REQ-1", Text: "First requirement"},
		{ID: "REQ-2", Text: "Second requirement"},
	}

	err := ValidatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), `duplicate epic_ref "shared-ref"`) {
		t.Fatalf("ValidatePlanDocument() error = %v, want duplicate epic_ref failure", err)
	}
}

func TestValidatePlanDocumentRejectsDuplicateTaskRefs(t *testing.T) {
	projectDir := t.TempDir()
	writePlanValidationFile(t, projectDir, "cmd/conductor/plan.go")

	doc := validationDoc(PlanEpic{
		ID:          "epic-001",
		EpicRef:     "planner-prompts",
		Title:       "Planner prompts",
		Description: "Wire planner prompts into the command.",
		Covers:      []string{"REQ-1", "REQ-2"},
		Tasks: []PlanTask{
			validationTask("task-001", "shared-task-ref", "epic-001", "First task", "cmd/conductor", "cmd/conductor/plan.go", "REQ-1", "First requirement"),
			validationTask("task-002", "shared-task-ref", "epic-001", "Second task", "cmd/conductor", "cmd/conductor/plan.go", "REQ-2", "Second requirement"),
		},
	})
	doc.Requirements = []PlanRequirement{
		{ID: "REQ-1", Text: "First requirement"},
		{ID: "REQ-2", Text: "Second requirement"},
	}

	err := ValidatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), `duplicate task_ref "shared-task-ref"`) {
		t.Fatalf("ValidatePlanDocument() error = %v, want duplicate task_ref failure", err)
	}
}

func TestValidatePlanDocumentRejectsImpossibleKnownFiles(t *testing.T) {
	projectDir := t.TempDir()
	doc := validationDoc(PlanEpic{
		ID:          "epic-001",
		EpicRef:     "planner-prompts",
		Title:       "Planner prompts",
		Description: "Wire planner prompts into the command.",
		Covers:      []string{"REQ-1"},
		Tasks: []PlanTask{
			validationTask("task-001", "broken-known-files", "epic-001", "Broken known files", "cmd/conductor", "does/not/exist.go", "REQ-1", "Fix path handling"),
		},
	})
	doc.Requirements = []PlanRequirement{{ID: "REQ-1", Text: "Fix path handling"}}

	err := ValidatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), `known_files path "does/not/exist.go" does not exist`) {
		t.Fatalf("ValidatePlanDocument() error = %v, want known_files failure", err)
	}
}

func TestValidatePlanDocumentRejectsOversizedAndObviousOverlap(t *testing.T) {
	projectDir := t.TempDir()
	paths := make([]string, 0, maxPlanKnownFilesPerWorkOrder+1)
	for i := 0; i < maxPlanKnownFilesPerWorkOrder+1; i++ {
		relPath := fmt.Sprintf("internal/file%d.go", i)
		writePlanValidationFile(t, projectDir, relPath)
		paths = append(paths, relPath)
	}

	oversizedTask := validationTask("task-001", "oversized-task", "epic-001", "Oversized task", "internal", paths[0], "REQ-1", "Refactor internal package")
	oversizedTask.KnownFiles = paths
	doc := validationDoc(PlanEpic{
		ID:          "epic-001",
		EpicRef:     "oversized-epic",
		Title:       "Oversized epic",
		Description: "Contains an oversized task.",
		Covers:      []string{"REQ-1"},
		Tasks:       []PlanTask{oversizedTask},
	})
	doc.Requirements = []PlanRequirement{{ID: "REQ-1", Text: "Refactor internal package"}}

	err := ValidatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), "is oversized") {
		t.Fatalf("ValidatePlanDocument() error = %v, want oversized failure", err)
	}

	first := validationTask("task-001", "first", "epic-001", "First", "internal", "internal/file0.go", "REQ-1", "Refactor file one")
	first.KnownFiles = []string{"internal/file0.go", "internal/file1.go"}
	second := validationTask("task-002", "second", "epic-001", "Second", "internal", "internal/file1.go", "REQ-2", "Refactor file two")
	second.KnownFiles = []string{"internal/file1.go", "internal/file0.go"}

	doc = validationDoc(PlanEpic{
		ID:          "epic-001",
		EpicRef:     "overlap-epic",
		Title:       "Overlap epic",
		Description: "Contains overlapping tasks.",
		Covers:      []string{"REQ-1", "REQ-2"},
		Tasks:       []PlanTask{first, second},
	})
	doc.Requirements = []PlanRequirement{
		{ID: "REQ-1", Text: "Refactor file one"},
		{ID: "REQ-2", Text: "Refactor file two"},
	}

	err = ValidatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), "obviously overlaps") {
		t.Fatalf("ValidatePlanDocument() error = %v, want overlap failure", err)
	}
}

func TestResolveAuditOutcomeFailsClosedByDefault(t *testing.T) {
	base := &PlanDocument{}
	_, _, _, err := ResolveAuditOutcome(base, nil, nil, nil, fmt.Errorf("bad audit"), false)
	if err == nil || !strings.Contains(err.Error(), "unaudited fallback is disabled") {
		t.Fatalf("ResolveAuditOutcome() error = %v, want fail-closed error", err)
	}
}

func TestResolveAuditOutcomeAllowsExplicitFallback(t *testing.T) {
	base := &PlanDocument{
		Version: 1,
		Epics: []PlanEpic{
			{
				ID:          "epic-001",
				EpicRef:     "base-epic",
				Title:       "Base epic",
				Description: "Base epic description.",
				Tasks: []PlanTask{
					{
						ID:            "task-001",
						TaskRef:       "base-task",
						SchemaVersion: 2,
						EpicID:        "epic-001",
						Title:         "Base",
						Type:          "refactor",
						TargetModule:  "cmd",
						Requirements: []models.WorkOrderRequirement{
							{ID: "REQ-1", Text: "Base requirement"},
						},
						AcceptanceCriteria: validationCriterion("REQ-1"),
					},
				},
			},
		},
	}
	resolved, summary, result, err := ResolveAuditOutcome(base, nil, nil, nil, fmt.Errorf("bad audit"), true)
	if err != nil {
		t.Fatalf("ResolveAuditOutcome() error = %v", err)
	}
	if resolved != base {
		t.Fatal("expected base plan to be returned when fallback is enabled")
	}
	if summary != nil || result != nil {
		t.Fatal("expected summary and audit result to be nil when falling back")
	}
}

func validationConfig(projectDir string) *config.ProjectConfig {
	return &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
	}
}

func writePlanValidationFile(t *testing.T, root, relPath string) {
	t.Helper()
	absPath := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", absPath, err)
	}
	if err := os.WriteFile(absPath, []byte("package stub\n"), 0644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", absPath, err)
	}
}
