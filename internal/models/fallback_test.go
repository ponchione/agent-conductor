package models_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ponchione/agent-conductor/internal/models"
)

func TestScopeDriftStringFallback(t *testing.T) {
	input := `{"status":"WARN","summary":"test","scope_drift":{"detected":true,"unscoped_files":["path/a","path/b"]},"completeness":{"all_criteria_met":true,"criteria_results":[]},"pattern_consistency":{"follows_conventions":true,"issues":[]},"concerns":[]}`
	var r models.VerificationReport
	if err := json.Unmarshal([]byte(input), &r); err != nil {
		t.Fatal("unmarshal failed:", err)
	}
	fmt.Printf("status=%s drift=%v files=%+v\n", r.Status, r.ScopeDrift.Detected, r.ScopeDrift.UnscopedFiles)
	if len(r.ScopeDrift.UnscopedFiles) != 2 {
		t.Fatalf("expected 2 files, got %d", len(r.ScopeDrift.UnscopedFiles))
	}
	if r.ScopeDrift.UnscopedFiles[0].Path != "path/a" {
		t.Fatalf("unexpected path: %s", r.ScopeDrift.UnscopedFiles[0].Path)
	}
	if r.ScopeDrift.UnscopedFiles[0].ReasonConcerning != "unclassified (string fallback)" {
		t.Fatalf("unexpected reason: %s", r.ScopeDrift.UnscopedFiles[0].ReasonConcerning)
	}
}

func TestScopeDriftObjectArray(t *testing.T) {
	input := `{"status":"PASS","summary":"ok","scope_drift":{"detected":true,"unscoped_files":[{"path":"internal/foo.go","reason_concerning":"out of scope"}]},"completeness":{"all_criteria_met":true,"criteria_results":[]},"pattern_consistency":{"follows_conventions":true,"issues":[]},"concerns":[]}`
	var r models.VerificationReport
	if err := json.Unmarshal([]byte(input), &r); err != nil {
		t.Fatal("unmarshal failed:", err)
	}
	if len(r.ScopeDrift.UnscopedFiles) != 1 {
		t.Fatalf("expected 1 file, got %d", len(r.ScopeDrift.UnscopedFiles))
	}
	if r.ScopeDrift.UnscopedFiles[0].ReasonConcerning != "out of scope" {
		t.Fatalf("unexpected reason: %s", r.ScopeDrift.UnscopedFiles[0].ReasonConcerning)
	}
}

func TestCriterionResultLegacyBooleanFallback(t *testing.T) {
	input := `{"criterion":"go test passes","met":true,"notes":"verified"}`
	var result models.CriterionResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		t.Fatal("unmarshal failed:", err)
	}
	if result.Criterion != "go test passes" {
		t.Fatalf("Criterion = %q, want go test passes", result.Criterion)
	}
	if result.NormalizedResult() != models.CriterionResultMet {
		t.Fatalf("Result = %q, want met", result.NormalizedResult())
	}
}
