package rag

import (
	"testing"

	"github.com/ponchione/agent-conductor/internal/models"
)

func TestExpandQueries(t *testing.T) {
	wo := &models.WorkOrder{
		Title:        "Add logout endpoint to auth module",
		TargetModule: "auth",
		AcceptanceCriteria: []string{
			"go test passes",                                         // filtered: build/test
			"Handler returns 200 on valid session token",             // kept: >4 words, not build/test
			"compiles",                                               // filtered: build/test
			"short",                                                  // filtered: <=4 words
			"Session is invalidated and cookie is cleared on logout", // kept
		},
		KnownFiles: []string{
			"internal/auth/handler.go",
			"internal/auth/service.go",
		},
	}

	queries := expandQueries(wo)

	// Expected: title, target_module, 2 AC, 2 known_files = 6
	if len(queries) != 6 {
		t.Fatalf("expected 6 queries, got %d: %v", len(queries), queries)
	}

	if queries[0] != wo.Title {
		t.Errorf("first query should be title, got %q", queries[0])
	}
	if queries[1] != "auth" {
		t.Errorf("second query should be target_module, got %q", queries[1])
	}
	if queries[2] != "Handler returns 200 on valid session token" {
		t.Errorf("expected AC query, got %q", queries[2])
	}
	if queries[3] != "Session is invalidated and cookie is cleared on logout" {
		t.Errorf("expected AC query, got %q", queries[3])
	}
	if queries[4] != "internal/auth handler" {
		t.Errorf("expected known_files query 'internal/auth handler', got %q", queries[4])
	}
	if queries[5] != "internal/auth service" {
		t.Errorf("expected known_files query 'internal/auth service', got %q", queries[5])
	}
}

func TestExpandQueries_Empty(t *testing.T) {
	wo := &models.WorkOrder{
		Title: "Simple title only",
	}
	queries := expandQueries(wo)
	if len(queries) != 1 {
		t.Fatalf("expected 1 query, got %d: %v", len(queries), queries)
	}
	if queries[0] != "Simple title only" {
		t.Errorf("expected title query, got %q", queries[0])
	}
}

func TestIsBuildTestCriterion(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"go test passes", true},
		{"go build succeeds", true},
		{"go vet ./... passes", true},
		{"passes all checks", true},
		{"compiles without errors", true},
		{"Handler returns 200 on valid session token", false},
		{"Session is invalidated on logout", false},
		{"Add new endpoint for user auth", false},
	}

	for _, tt := range tests {
		got := isBuildTestCriterion(tt.input)
		if got != tt.want {
			t.Errorf("isBuildTestCriterion(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
