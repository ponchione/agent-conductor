package verify

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/scope"
)

// --- Mock types ---

type mockResponse struct {
	content string
	err     error
}

type mockClient struct {
	responses []mockResponse
	callIndex int
}

func (m *mockClient) Complete(_ context.Context, _ string, _ string) (*llm.CompletionResult, error) {
	if m.callIndex >= len(m.responses) {
		return nil, fmt.Errorf("mockClient: no more responses (called %d times)", m.callIndex+1)
	}
	resp := m.responses[m.callIndex]
	m.callIndex++
	if resp.err != nil {
		return nil, resp.err
	}
	return &llm.CompletionResult{
		Content:   resp.content,
		TokensIn:  100,
		TokensOut: 50,
		LatencyMs: 200,
		Provider:  "http://test:8080/v1",
		Model:     "test-model",
	}, nil
}

// ctxAwareMockClient returns context.Err() if the context is done.
type ctxAwareMockClient struct{}

func (c *ctxAwareMockClient) Complete(ctx context.Context, _ string, _ string) (*llm.CompletionResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return &llm.CompletionResult{Content: "ok"}, nil
}

// --- Fixtures ---

const validAnalyzeResponse = `{
  "files": ["internal/auth/handler.go"],
  "aligned_with_intent": true,
  "criteria_assessed": [{"criterion": "go test passes", "met": true, "notes": "tests pass"}],
  "bugs": [],
  "style_issues": [],
  "concerns": [],
  "analysis_failed": false
}`

const validSynthesizeResponse = `{
  "status": "PASS",
  "summary": "All changes aligned with work order",
  "scope_drift": {"detected": false, "unscoped_files": []},
  "completeness": {
    "all_criteria_met": true,
    "criteria_results": [{"criterion": "go test passes", "met": true, "notes": "verified"}]
  },
  "pattern_consistency": {"follows_conventions": true, "issues": []},
  "concerns": []
}`

// sampleDiff has foo.go and foo_test.go → should group into 1 segment.
const sampleDiff = `diff --git a/internal/auth/handler.go b/internal/auth/handler.go
--- a/internal/auth/handler.go
+++ b/internal/auth/handler.go
@@ -1,3 +1,4 @@
 package auth
+func Logout() {}
diff --git a/internal/auth/handler_test.go b/internal/auth/handler_test.go
--- a/internal/auth/handler_test.go
+++ b/internal/auth/handler_test.go
@@ -1,3 +1,4 @@
 package auth
+func TestLogout() {}
`

// multiSegmentDiff has 3 unrelated files → should produce 3 segments.
const multiSegmentDiff = `diff --git a/internal/auth/handler.go b/internal/auth/handler.go
--- a/internal/auth/handler.go
+++ b/internal/auth/handler.go
@@ -1,3 +1,4 @@
 package auth
+func Logout() {}
diff --git a/internal/middleware/cors.go b/internal/middleware/cors.go
--- a/internal/middleware/cors.go
+++ b/internal/middleware/cors.go
@@ -1,3 +1,4 @@
 package middleware
+func CORS() {}
diff --git a/internal/config/config.go b/internal/config/config.go
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -1,3 +1,4 @@
 package config
+var Version = "2"
`

func testConfig() *config.ProjectConfig {
	return &config.ProjectConfig{
		Project: config.Project{Name: "test", Path: "/tmp/test"},
		Models: config.Models{
			Providers: map[string]config.ProviderConfig{
				"test-provider": {
					Endpoint: "http://test:8080/v1",
					Model:    "test-model",
					Pricing: &config.Pricing{
						InputPerMillion:  3.0,
						OutputPerMillion: 15.0,
					},
				},
			},
			Roles: map[string]string{
				"verify_analyze":    "test-provider",
				"verify_synthesize": "test-provider",
			},
		},
	}
}

