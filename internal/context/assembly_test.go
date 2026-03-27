package context

import (
	"context"
	"os"
	"path/filepath"
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
	assembler := NewAssembler(cfg, searcher, nil)

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
	if full.WorkOrder.SchemaVersion != models.WorkOrderSchemaVersion {
		t.Errorf("expected schema version %d, got %d", models.WorkOrderSchemaVersion, full.WorkOrder.SchemaVersion)
	}
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
	assembler := NewAssembler(cfg, nil, nil)

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
	assembler := NewAssembler(cfg, nil, nil)

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
				Body:        "func Login() { ... }",
				Signature:   "func Login() error",
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
	assembler := NewAssembler(cfg, searcher, nil)

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
	if rc.Body != "func Login() { ... }" {
		t.Errorf("expected Body 'func Login() { ... }', got %q", rc.Body)
	}
	if rc.Signature != "func Login() error" {
		t.Errorf("expected Signature 'func Login() error', got %q", rc.Signature)
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
	assembler := NewAssembler(cfg, searcher, nil)

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
	assembler := NewAssembler(cfg, searcher, nil)

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

func TestGatherPreScope_ReturnsFileTreeAndConventions(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a Go file so the tree is non-empty.
	if err := os.MkdirAll(filepath.Join(tmpDir, "internal", "auth"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "internal", "auth", "handler.go"), []byte("package auth\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: tmpDir, DataDir: t.TempDir()},
		Index: config.Index{
			Include: []string{"**/*.go"},
		},
		Conventions: config.Conventions{
			ModulePath: "github.com/example/project",
		},
	}
	assembler := NewAssembler(cfg, nil, nil)

	bundle, err := assembler.GatherPreScope(context.Background(), &models.WorkOrder{
		Title: "test", Type: "new_feature", TargetModule: "auth",
	})
	if err != nil {
		t.Fatalf("GatherPreScope failed: %v", err)
	}

	if bundle.FileTree == "" {
		t.Error("expected non-empty FileTree")
	}
	if !strings.Contains(bundle.Conventions, "github.com/example/project") {
		t.Errorf("expected conventions to contain module path, got %q", bundle.Conventions)
	}
	if bundle.RecallSummary != "" {
		t.Errorf("expected empty RecallSummary, got %q", bundle.RecallSummary)
	}
}

func TestGatherPreScope_EmptyConventions(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: t.TempDir(), DataDir: t.TempDir()},
	}
	assembler := NewAssembler(cfg, nil, nil)

	bundle, err := assembler.GatherPreScope(context.Background(), &models.WorkOrder{
		Title: "test", Type: "new_feature", TargetModule: "auth",
	})
	if err != nil {
		t.Fatalf("GatherPreScope failed: %v", err)
	}
	if bundle.Conventions != "" {
		t.Errorf("expected empty Conventions, got %q", bundle.Conventions)
	}
}

func TestGatherForTarget_RAGFiltering(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: t.TempDir(), DataDir: t.TempDir()},
	}

	searcher := &stubSearcher{results: []RAGResult{
		{Function: "Login", File: "internal/auth/service.go", Description: "handles login"},
		{Function: "ListUsers", File: "internal/users/service.go", Description: "lists users"},
		{Function: "Middleware", File: "internal/auth/middleware.go", Description: "auth middleware"},
	}}
	assembler := NewAssembler(cfg, searcher, nil)

	wo := &models.WorkOrder{Title: "test", Type: "new_feature", TargetModule: "auth"}
	target := Target{Path: "internal/auth", Rationale: "auth changes"}

	bundle, err := assembler.GatherForTarget(context.Background(), wo, target)
	if err != nil {
		t.Fatalf("GatherForTarget failed: %v", err)
	}

	if len(bundle.RAGChunks) != 2 {
		t.Fatalf("expected 2 RAG chunks after filtering, got %d", len(bundle.RAGChunks))
	}
	for _, chunk := range bundle.RAGChunks {
		if !strings.HasPrefix(chunk.File, "internal/auth") {
			t.Errorf("expected chunk file to start with internal/auth, got %q", chunk.File)
		}
	}
}

