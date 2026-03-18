package models_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/models"
)

func TestVerificationReportRejectsStringUnscopedFiles(t *testing.T) {
	input := `{"status":"WARN","summary":"test","scope_drift":{"detected":true,"unscoped_files":["path/a","path/b"]},"completeness":{"all_criteria_met":true,"criteria_results":[]},"pattern_consistency":{"follows_conventions":true,"issues":[]},"concerns":[]}`
	var report models.VerificationReport
	err := json.Unmarshal([]byte(input), &report)
	if err == nil || !strings.Contains(err.Error(), "cannot unmarshal string into Go struct field ScopeDrift.scope_drift.unscoped_files") {
		t.Fatalf("json.Unmarshal() error = %v, want canonical unscoped_files object-array rejection", err)
	}
}

func TestVerificationReportAcceptsCanonicalScopeDriftObjects(t *testing.T) {
	input := `{"status":"PASS","summary":"ok","scope_drift":{"detected":true,"unscoped_files":[{"path":"internal/foo.go","reason_concerning":"out of scope"}]},"completeness":{"all_criteria_met":true,"criteria_results":[]},"pattern_consistency":{"follows_conventions":true,"issues":[]},"concerns":[]}`
	var report models.VerificationReport
	if err := json.Unmarshal([]byte(input), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(report.ScopeDrift.UnscopedFiles) != 1 {
		t.Fatalf("len(UnscopedFiles) = %d, want 1", len(report.ScopeDrift.UnscopedFiles))
	}
	if report.ScopeDrift.UnscopedFiles[0].ReasonConcerning != "out of scope" {
		t.Fatalf("ReasonConcerning = %q, want out of scope", report.ScopeDrift.UnscopedFiles[0].ReasonConcerning)
	}
}

func TestCriterionResultIgnoresLegacyCriterionAndMetFields(t *testing.T) {
	input := `{"criterion":"go test passes","met":true,"notes":"verified"}`
	var result models.CriterionResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if result.Criterion != "" {
		t.Fatalf("Criterion = %q, want empty when only legacy field names are present", result.Criterion)
	}
	if result.NormalizedResult() != models.CriterionResultUnassessable {
		t.Fatalf("NormalizedResult() = %q, want unassessable without canonical result field", result.NormalizedResult())
	}
}

func TestCriterionResultAcceptsCanonicalFields(t *testing.T) {
	input := `{"criterion_id":"AC-1","description":"go test passes","required":true,"result":"met","verification_kind":"precheck","notes":"verified"}`
	var result models.CriterionResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if result.CriterionID != "AC-1" {
		t.Fatalf("CriterionID = %q, want AC-1", result.CriterionID)
	}
	if result.Criterion != "go test passes" {
		t.Fatalf("Criterion = %q, want go test passes", result.Criterion)
	}
	if result.NormalizedResult() != models.CriterionResultMet {
		t.Fatalf("NormalizedResult() = %q, want met", result.NormalizedResult())
	}
	if result.VerificationKind != "precheck" {
		t.Fatalf("VerificationKind = %q, want precheck", result.VerificationKind)
	}
}