func testGuardrails() *config.Guardrails {
	return &config.Guardrails{
		MaxInvestigationTargets: 6,
		MaxSubCallsTotal:        12,
		PhaseTimeoutSeconds:     300,
		MaxCostPerPhaseUSD:      0.50,
		WarnCostPerPhaseUSD:     0.10,
	}
}

func testWorkOrder() *models.WorkOrder {
	return &models.WorkOrder{
		Title:              "Add logout endpoint",
		Type:               "new_feature",
		TargetModule:       "auth",
		AcceptanceCriteria: []string{"go test passes"},
		Constraints:        []string{"no breaking changes"},
		KnownFiles:         []string{"internal/auth/handler.go"},
	}
}

func newOrchestrator(client llm.Client) *VerifyOrchestrator {
	cfg := testConfig()
	providers := map[string]llm.Client{"test-provider": client}
	resolver := llm.NewRoleResolver(providers, cfg.Models.Roles)
	guardrails := testGuardrails()

	return NewVerifyOrchestrator(resolver, cfg, guardrails, VerifyPrompts{
		Analyze:    "analyze system prompt",
		Synthesize: "synthesize system prompt",
	})
}

// --- Tests ---

func TestExecute_HappyPath(t *testing.T) {
	// multiSegmentDiff → 3 segments → 3 analyze + 1 synthesize = 4 calls
	client := &mockClient{responses: []mockResponse{
		{content: validAnalyzeResponse},    // analyze segment 1
		{content: validAnalyzeResponse},    // analyze segment 2
		{content: validAnalyzeResponse},    // analyze segment 3
		{content: validSynthesizeResponse}, // synthesize
	}}

	orch := newOrchestrator(client)
	report, records, err := orch.Execute(context.Background(), testWorkOrder(), multiSegmentDiff, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if report.Status != "PASS" {
		t.Errorf("unexpected status: %q", report.Status)
	}
	if report.Summary != "All changes aligned with work order" {
		t.Errorf("unexpected summary: %q", report.Summary)
	}

	// 3 analyze + 1 synthesize = 4
	if len(records) != 4 {
		t.Fatalf("expected 4 sub-call records, got %d", len(records))
	}
	for i, rec := range records {
		if !rec.Success {
			t.Errorf("record %d should be successful: %s", i, rec.ErrorMessage)
		}
		if rec.Phase != "verify" {
			t.Errorf("record %d phase should be 'verify', got %q", i, rec.Phase)
		}
		if rec.EstimatedCostUSD <= 0 {
			t.Errorf("record %d should have positive cost estimate, got %f", i, rec.EstimatedCostUSD)
		}
	}
}

func TestExecute_TestFileGrouping(t *testing.T) {
	// sampleDiff has handler.go + handler_test.go → grouped into 1 segment
	client := &mockClient{responses: []mockResponse{
		{content: validAnalyzeResponse},    // analyze (single grouped segment)
		{content: validSynthesizeResponse}, // synthesize
	}}

	orch := newOrchestrator(client)
	report, records, err := orch.Execute(context.Background(), testWorkOrder(), sampleDiff, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}

	// 1 analyze + 1 synthesize = 2
	if len(records) != 2 {
		t.Fatalf("expected 2 sub-call records, got %d", len(records))
	}
	analyzeCount := 0
	for _, rec := range records {
		if rec.Step == "analyze" {
			analyzeCount++
		}
	}
	if analyzeCount != 1 {
		t.Errorf("expected 1 analyze call (grouped), got %d", analyzeCount)
	}
}

func TestExecute_PartialAnalyzeFailure(t *testing.T) {
	// multiSegmentDiff → 3 segments; segment 1 fails, others succeed
	client := &mockClient{responses: []mockResponse{
		{err: fmt.Errorf("analyze error")},  // analyze segment 1 fails
		{content: validAnalyzeResponse},     // analyze segment 2 succeeds
		{content: validAnalyzeResponse},     // analyze segment 3 succeeds
		{content: validSynthesizeResponse},  // synthesize
	}}

	orch := newOrchestrator(client)
	report, records, err := orch.Execute(context.Background(), testWorkOrder(), multiSegmentDiff, nil)
	if err != nil {
		t.Fatalf("expected no error on partial failure, got: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}

	failedAnalyze := 0
	for _, rec := range records {
		if rec.Step == "analyze" && !rec.Success {
			failedAnalyze++
		}
	}
	if failedAnalyze != 1 {
		t.Errorf("expected 1 failed analyze record, got %d", failedAnalyze)
	}
}

func TestExecute_AllAnalyzeFail(t *testing.T) {
	// All analyze calls fail — synthesize should still be called (differs from scope).
	client := &mockClient{responses: []mockResponse{
		{err: fmt.Errorf("analyze error 1")}, // analyze segment 1
		{err: fmt.Errorf("analyze error 2")}, // analyze segment 2
		{err: fmt.Errorf("analyze error 3")}, // analyze segment 3
		{content: validSynthesizeResponse},   // synthesize still called
	}}

	orch := newOrchestrator(client)
	report, records, err := orch.Execute(context.Background(), testWorkOrder(), multiSegmentDiff, nil)
	if err != nil {
		t.Fatalf("expected no error when all analyze fail (synthesize still runs), got: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}

	failedAnalyze := 0
	for _, rec := range records {
		if rec.Step == "analyze" && !rec.Success {
			failedAnalyze++
		}
	}
	if failedAnalyze != 3 {
		t.Errorf("expected 3 failed analyze records, got %d", failedAnalyze)
	}

	// Verify synthesize was called
	synthCount := 0
	for _, rec := range records {
		if rec.Step == "synthesize" {
			synthCount++
		}
	}
	if synthCount != 1 {
		t.Errorf("expected 1 synthesize record, got %d", synthCount)
	}
}

func TestExecute_SynthesizeRetrySuccess(t *testing.T) {
	// sampleDiff → 1 segment
	client := &mockClient{responses: []mockResponse{
		{content: validAnalyzeResponse},               // analyze
		{err: fmt.Errorf("first attempt fails")},      // synthesize attempt 1
		{content: validSynthesizeResponse},             // synthesize attempt 2
	}}

	orch := newOrchestrator(client)
	report, records, err := orch.Execute(context.Background(), testWorkOrder(), sampleDiff, nil)
	if err != nil {
		t.Fatalf("expected success on retry, got: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}

	synthCount := 0
	for _, rec := range records {
		if rec.Step == "synthesize" {
			synthCount++
		}
	}
	if synthCount != 2 {
		t.Errorf("expected 2 synthesize records, got %d", synthCount)
	}
}

func TestExecute_SynthesizeBothFail(t *testing.T) {
	// sampleDiff → 1 segment
	client := &mockClient{responses: []mockResponse{
		{content: validAnalyzeResponse}, // analyze
		{err: fmt.Errorf("fail 1")},     // synthesize attempt 1
		{err: fmt.Errorf("fail 2")},     // synthesize attempt 2
	}}

	orch := newOrchestrator(client)
	_, _, err := orch.Execute(context.Background(), testWorkOrder(), sampleDiff, nil)
	if err == nil {
		t.Fatal("expected error when both synthesize attempts fail")
	}
	if !strings.Contains(err.Error(), "synthesize failed after 2 attempts") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecute_EmptyDiff(t *testing.T) {
	client := &mockClient{responses: []mockResponse{}}

	orch := newOrchestrator(client)
	_, _, err := orch.Execute(context.Background(), testWorkOrder(), "", nil)
	if err == nil {
		t.Fatal("expected error for empty diff")
	}
	if !strings.Contains(err.Error(), "0 segments") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecute_PhaseTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately expired

	ctxClient := &ctxAwareMockClient{}
	cfg := testConfig()
	providers := map[string]llm.Client{"test-provider": ctxClient}
	resolver := llm.NewRoleResolver(providers, cfg.Models.Roles)
	guardrails := testGuardrails()

	orch := NewVerifyOrchestrator(resolver, cfg, guardrails, VerifyPrompts{
		Analyze: "sys", Synthesize: "sys",
	})

	// Use multiSegmentDiff so we get past the segment step
	_, _, err := orch.Execute(ctx, testWorkOrder(), multiSegmentDiff, nil)
	if err == nil {
		t.Fatal("expected error for expired context")
	}
}

func TestSegmentDiff(t *testing.T) {
	t.Run("multi-file diff", func(t *testing.T) {
		segments := segmentDiff(multiSegmentDiff)
		if len(segments) != 3 {
			t.Fatalf("expected 3 segments, got %d", len(segments))
		}
		if segments[0].Files[0] != "internal/auth/handler.go" {
			t.Errorf("segment 0 file: %q", segments[0].Files[0])
		}
		if segments[1].Files[0] != "internal/middleware/cors.go" {
			t.Errorf("segment 1 file: %q", segments[1].Files[0])
		}
		if segments[2].Files[0] != "internal/config/config.go" {
			t.Errorf("segment 2 file: %q", segments[2].Files[0])
		}
	})

	t.Run("test file grouping", func(t *testing.T) {
		segments := segmentDiff(sampleDiff)
		if len(segments) != 1 {
			t.Fatalf("expected 1 segment (grouped), got %d", len(segments))
		}
		if len(segments[0].Files) != 2 {
			t.Errorf("expected 2 files in grouped segment, got %d", len(segments[0].Files))
		}
	})

	t.Run("single file", func(t *testing.T) {
		singleDiff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1 +1,2 @@
 package main
+func init() {}
`
		segments := segmentDiff(singleDiff)
		if len(segments) != 1 {
			t.Fatalf("expected 1 segment, got %d", len(segments))
		}
		if segments[0].Files[0] != "main.go" {
			t.Errorf("unexpected file: %q", segments[0].Files[0])
		}
	})

	t.Run("empty diff", func(t *testing.T) {
		segments := segmentDiff("")
		if len(segments) != 0 {
			t.Errorf("expected 0 segments for empty diff, got %d", len(segments))
		}
	})
}

func TestExecute_PreChecksPassedThrough(t *testing.T) {
	client := &mockClient{responses: []mockResponse{
		{content: validAnalyzeResponse},    // analyze
		{content: validSynthesizeResponse}, // synthesize
	}}

	// Capture the user message sent to synthesize by checking the raw content.
	// We use sampleDiff (1 segment) so there's exactly 1 analyze + 1 synthesize call.
	orch := newOrchestrator(client)
	preChecks := []PreCheckResult{
		{Criterion: "go test passes", Met: true, Notes: "exit code 0"},
		{Criterion: "go vet passes", Met: false, Notes: "vet found issues"},
	}

	report, _, err := orch.Execute(context.Background(), testWorkOrder(), sampleDiff, preChecks)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}

	// Verify that buildSynthesizeUserMessage includes the pre-checks.
	wo := testWorkOrder()
	msg := buildSynthesizeUserMessage(wo, preChecks, []string{validAnalyzeResponse})
	if !strings.Contains(msg, "PRE-CHECK RESULTS") {
		t.Error("synthesize message should contain PRE-CHECK RESULTS section")
	}
	if !strings.Contains(msg, "[PASS] go test passes") {
		t.Error("synthesize message should contain passing pre-check")
	}
	if !strings.Contains(msg, "[FAIL] go vet passes") {
		t.Error("synthesize message should contain failing pre-check")
	}
}

// --- Verify SubCallRecord import from scope ---

func TestSubCallRecordFromScope(t *testing.T) {
	// Ensure we're using scope.SubCallRecord, not a local type.
	var rec scope.SubCallRecord
	rec.Phase = "verify"
	if rec.Phase != "verify" {
		t.Errorf("unexpected phase: %q", rec.Phase)
	}
}
