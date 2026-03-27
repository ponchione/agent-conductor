package graph

import (
	"strings"
	"testing"
)

func TestFormatBlastRadiusBlock_Empty(t *testing.T) {
	result := FormatBlastRadiusBlock(nil)
	if result != "" {
		t.Fatalf("expected empty string for nil results, got %q", result)
	}
}

func TestFormatBlastRadiusBlock_SingleTarget(t *testing.T) {
	results := []*BlastRadiusResult{
		{
			Target: Symbol{
				Name: "CreateSession", Kind: "method",
				FilePath: "internal/auth/service.go", LineStart: 45, LineEnd: 82,
				Signature: "func (s *Service) CreateSession(ctx context.Context, req SessionRequest) (*Session, error)",
			},
			Upstream: []BlastRadiusNode{
				{
					Symbol:     Symbol{Name: "HandleLogin", Kind: "function", FilePath: "internal/api/auth.go", LineStart: 45, Signature: "func HandleLogin(w http.ResponseWriter, r *http.Request)"},
					Depth:      1,
					EdgeType:   "CALLS",
					Confidence: 1.0,
				},
			},
			Downstream: []BlastRadiusNode{
				{
					Symbol:     Symbol{Name: "FindUserByEmail", Kind: "method", FilePath: "internal/auth/repository.go", LineStart: 31, Signature: "func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*User, error)"},
					Depth:      1,
					EdgeType:   "CALLS",
					Confidence: 1.0,
				},
			},
			Interfaces: []Symbol{
				{Name: "SessionCreator", Kind: "interface", FilePath: "internal/auth/interfaces.go", LineStart: 5},
			},
		},
	}

	block := FormatBlastRadiusBlock(results)

	if !strings.Contains(block, "=== STRUCTURAL CONTEXT (call graph) ===") {
		t.Error("missing header")
	}
	if !strings.Contains(block, "CreateSession") {
		t.Error("missing target name")
	}
	if !strings.Contains(block, "UPSTREAM") {
		t.Error("missing upstream section")
	}
	if !strings.Contains(block, "HandleLogin") {
		t.Error("missing upstream caller")
	}
	if !strings.Contains(block, "DOWNSTREAM") {
		t.Error("missing downstream section")
	}
	if !strings.Contains(block, "FindUserByEmail") {
		t.Error("missing downstream callee")
	}
	if !strings.Contains(block, "IMPLEMENTS") {
		t.Error("missing implements section")
	}
	if !strings.Contains(block, "SessionCreator") {
		t.Error("missing interface name")
	}
}

func TestFormatBlastRadiusBlock_NoUpstream(t *testing.T) {
	results := []*BlastRadiusResult{
		{
			Target: Symbol{Name: "main", Kind: "function", FilePath: "cmd/app/main.go", LineStart: 1, LineEnd: 10},
			Downstream: []BlastRadiusNode{
				{
					Symbol:     Symbol{Name: "Run", Kind: "function", FilePath: "internal/app/run.go", LineStart: 5},
					Depth:      1,
					EdgeType:   "CALLS",
					Confidence: 0.9,
				},
			},
		},
	}

	block := FormatBlastRadiusBlock(results)

	if strings.Contains(block, "UPSTREAM") {
		t.Error("should not contain UPSTREAM section when empty")
	}
	if !strings.Contains(block, "DOWNSTREAM") {
		t.Error("missing downstream section")
	}
}

func TestFormatEdgeCountAnnotation(t *testing.T) {
	tests := []struct {
		callers  int
		callees  int
		expected string
	}{
		{0, 0, ""},
		{4, 0, "[called by 4 functions]"},
		{0, 2, "[calls 2 functions]"},
		{4, 2, "[called by 4 functions, calls 2 functions]"},
	}

	for _, tt := range tests {
		got := FormatEdgeCountAnnotation(tt.callers, tt.callees)
		if got != tt.expected {
			t.Errorf("FormatEdgeCountAnnotation(%d, %d) = %q, want %q", tt.callers, tt.callees, got, tt.expected)
		}
	}
}