func TestGatherForTarget_NilSearcher(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: t.TempDir(), DataDir: t.TempDir()},
	}
	assembler := NewAssembler(cfg, nil, nil)

	wo := &models.WorkOrder{Title: "test", Type: "new_feature", TargetModule: "auth"}
	target := Target{Path: "internal/auth", Rationale: "auth changes"}

	bundle, err := assembler.GatherForTarget(context.Background(), wo, target)
	if err != nil {
		t.Fatalf("GatherForTarget failed: %v", err)
	}

	if len(bundle.RAGChunks) != 0 {
		t.Errorf("expected empty RAGChunks with nil searcher, got %d", len(bundle.RAGChunks))
	}
}

func TestGatherForTarget_FilesAndSignatures(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, "internal", "auth")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}

	goContent := `package auth

func Login(user string) error {
	return nil
}

type Service struct {
	db *DB
}

func (s *Service) Logout() error {
	return nil
}
`
	if err := os.WriteFile(filepath.Join(targetDir, "service.go"), []byte(goContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: tmpDir, DataDir: t.TempDir()},
		Index: config.Index{
			Include: []string{"**/*.go"},
		},
	}
	assembler := NewAssembler(cfg, nil, nil)

	wo := &models.WorkOrder{Title: "test", Type: "new_feature", TargetModule: "auth"}
	target := Target{Path: "internal/auth", Rationale: "auth changes"}

	bundle, err := assembler.GatherForTarget(context.Background(), wo, target)
	if err != nil {
		t.Fatalf("GatherForTarget failed: %v", err)
	}

	if len(bundle.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(bundle.Files))
	}
	if bundle.Files[0].Path != "internal/auth/service.go" {
		t.Errorf("expected path internal/auth/service.go, got %q", bundle.Files[0].Path)
	}
	if !strings.Contains(bundle.Files[0].Content, "func Login") {
		t.Error("expected file content to contain func Login")
	}

	// Check signatures extracted
	if len(bundle.Signatures) < 3 {
		t.Fatalf("expected at least 3 signatures (func Login, type Service, func (s *Service) Logout), got %d: %v",
			len(bundle.Signatures), bundle.Signatures)
	}

	foundFunc := false
	foundType := false
	foundMethod := false
	for _, sig := range bundle.Signatures {
		if strings.HasPrefix(sig, "func Login") {
			foundFunc = true
		}
		if strings.HasPrefix(sig, "type Service") {
			foundType = true
		}
		if strings.HasPrefix(sig, "func (s *Service) Logout") {
			foundMethod = true
		}
	}
	if !foundFunc {
		t.Error("expected signature for func Login")
	}
	if !foundType {
		t.Error("expected signature for type Service")
	}
	if !foundMethod {
		t.Error("expected signature for method Logout")
	}
}

func TestAssemble_FileContentsPopulated(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "internal", "auth")
	if err := os.MkdirAll(authDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "handler.go"), []byte("package auth\n// handler\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: tmpDir, DataDir: t.TempDir()},
		Index: config.Index{
			MaxFileSizeBytes:      50 * 1024,
			MaxTotalFileSizeBytes: 512 * 1024,
		},
	}
	assembler := NewAssembler(cfg, nil, nil)

	wo := &models.WorkOrder{Title: "test", Type: "new_feature", TargetModule: "auth"}
	scopePkg := &models.ContextPackage{
		Summary:             "test",
		EstimatedComplexity: "low",
		FilesToModify:       []models.FileRef{{Path: "internal/auth/handler.go", Reason: "add route"}},
	}

	full, err := assembler.Assemble(context.Background(), wo, scopePkg, "feature/test")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(full.Scope.FileContents) != 1 {
		t.Fatalf("expected 1 file content entry, got %d", len(full.Scope.FileContents))
	}
	if full.Scope.FileContents[0].Path != "internal/auth/handler.go" {
		t.Errorf("expected path 'internal/auth/handler.go', got %q", full.Scope.FileContents[0].Path)
	}
	if !strings.Contains(full.Scope.FileContents[0].Source, "package auth") {
		t.Error("expected source to contain 'package auth'")
	}
}

