package context

import (
	"context"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/models"
)

// stubSearcher is a test double for RAGSearcher (basic, no work order awareness).
type stubSearcher struct {
	results []RAGResult
}

func (s *stubSearcher) SearchStructured(_ context.Context, _ string, _ int) ([]RAGResult, error) {
	return s.results, nil
}

// enrichedStubSearcher implements both RAGSearcher and RAGWorkOrderSearcher.
type enrichedStubSearcher struct {
	basicResults    []RAGResult
	enrichedResults []EnrichedRAGResult
}

func (s *enrichedStubSearcher) SearchStructured(_ context.Context, _ string, _ int) ([]RAGResult, error) {
	return s.basicResults, nil
}

func (s *enrichedStubSearcher) SearchForWorkOrder(_ context.Context, _ *models.WorkOrder, _ int) ([]EnrichedRAGResult, error) {
	return s.enrichedResults, nil
}

func TestAssemble_FullContextPackage(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: "/tmp/test", DataDir: "/tmp/test/data"},
	}

	searcher := &stubSearcher{results: []RAGResult{
		{Function: "Login", File: "internal/auth/service.go", Description: "Handles user login"},
	}}
	assembler := NewAssembler(cfg, searcher)

	wo := &models.WorkOrder{
		Title:              "Add logout endpoint",
		Type:               "new_feature",
		TargetModule:       "auth",
		ReferenceModule:    "users",
		AcceptanceCriteria: []string{"go test passes"},
		Constraints:        []string{"no breaking changes"},
		KnownFiles:         []string{"internal/auth/handler.go"},
	}

	scopePkg := &models.ContextPackage{
		Summary:             "Add logout handler",
		EstimatedComplexity: "low",
		FilesToModify:       []models.FileRef{{Path: "internal/auth/handler.go", Reason: "add route"}},
		FilesToReference:    []models.FileRef{{Path: "internal/auth/service.go", Reason: "pattern reference"}},
	}

	full, err := assembler.Assemble(context.Background(), wo, scopePkg, "feature/conducted-abc12345")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}

	// Work order fields
	if full.WorkOrder.Title != "Add logout endpoint" {
		t.Errorf("expected title 'Add logout endpoint', got %q", full.WorkOrder.Title)
	}
	if full.WorkOrder.TargetModule != "auth" {
		t.Errorf("expected target module 'auth', got %q", full.WorkOrder.TargetModule)
	}
	if full.WorkOrder.ReferenceModule != "users" {
		t.Errorf("expected reference module 'users', got %q", full.WorkOrder.ReferenceModule)
	}

	// Scope fields
	if full.Scope.Summary != "Add logout handler" {
		t.Errorf("expected summary 'Add logout handler', got %q", full.Scope.Summary)
	}
	if len(full.Scope.FilesToModify) != 1 {
		t.Errorf("expected 1 file to modify, got %d", len(full.Scope.FilesToModify))
	}

	// RAG results mapped to RelevantCode
	if len(full.Scope.RelevantCode) != 1 {
		t.Fatalf("expected 1 relevant code entry, got %d", len(full.Scope.RelevantCode))
	}
	if full.Scope.RelevantCode[0].Function != "Login" {
		t.Errorf("expected function 'Login', got %q", full.Scope.RelevantCode[0].Function)
	}

	// Directives
	if full.Directives.BranchName != "feature/conducted-abc12345" {
		t.Errorf("expected branch 'feature/conducted-abc12345', got %q", full.Directives.BranchName)
	}
	if full.Directives.ReferenceModuleNote == "" {
		t.Error("expected reference module note to be set when ReferenceModule is non-empty")
	}
}

func TestAssemble_NilSearcher(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: "/tmp/test", DataDir: "/tmp/test/data"},
	}
	assembler := NewAssembler(cfg, nil)

	wo := &models.WorkOrder{
		Title:        "Test feature",
		Type:         "new_feature",
		TargetModule: "auth",
	}
	scopePkg := &models.ContextPackage{
		Summary:             "test",
		EstimatedComplexity: "low",
	}

	full, err := assembler.Assemble(context.Background(), wo, scopePkg, "feature/test-branch")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}

	if len(full.Scope.RelevantCode) != 0 {
		t.Errorf("expected empty RelevantCode with nil searcher, got %d entries", len(full.Scope.RelevantCode))
	}
}

func TestAssemble_NoReferenceModuleNote(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: "/tmp/test", DataDir: "/tmp/test/data"},
	}
	assembler := NewAssembler(cfg, nil)

	wo := &models.WorkOrder{
		Title:        "Test feature",
		Type:         "new_feature",
		TargetModule: "auth",
		// ReferenceModule intentionally empty
	}
	scopePkg := &models.ContextPackage{
		Summary:             "test",
		EstimatedComplexity: "low",
	}

	full, err := assembler.Assemble(context.Background(), wo, scopePkg, "feature/test-branch")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}

	if full.Directives.ReferenceModuleNote != "" {
		t.Errorf("expected empty reference module note, got %q", full.Directives.ReferenceModuleNote)
	}
}

