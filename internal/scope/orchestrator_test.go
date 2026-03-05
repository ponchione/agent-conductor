package scope

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appcontext "github.com/ponchione/agent-conductor/internal/context"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
)

// --- Mock types ---

// mockResponse is a single canned response for mockClient.
type mockResponse struct {
	content string
	err     error
}

// mockClient implements llm.Client, returning responses in order.
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

// mockGatherer implements ContextGatherer.
type mockGatherer struct {
	preScope *appcontext.PreScopeBundle
	bundles  map[string]*appcontext.TargetBundle
}

func (m *mockGatherer) GatherPreScope(_ context.Context, _ *models.WorkOrder) (*appcontext.PreScopeBundle, error) {
	return m.preScope, nil
}

func (m *mockGatherer) GatherForTarget(_ context.Context, _ *models.WorkOrder, target appcontext.Target) (*appcontext.TargetBundle, error) {
	if b, ok := m.bundles[target.Path]; ok {
		return b, nil
	}
	return &appcontext.TargetBundle{}, nil
}

// --- Fixtures ---

const validDecomposeResponse = `[
  {"path": "internal/auth", "rationale": "auth changes needed", "investigation_type": "primary_modification"},
  {"path": "internal/middleware", "rationale": "middleware updates", "investigation_type": "supporting_modification"}
]`

const validAnalyzeResponse = `{
  "target": "internal/auth",
  "files_to_modify": [{"path": "internal/auth/handler.go", "reason": "add endpoint"}],
  "files_to_reference": [{"path": "internal/auth/service.go", "reason": "pattern"}],
  "new_files": [],
  "interfaces_to_preserve": ["AuthService"],
  "concerns": ["backwards compat"],
  "analysis_failed": false
}`

const validCrosscutResponse = `{
  "shared_types": [{"name": "User", "package": "models", "used_by": ["auth", "middleware"]}],
  "dependencies": [{"from": "middleware", "to": "auth", "type": "imports"}],
  "integration_risks": ["token format change"],
  "suggested_order": ["auth", "middleware"]
}`

const validSynthesizeResponse = `{
  "summary": "Add logout endpoint to auth module",
  "estimated_complexity": "medium",
  "files_to_modify": [{"path": "internal/auth/handler.go", "reason": "add logout route"}],
  "files_to_reference": [{"path": "internal/auth/service.go", "reason": "existing patterns"}],
  "sql_files": [],
  "new_files": [],
  "dependencies": ["auth"],
  "build_instructions": "go build ./..."
}`

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
				"decompose":  "test-provider",
				"analyze":    "test-provider",
				"crosscut":   "test-provider",
				"synthesize": "test-provider",
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

func testGatherer() *mockGatherer {
	return &mockGatherer{
		preScope: &appcontext.PreScopeBundle{
			FileTree:    "internal/\n  auth/\n    handler.go\n",
			Conventions: "Module path: github.com/example/project\n",
		},
		bundles: map[string]*appcontext.TargetBundle{
			"internal/auth": {
				Files: []appcontext.FileContent{
					{Path: "internal/auth/handler.go", Content: "package auth\n"},
				},
				Signatures: []string{"func Login(user string) error"},
			},
			"internal/middleware": {
				Files: []appcontext.FileContent{
					{Path: "internal/middleware/auth.go", Content: "package middleware\n"},
				},
			},
		},
	}
}

func newOrchestrator(client *mockClient) *ScopeOrchestrator {
	cfg := testConfig()
	providers := map[string]llm.Client{"test-provider": client}
	resolver := llm.NewRoleResolver(providers, cfg.Models.Roles)
	guardrails := testGuardrails()

	return NewScopeOrchestrator(resolver, testGatherer(), cfg, guardrails, ScopePrompts{
		Decompose:  "decompose system prompt",
		Analyze:    "analyze system prompt",
		Crosscut:   "crosscut system prompt",
		Synthesize: "synthesize system prompt",
	})
}

// --- Tests ---

