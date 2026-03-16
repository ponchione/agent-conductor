package verify

import (
	"testing"

	"github.com/ponchione/agent-conductor/internal/models"
)

func TestNormalizeVerificationReportMergesMetadataAndDerivesWarnForRequiredUnassessable(t *testing.T) {
	required := true
	wo := &models.WorkOrder{
		SchemaVersion: 2,
		TypedAcceptanceCriteria: []models.TypedAcceptanceCriterion{
			{
				ID:             "AC-1",
				Description:    "Configured test precheck passes",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification: models.AcceptanceVerification{
					Kind:  "precheck",
					Check: "test",
				},
			},
			{
				ID:             "AC-2",
				Description:    "API diff review passes",
				RequirementIDs: []string{"REQ-2"},
				Required:       &required,
				Verification: models.AcceptanceVerification{
					Kind: "diff_review",
				},
			},
		},
	}

	report := &models.VerificationReport{
		Summary: "Some checks could not be fully assessed",
		PatternConsistency: models.PatternConsistency{
			FollowsConventions: true,
		},
		Completeness: models.Completeness{
			CriteriaResults: []models.CriterionResult{
				{
					CriterionID: "AC-2",
					Criterion:   "API diff review passes",
					Result:      models.CriterionResultMet,
					Notes:       "Diff review looked good",
				},
			},
		},
	}

	preChecks := []PreCheckResult{
		{
			CriterionID:      "AC-1",
			Criterion:        "Configured test precheck passes",
			Required:         &required,
			Result:           models.CriterionResultUnassessable,
			VerificationKind: "precheck",
			Notes:            "Configured command missing in this environment",
		},
	}

	normalized := NormalizeVerificationReport(wo, report, preChecks)
	if normalized.Status != statusWarn {
		t.Fatalf("Status = %q, want WARN", normalized.Status)
	}
	if normalized.Completeness.AllCriteriaMet {
		t.Fatal("AllCriteriaMet = true, want false")
	}
	if len(normalized.Completeness.CriteriaResults) != 2 {
		t.Fatalf("len(CriteriaResults) = %d, want 2", len(normalized.Completeness.CriteriaResults))
	}
	if normalized.Completeness.CriteriaResults[0].CriterionID != "AC-1" {
		t.Fatalf("CriterionID[0] = %q, want AC-1", normalized.Completeness.CriteriaResults[0].CriterionID)
	}
	if normalized.Completeness.CriteriaResults[0].VerificationKind != "precheck" {
		t.Fatalf("VerificationKind[0] = %q, want precheck", normalized.Completeness.CriteriaResults[0].VerificationKind)
	}
	if normalized.Completeness.CriteriaResults[0].NormalizedResult() != models.CriterionResultUnassessable {
		t.Fatalf("Result[0] = %q, want unassessable", normalized.Completeness.CriteriaResults[0].NormalizedResult())
	}
}

func TestDeriveWorkflowStatusAdvisoryUnmetDoesNotFailWorkflow(t *testing.T) {
	required := false
	report := &models.VerificationReport{
		Completeness: models.Completeness{
			CriteriaResults: []models.CriterionResult{
				{
					CriterionID: "AC-1",
					Criterion:   "Non-blocking docs follow-up",
					Required:    &required,
					Result:      models.CriterionResultUnmet,
				},
			},
		},
		PatternConsistency: models.PatternConsistency{
			FollowsConventions: true,
		},
	}

	if status := DeriveWorkflowStatus(report); status != statusWarn {
		t.Fatalf("DeriveWorkflowStatus() = %q, want WARN", status)
	}
}

func TestBuildPrecheckOnlyReportFailsOnRequiredUnmetCriterion(t *testing.T) {
	required := true
	wo := &models.WorkOrder{
		SchemaVersion: 2,
		TypedAcceptanceCriteria: []models.TypedAcceptanceCriterion{
			{
				ID:             "AC-1",
				Description:    "Configured build precheck passes",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification: models.AcceptanceVerification{
					Kind:  "precheck",
					Check: "build",
				},
			},
		},
	}

	report := BuildPrecheckOnlyReport(wo, []PreCheckResult{
		{
			CriterionID:      "AC-1",
			Criterion:        "Configured build precheck passes",
			Required:         &required,
			Result:           models.CriterionResultUnmet,
			VerificationKind: "precheck",
			Notes:            "make build exited non-zero",
		},
	}, "Build failed")

	if report.Status != statusFail {
		t.Fatalf("Status = %q, want FAIL", report.Status)
	}
	if report.Completeness.AllCriteriaMet {
		t.Fatal("AllCriteriaMet = true, want false")
	}
}
