package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewGoAnalyzer_InvalidDir(t *testing.T) {
	_, err := NewGoAnalyzer("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestNewGoAnalyzer_ValidModule(t *testing.T) {
	root, err := findModuleRoot()
	if err != nil {
		t.Skipf("cannot find module root: %v", err)
	}

	analyzer, err := NewGoAnalyzer(root)
	if err != nil {
		t.Fatalf("NewGoAnalyzer: %v", err)
	}

	if analyzer.modulePath == "" {
		t.Fatal("expected non-empty module path")
	}
	if len(analyzer.pkgsByFile) == 0 {
		t.Fatal("expected pkgsByFile to be populated")
	}
}

func TestSymbolID(t *testing.T) {
	tests := []struct {
		lang, pkg, kind, name string
		want                  string
	}{
		{"go", "github.com/org/repo/internal/auth", "function", "CreateSession", "go:github.com/org/repo/internal/auth:function:CreateSession"},
		{"go", "github.com/org/repo/internal/auth", "method", "Service.CreateSession", "go:github.com/org/repo/internal/auth:method:Service.CreateSession"},
		{"go", "github.com/org/repo/internal/auth", "interface", "SessionCreator", "go:github.com/org/repo/internal/auth:interface:SessionCreator"},
	}

	for _, tt := range tests {
		got := symbolID(tt.lang, tt.pkg, tt.kind, tt.name)
		if got != tt.want {
			t.Errorf("symbolID(%q, %q, %q, %q) = %q, want %q", tt.lang, tt.pkg, tt.kind, tt.name, got, tt.want)
		}
	}
}

func TestGoAnalyzer_Integration(t *testing.T) {
	root, err := findModuleRoot()
	if err != nil {
		t.Skipf("cannot find module root: %v", err)
	}

	analyzer, err := NewGoAnalyzer(root)
	if err != nil {
		t.Fatalf("NewGoAnalyzer: %v", err)
	}

	result, err := analyzer.Analyze()
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(result.Symbols) == 0 {
		t.Fatal("expected symbols to be extracted")
	}
	if len(result.Edges) == 0 {
		t.Fatal("expected edges to be extracted")
	}

	// Spot-check: NewGoAnalyzer should exist as a function symbol.
	found := false
	for _, sym := range result.Symbols {
		if sym.Name == "NewGoAnalyzer" && sym.Kind == "function" {
			found = true
			if sym.Language != "go" {
				t.Errorf("NewGoAnalyzer: expected language=go, got %q", sym.Language)
			}
			if !sym.Exported {
				t.Error("NewGoAnalyzer: expected Exported=true")
			}
			if !strings.HasPrefix(sym.ID, "go:") {
				t.Errorf("NewGoAnalyzer: expected ID to start with go:, got %q", sym.ID)
			}
			break
		}
	}
	if !found {
		t.Error("expected to find NewGoAnalyzer symbol")
	}

	// Spot-check: Symbol type should exist.
	found = false
	for _, sym := range result.Symbols {
		if sym.Name == "Symbol" && sym.Kind == "type" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find Symbol type symbol")
	}

	// Spot-check: GoAnalyzer type should exist.
	found = false
	for _, sym := range result.Symbols {
		if sym.Name == "GoAnalyzer" && sym.Kind == "type" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find GoAnalyzer type symbol")
	}

	// Spot-check: should have CALLS edges.
	callEdges := 0
	for _, e := range result.Edges {
		if e.EdgeType == "CALLS" {
			callEdges++
		}
	}
	if callEdges == 0 {
		t.Error("expected CALLS edges")
	}

	// Spot-check: should have IMPORTS edges.
	importEdges := 0
	for _, e := range result.Edges {
		if e.EdgeType == "IMPORTS" {
			importEdges++
		}
	}
	if importEdges == 0 {
		t.Error("expected IMPORTS edges")
	}

	// Spot-check: should have boundary symbols (stdlib calls).
	if len(result.BoundarySymbols) == 0 {
		t.Error("expected boundary symbols")
	}

	t.Logf("Symbols: %d, Edges: %d, Boundary: %d, CALLS: %d, IMPORTS: %d",
		len(result.Symbols), len(result.Edges), len(result.BoundarySymbols), callEdges, importEdges)
}

// findModuleRoot walks up from the current directory to find go.mod.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