func TestAssemble_WithEnrichedSearcher(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: "/tmp/test", DataDir: "/tmp/test/data"},
		Index:   config.Index{MaxRAGResults: 30},
	}

	searcher := &enrichedStubSearcher{
		enrichedResults: []EnrichedRAGResult{
			{
				Function:    "Login",
				File:        "internal/auth/service.go",
				Description: "Handles user login",
				Calls: []models.CodeRef{
					{Name: "ValidateToken", Package: "auth"},
				},
				CalledBy: []models.CodeRef{
					{Name: "HandleLogin", Package: "handler"},
				},
				QueryHitCount: 3,
				Score:         0.85,
			},
			{
				Function:        "ValidateToken",
				File:            "internal/auth/token.go",
				Description:     "Validates JWT token",
				IsDependencyHop: true,
				Score:           0,
			},
		},
	}
	assembler := NewAssembler(cfg, searcher)

	wo := &models.WorkOrder{
		Title:        "Add logout endpoint",
		Type:         "new_feature",
		TargetModule: "auth",
	}
	scopePkg := &models.ContextPackage{
		Summary:             "Add logout handler",
		EstimatedComplexity: "low",
	}

	full, err := assembler.Assemble(context.Background(), wo, scopePkg, "feature/test")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}

	if len(full.Scope.RelevantCode) != 2 {
		t.Fatalf("expected 2 relevant code entries, got %d", len(full.Scope.RelevantCode))
	}

	// First result: direct hit with relationships
	rc := full.Scope.RelevantCode[0]
	if rc.Function != "Login" {
		t.Errorf("expected function 'Login', got %q", rc.Function)
	}
	if len(rc.Calls) != 1 || rc.Calls[0].Name != "ValidateToken" {
		t.Errorf("expected Calls to contain ValidateToken, got %v", rc.Calls)
	}
	if len(rc.CalledBy) != 1 || rc.CalledBy[0].Name != "HandleLogin" {
		t.Errorf("expected CalledBy to contain HandleLogin, got %v", rc.CalledBy)
	}
	if rc.QueryHitCount != 3 {
		t.Errorf("expected QueryHitCount 3, got %d", rc.QueryHitCount)
	}
	if rc.IsDependencyHop {
		t.Error("first result should not be a dependency hop")
	}

	// Second result: dependency hop
	rc2 := full.Scope.RelevantCode[1]
	if !rc2.IsDependencyHop {
		t.Error("second result should be a dependency hop")
	}
	if rc2.Function != "ValidateToken" {
		t.Errorf("expected function 'ValidateToken', got %q", rc2.Function)
	}
}

func TestAssemble_FallbackToBasicSearcher(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: "/tmp/test", DataDir: "/tmp/test/data"},
	}

	// Use plain stubSearcher — only implements RAGSearcher, not RAGWorkOrderSearcher
	searcher := &stubSearcher{results: []RAGResult{
		{Function: "Login", File: "internal/auth/service.go", Description: "Handles login"},
	}}
	assembler := NewAssembler(cfg, searcher)

	wo := &models.WorkOrder{
		Title:        "Add logout",
		Type:         "new_feature",
		TargetModule: "auth",
	}
	scopePkg := &models.ContextPackage{
		Summary:             "test",
		EstimatedComplexity: "low",
	}

	full, err := assembler.Assemble(context.Background(), wo, scopePkg, "feature/test")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}

	if len(full.Scope.RelevantCode) != 1 {
		t.Fatalf("expected 1 relevant code entry from fallback, got %d", len(full.Scope.RelevantCode))
	}
	if full.Scope.RelevantCode[0].Function != "Login" {
		t.Errorf("expected function 'Login', got %q", full.Scope.RelevantCode[0].Function)
	}
	// Basic searcher should not populate enriched fields
	if full.Scope.RelevantCode[0].IsDependencyHop {
		t.Error("basic searcher result should not be dependency hop")
	}
}

func TestAssembleScopePrompt_DependencyHopAnnotation(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: "/tmp/test", DataDir: "/tmp/test/data"},
		Index:   config.Index{MaxRAGResults: 30},
	}

	searcher := &enrichedStubSearcher{
		enrichedResults: []EnrichedRAGResult{
			{
				Function:    "Login",
				File:        "internal/auth/service.go",
				Description: "Handles user login",
			},
			{
				Function:        "ValidateToken",
				File:            "internal/auth/token.go",
				Description:     "Validates JWT token",
				IsDependencyHop: true,
			},
		},
	}
	assembler := NewAssembler(cfg, searcher)

	wo := &models.WorkOrder{
		Title:        "Add logout endpoint",
		Type:         "new_feature",
		TargetModule: "auth",
	}

	prompt, err := assembler.AssembleScopePrompt(context.Background(), wo)
	if err != nil {
		t.Fatalf("AssembleScopePrompt failed: %v", err)
	}

	if !strings.Contains(prompt, "[dependency-hop]") {
		t.Error("expected prompt to contain [dependency-hop] marker")
	}
	// Non-hop entry should not have the marker
	if strings.Contains(prompt, "Login [dependency-hop]") {
		t.Error("Login should not have dependency-hop marker")
	}
	if !strings.Contains(prompt, "ValidateToken [dependency-hop]") {
		t.Error("ValidateToken should have dependency-hop marker")
	}
}
