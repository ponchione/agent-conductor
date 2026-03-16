package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
)

func TestValidatePlanDocumentRejectsMissingRequirementCoverage(t *testing.T) {
	projectDir := t.TempDir()
	writePlanValidationFile(t, projectDir, "cmd/conductor/plan.go")

	doc := &planDocument{
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Covered"},
			{ID: "REQ-2", Text: "Uncovered"},
		},
		WorkOrders: []planWorkOrder{
			{
				Title:              "Wire planner prompts",
				Type:               "refactor",
				TargetModule:       "cmd/conductor",
				KnownFiles:         []string{"cmd/conductor/plan.go"},
				AcceptanceCriteria: []string{"make test passes"},
				Covers:             []string{"REQ-1"},
			},
		},
	}

	err := validatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), `requirement "REQ-2" is not covered`) {
		t.Fatalf("validatePlanDocument() error = %v, want missing requirement coverage", err)
	}
}

func TestValidatePlanDocumentRejectsInvalidDependencyOrdering(t *testing.T) {
	projectDir := t.TempDir()
	writePlanValidationFile(t, projectDir, "cmd/conductor/plan.go")
	writePlanValidationFile(t, projectDir, "internal/config/config.go")

	doc := &planDocument{
		WorkOrders: []planWorkOrder{
			{
				Title:              "Second step",
				Type:               "refactor",
				TargetModule:       "cmd/conductor",
				KnownFiles:         []string{"cmd/conductor/plan.go"},
				AcceptanceCriteria: []string{"make test passes"},
				DependsOn:          []string{"Third step"},
			},
			{
				Title:              "Third step",
				Type:               "refactor",
				TargetModule:       "internal/config",
				KnownFiles:         []string{"internal/config/config.go"},
				AcceptanceCriteria: []string{"make test passes"},
			},
		},
	}

	err := validatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), "depends_on references work order") {
		t.Fatalf("validatePlanDocument() error = %v, want dependency ordering failure", err)
	}
}

func TestValidatePlanDocumentRejectsImpossibleKnownFiles(t *testing.T) {
	projectDir := t.TempDir()
	doc := &planDocument{
		WorkOrders: []planWorkOrder{
			{
				Title:              "Broken known files",
				Type:               "bug_fix",
				TargetModule:       "cmd/conductor",
				KnownFiles:         []string{"does/not/exist.go"},
				AcceptanceCriteria: []string{"make test passes"},
			},
		},
	}

	err := validatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), `known_files path "does/not/exist.go" does not exist`) {
		t.Fatalf("validatePlanDocument() error = %v, want known_files failure", err)
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

	doc := &planDocument{
		WorkOrders: []planWorkOrder{
			{
				Title:              "Oversized work order",
				Type:               "refactor",
				TargetModule:       "internal",
				KnownFiles:         paths,
				AcceptanceCriteria: []string{"make test passes"},
			},
		},
	}

	err := validatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), "is oversized") {
		t.Fatalf("validatePlanDocument() error = %v, want oversized failure", err)
	}

	doc = &planDocument{
		WorkOrders: []planWorkOrder{
			{
				Title:              "First",
				Type:               "refactor",
				TargetModule:       "internal",
				KnownFiles:         []string{"internal/file0.go", "internal/file1.go"},
				AcceptanceCriteria: []string{"make test passes"},
			},
			{
				Title:              "Second",
				Type:               "refactor",
				TargetModule:       "internal",
				KnownFiles:         []string{"internal/file1.go", "internal/file0.go"},
				AcceptanceCriteria: []string{"make test passes"},
			},
		},
	}

	err = validatePlanDocument(doc, validationConfig(projectDir))
	if err == nil || !strings.Contains(err.Error(), "obviously overlaps") {
		t.Fatalf("validatePlanDocument() error = %v, want overlap failure", err)
	}
}

func TestResolveAuditOutcomeFailsClosedByDefault(t *testing.T) {
	base := &planDocument{}
	_, _, _, err := resolveAuditOutcome(base, nil, nil, nil, fmt.Errorf("bad audit"), false)
	if err == nil || !strings.Contains(err.Error(), "unaudited fallback is disabled") {
		t.Fatalf("resolveAuditOutcome() error = %v, want fail-closed error", err)
	}
}

func TestResolveAuditOutcomeAllowsExplicitFallback(t *testing.T) {
	base := &planDocument{WorkOrders: []planWorkOrder{{Title: "Base", Type: "refactor", TargetModule: "cmd", AcceptanceCriteria: []string{"make test passes"}}}}
	resolved, summary, result, err := resolveAuditOutcome(base, nil, nil, nil, fmt.Errorf("bad audit"), true)
	if err != nil {
		t.Fatalf("resolveAuditOutcome() error = %v", err)
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