func TestExecute_HappyPath(t *testing.T) {
	client := &mockClient{responses: []mockResponse{
		{content: validDecomposeResponse},   // decompose
		{content: validAnalyzeResponse},     // analyze target 1
		{content: validAnalyzeResponse},     // analyze target 2
		{content: validCrosscutResponse},    // crosscut
		{content: validSynthesizeResponse},  // synthesize
	}}

	orch := newOrchestrator(client)
	pkg, records, err := orch.Execute(context.Background(), testWorkOrder())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if pkg.Summary != "Add logout endpoint to auth module" {
		t.Errorf("unexpected summary: %q", pkg.Summary)
	}
	if pkg.EstimatedComplexity != "medium" {
		t.Errorf("unexpected complexity: %q", pkg.EstimatedComplexity)
	}
	if len(pkg.FilesToModify) != 1 {
		t.Errorf("expected 1 file to modify, got %d", len(pkg.FilesToModify))
	}

	// 1 decompose + 2 analyze + 1 crosscut + 1 synthesize = 5
	if len(records) != 5 {
		t.Fatalf("expected 5 sub-call records, got %d", len(records))
	}
	for i, rec := range records {
		if !rec.Success {
			t.Errorf("record %d should be successful: %s", i, rec.ErrorMessage)
		}
		if rec.EstimatedCostUSD <= 0 {
			t.Errorf("record %d should have positive cost estimate, got %f", i, rec.EstimatedCostUSD)
		}
	}
}

func TestExecute_TargetsTruncated(t *testing.T) {
	// 8 targets, max 3
	eightTargets := `[
		{"path":"a","rationale":"r","investigation_type":"primary_modification"},
		{"path":"b","rationale":"r","investigation_type":"primary_modification"},
		{"path":"c","rationale":"r","investigation_type":"primary_modification"},
		{"path":"d","rationale":"r","investigation_type":"primary_modification"},
		{"path":"e","rationale":"r","investigation_type":"primary_modification"},
		{"path":"f","rationale":"r","investigation_type":"primary_modification"},
		{"path":"g","rationale":"r","investigation_type":"primary_modification"},
		{"path":"h","rationale":"r","investigation_type":"primary_modification"}
	]`

	client := &mockClient{responses: []mockResponse{
		{content: eightTargets},             // decompose
		{content: validAnalyzeResponse},     // analyze 1
		{content: validAnalyzeResponse},     // analyze 2
		{content: validAnalyzeResponse},     // analyze 3
		{content: validCrosscutResponse},    // crosscut
		{content: validSynthesizeResponse},  // synthesize
	}}

	orch := newOrchestrator(client)
	orch.guardrails.MaxInvestigationTargets = 3

	_, records, err := orch.Execute(context.Background(), testWorkOrder())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Count analyze records
	analyzeCount := 0
	for _, rec := range records {
		if rec.Step == "analyze" {
			analyzeCount++
		}
	}
	if analyzeCount != 3 {
		t.Errorf("expected 3 analyze calls after truncation, got %d", analyzeCount)
	}
}

func TestExecute_DecomposeZeroTargets(t *testing.T) {
	client := &mockClient{responses: []mockResponse{
		{content: "[]"},
	}}

	orch := newOrchestrator(client)
	_, records, err := orch.Execute(context.Background(), testWorkOrder())
	if err == nil {
		t.Fatal("expected error for zero targets")
	}
	if !strings.Contains(err.Error(), "zero investigation targets") {
		t.Errorf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record, got %d", len(records))
	}
}