func TestAssemble_FileContents_MissingFileSkipped(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: t.TempDir(), DataDir: t.TempDir()},
		Index: config.Index{
			MaxFileSizeBytes:      50 * 1024,
			MaxTotalFileSizeBytes: 512 * 1024,
		},
	}
	assembler := NewAssembler(cfg, nil, nil)

	wo := &models.WorkOrder{Title: "test", Type: "new_feature", TargetModule: "auth"}
	scopePkg := &models.ContextPackage{
		Summary:             "test",
		EstimatedComplexity: "low",
		FilesToModify:       []models.FileRef{{Path: "does/not/exist.go", Reason: "missing"}},
	}

	full, err := assembler.Assemble(context.Background(), wo, scopePkg, "feature/test")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(full.Scope.FileContents) != 0 {
		t.Errorf("expected 0 file contents for missing file, got %d", len(full.Scope.FileContents))
	}
}

func TestAssemble_FileContents_PerFileTruncation(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "big.go"), []byte(strings.Repeat("x", 200)), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: tmpDir, DataDir: t.TempDir()},
		Index: config.Index{
			MaxFileSizeBytes:      50, // small cap
			MaxTotalFileSizeBytes: 512 * 1024,
		},
	}
	assembler := NewAssembler(cfg, nil, nil)

	wo := &models.WorkOrder{Title: "test", Type: "new_feature", TargetModule: "x"}
	scopePkg := &models.ContextPackage{
		Summary:             "test",
		EstimatedComplexity: "low",
		FilesToModify:       []models.FileRef{{Path: "big.go", Reason: "big file"}},
	}

	full, err := assembler.Assemble(context.Background(), wo, scopePkg, "feature/test")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(full.Scope.FileContents) != 1 {
		t.Fatalf("expected 1 file content entry, got %d", len(full.Scope.FileContents))
	}
	if !strings.Contains(full.Scope.FileContents[0].Source, "[truncated: file exceeds max_file_size_bytes]") {
		t.Error("expected truncation marker in source")
	}
}

func TestAssemble_FileContents_TotalBudgetExhausted(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte(strings.Repeat("a", 80)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte(strings.Repeat("b", 80)), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: tmpDir, DataDir: t.TempDir()},
		Index: config.Index{
			MaxFileSizeBytes:      1024,
			MaxTotalFileSizeBytes: 80, // exactly enough for one file, exhausted after
		},
	}
	assembler := NewAssembler(cfg, nil, nil)

	wo := &models.WorkOrder{Title: "test", Type: "new_feature", TargetModule: "x"}
	scopePkg := &models.ContextPackage{
		Summary:             "test",
		EstimatedComplexity: "low",
		FilesToModify: []models.FileRef{
			{Path: "a.go", Reason: "first"},
			{Path: "b.go", Reason: "second"},
		},
	}

	full, err := assembler.Assemble(context.Background(), wo, scopePkg, "feature/test")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(full.Scope.FileContents) != 1 {
		t.Fatalf("expected 1 file content (budget exhausted), got %d", len(full.Scope.FileContents))
	}
	if full.Scope.FileContents[0].Path != "a.go" {
		t.Errorf("expected first file 'a.go', got %q", full.Scope.FileContents[0].Path)
	}
}