func TestExecute_DecomposeCallFailure(t *testing.T) {
	client := &mockClient{responses: []mockResponse{
		{err: fmt.Errorf("connection refused")},
	}}

	orch := newOrchestrator(client)
	_, records, err := orch.Execute(context.Background(), testWorkOrder())
	if err == nil {
		t.Fatal("expected error for decompose failure")
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Success {
		t.Error("record should not be successful")
	}
	if records[0].ErrorMessage != "connection refused" {
		t.Errorf("unexpected error message: %q", records[0].ErrorMessage)
	}
}

func TestExecute_AllAnalyzeFail(t *testing.T) {
	client := &mockClient{responses: []mockResponse{
		{content: validDecomposeResponse},                  // decompose
		{err: fmt.Errorf("analyze error 1")},               // analyze 1
		{err: fmt.Errorf("analyze error 2")},               // analyze 2
	}}

	orch := newOrchestrator(client)
	_, _, err := orch.Execute(context.Background(), testWorkOrder())
	if err == nil {
		t.Fatal("expected error when all analyze calls fail")
	}
	if !strings.Contains(err.Error(), "all 2 analyze calls failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecute_PartialAnalyzeFailure(t *testing.T) {
	client := &mockClient{responses: []mockResponse{
		{content: validDecomposeResponse},   // decompose
		{err: fmt.Errorf("analyze error")},  // analyze 1 fails
		{content: validAnalyzeResponse},     // analyze 2 succeeds
		{content: validCrosscutResponse},    // crosscut
		{content: validSynthesizeResponse},  // synthesize
	}}

	orch := newOrchestrator(client)
	pkg, records, err := orch.Execute(context.Background(), testWorkOrder())
	if err != nil {
		t.Fatalf("expected no error on partial failure, got: %v", err)
	}
	if pkg == nil {
		t.Fatal("expected non-nil package")
	}

	// Check that the failed analyze is recorded
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

func TestExecute_CrosscutFailure_NonFatal(t *testing.T) {
	client := &mockClient{responses: []mockResponse{
		{content: validDecomposeResponse},      // decompose
		{content: validAnalyzeResponse},        // analyze 1
		{content: validAnalyzeResponse},        // analyze 2
		{err: fmt.Errorf("crosscut timeout")},  // crosscut fails
		{content: validSynthesizeResponse},     // synthesize still called
	}}

	orch := newOrchestrator(client)
	pkg, _, err := orch.Execute(context.Background(), testWorkOrder())
	if err != nil {
		t.Fatalf("expected no error on crosscut failure, got: %v", err)
	}
	if pkg == nil {
		t.Fatal("expected non-nil package")
	}
	if pkg.Summary != "Add logout endpoint to auth module" {
		t.Errorf("unexpected summary: %q", pkg.Summary)
	}
}

func TestExecute_SynthesizeRetrySuccess(t *testing.T) {
	client := &mockClient{responses: []mockResponse{
		{content: validDecomposeResponse},   // decompose
		{content: validAnalyzeResponse},     // analyze 1
		{content: validAnalyzeResponse},     // analyze 2
		{content: validCrosscutResponse},    // crosscut
		{err: fmt.Errorf("first attempt fails")},  // synthesize attempt 1
		{content: validSynthesizeResponse},         // synthesize attempt 2
	}}

	orch := newOrchestrator(client)
	pkg, records, err := orch.Execute(context.Background(), testWorkOrder())
	if err != nil {
		t.Fatalf("expected success on retry, got: %v", err)
	}
	if pkg == nil {
		t.Fatal("expected non-nil package")
	}

	// Count synthesize records — should be 2 (1 failed + 1 success)
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
	client := &mockClient{responses: []mockResponse{
		{content: validDecomposeResponse},              // decompose
		{content: validAnalyzeResponse},                // analyze 1
		{content: validAnalyzeResponse},                // analyze 2
		{content: validCrosscutResponse},               // crosscut
		{err: fmt.Errorf("fail 1")},                    // synthesize attempt 1
		{err: fmt.Errorf("fail 2")},                    // synthesize attempt 2
	}}

	orch := newOrchestrator(client)
	_, _, err := orch.Execute(context.Background(), testWorkOrder())
	if err == nil {
		t.Fatal("expected error when both synthesize attempts fail")
	}
	if !strings.Contains(err.Error(), "synthesize failed after 2 attempts") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecute_PhaseTimeout(t *testing.T) {
	// Use a context-aware mock that checks for cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately expired

	ctxClient := &ctxAwareMockClient{}

	cfg := testConfig()
	providers := map[string]llm.Client{"test-provider": ctxClient}
	resolver := llm.NewRoleResolver(providers, cfg.Models.Roles)
	guardrails := testGuardrails()

	orch := NewScopeOrchestrator(resolver, testGatherer(), cfg, guardrails, ScopePrompts{
		Decompose: "sys", Analyze: "sys", Crosscut: "sys", Synthesize: "sys",
	})

	_, _, err := orch.Execute(ctx, testWorkOrder())
	if err == nil {
		t.Fatal("expected error for expired context")
	}
}

// ctxAwareMockClient returns context.Err() if the context is done.
type ctxAwareMockClient struct{}

func (c *ctxAwareMockClient) Complete(ctx context.Context, _ string, _ string) (*llm.CompletionResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return &llm.CompletionResult{Content: "ok"}, nil
}

func TestCleanJSONArray(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "clean array",
			input: `[{"a":1}]`,
			want:  `[{"a":1}]`,
		},
		{
			name:  "with preamble",
			input: "Here are the targets:\n```json\n[{\"a\":1}]\n```",
			want:  `[{"a":1}]`,
		},
		{
			name:  "no brackets",
			input: "no json here",
			want:  "no json here",
		},
		{
			name:  "empty array",
			input: "[]",
			want:  "[]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanJSONArray(tt.input)
			if got != tt.want {
				t.Errorf("cleanJSONArray(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