func TestAssemble_PassThroughFields(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: t.TempDir(), DataDir: t.TempDir()},
		Index: config.Index{
			MaxFileSizeBytes:      50 * 1024,
			MaxTotalFileSizeBytes: 512 * 1024,
		},
	}
	assembler := NewAssembler(cfg, nil, nil)

	wo := &models.WorkOrder{Title: "test", Type: "new_feature", TargetModule: "auth"}
	scopePkg := &models.ContextPackage{
		Summary:             "test",
		EstimatedComplexity: "medium",
		BuildInstructions:   "make build",
		NewFiles: []models.NewFile{
			{Path: "internal/auth/logout.go", Purpose: "logout handler"},
		},
		SQLFiles: []models.FileRef{
			{Path: "sql/001_auth.sql", Reason: "auth schema"},
		},
		Dependencies: []string{"github.com/go-chi/chi/v5"},
	}

	full, err := assembler.Assemble(context.Background(), wo, scopePkg, "feature/test")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if full.Scope.BuildInstructions != "make build" {
		t.Errorf("expected BuildInstructions 'make build', got %q", full.Scope.BuildInstructions)
	}
	if len(full.Scope.NewFiles) != 1 || full.Scope.NewFiles[0].Path != "internal/auth/logout.go" {
		t.Errorf("expected NewFiles with logout.go, got %v", full.Scope.NewFiles)
	}
	if len(full.Scope.SQLFiles) != 1 || full.Scope.SQLFiles[0].Path != "sql/001_auth.sql" {
		t.Errorf("expected SQLFiles with 001_auth.sql, got %v", full.Scope.SQLFiles)
	}
	if len(full.Scope.Dependencies) != 1 || full.Scope.Dependencies[0] != "github.com/go-chi/chi/v5" {
		t.Errorf("expected Dependencies with chi, got %v", full.Scope.Dependencies)
	}
}

// mockGraphQuerier is a test double for GraphQuerier.
type mockGraphQuerier struct{}

func (m *mockGraphQuerier) BlastRadius(targetSymbol string, direction int, maxDepth, budget int, minConfidence float64, includeTests bool) (BlastRadiusFormatted, error) {
	return BlastRadiusFormatted{
		TargetName:  "CreateSession",
		TargetKind:  "method",
		TargetFile:  "internal/auth/service.go",
		TargetLines: [2]int{45, 82},
		TargetSig:   "func (s *Service) CreateSession(ctx context.Context)",
		Upstream: []NodeInfo{
			{Name: "HandleLogin", Kind: "function", FilePath: "internal/api/auth.go", LineStart: 45, Depth: 1, EdgeType: "CALLS", Confidence: 1.0},
		},
		Downstream: []NodeInfo{
			{Name: "FindUserByEmail", Kind: "method", FilePath: "internal/auth/repo.go", LineStart: 31, Depth: 1, EdgeType: "CALLS", Confidence: 1.0},
		},
	}, nil
}

func (m *mockGraphQuerier) GetSymbolsForFile(filePath string) ([]SymbolInfo, error) {
	return []SymbolInfo{
		{Name: "CreateSession", Kind: "method", FilePath: filePath, LineStart: 45},
	}, nil
}

func TestAssembleScopePrompt_WithStructuralContext(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Name: "test", Path: t.TempDir()},
		Graph: config.GraphConfig{
			Enabled: true,
			BlastRadius: config.BlastRadiusConfig{
				MaxDepth: 3, Budget: 30, MinConfidence: 0.5,
			},
		},
	}

	assembler := NewAssembler(cfg, nil, &mockGraphQuerier{})

	wo := &models.WorkOrder{
		Title:      "Test work order",
		Type:       "new_feature",
		KnownFiles: []string{"internal/auth/service.go"},
	}

	prompt, err := assembler.AssembleScopePrompt(context.Background(), wo)
	if err != nil {
		t.Fatalf("AssembleScopePrompt: %v", err)
	}

	if !strings.Contains(prompt, "STRUCTURAL CONTEXT") {
		t.Error("expected structural context section")
	}
	if !strings.Contains(prompt, "CreateSession") {
		t.Error("expected CreateSession in structural context")
	}
	if !strings.Contains(prompt, "HandleLogin") {
		t.Error("expected upstream caller HandleLogin")
	}
	if !strings.Contains(prompt, "FindUserByEmail") {
		t.Error("expected downstream callee FindUserByEmail")
	}
}

func TestAssembleScopePrompt_WithoutGraph(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Name: "test", Path: t.TempDir()},
	}

	// nil graph querier — should work without structural context
	assembler := NewAssembler(cfg, nil, nil)

	wo := &models.WorkOrder{
		Title: "Test work order",
		Type:  "new_feature",
	}

	prompt, err := assembler.AssembleScopePrompt(context.Background(), wo)
	if err != nil {
		t.Fatalf("AssembleScopePrompt: %v", err)
	}

	if strings.Contains(prompt, "STRUCTURAL CONTEXT") {
		t.Error("should not contain structural context when graph is nil")
	}
}
