package graph

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// testdataDir returns the absolute path to the testdata directory
// relative to this test file's source location.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata")
}

func TestNewTSAnalyzer(t *testing.T) {
	// Skip if node is not available.
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}

	analyzer, err := NewTSAnalyzer()
	if err != nil {
		t.Fatalf("NewTSAnalyzer: %v", err)
	}
	if analyzer.nodePath == "" {
		t.Fatal("expected nodePath to be set")
	}
	if analyzer.scriptPath == "" {
		t.Fatal("expected scriptPath to be set")
	}
}

func TestTSAnalyzer_Integration(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("npx not on PATH")
	}

	analyzer, err := NewTSAnalyzer()
	if err != nil {
		t.Fatalf("NewTSAnalyzer: %v", err)
	}

	testProject := filepath.Join(testdataDir(t), "tsproject")
	result, err := analyzer.Analyze(testProject, "")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(result.Symbols) == 0 {
		t.Fatal("expected symbols")
	}

	// Should find formatName, validateEmail, UserService, createUser
	names := make(map[string]bool)
	for _, sym := range result.Symbols {
		names[sym.Name] = true
		// All symbols should have typescript as language.
		if sym.Language != "typescript" {
			t.Errorf("symbol %s has language %q, want %q", sym.Name, sym.Language, "typescript")
		}
	}

	for _, expected := range []string{"formatName", "validateEmail", "UserService"} {
		if !names[expected] {
			t.Errorf("expected symbol %s not found; got symbols: %v", expected, symbolNames(result.Symbols))
		}
	}

	// Should have CALLS edges (createUser -> validateEmail, createUser -> formatName)
	if len(result.Edges) == 0 {
		t.Fatal("expected edges")
	}

	// Verify at least one CALLS edge exists.
	hasCallsEdge := false
	for _, e := range result.Edges {
		if e.EdgeType == "CALLS" {
			hasCallsEdge = true
			break
		}
	}
	if !hasCallsEdge {
		t.Error("expected at least one CALLS edge")
	}

	// Verify at least one IMPORTS edge exists.
	hasImportsEdge := false
	for _, e := range result.Edges {
		if e.EdgeType == "IMPORTS" {
			hasImportsEdge = true
			break
		}
	}
	if !hasImportsEdge {
		t.Error("expected at least one IMPORTS edge")
	}

	t.Logf("Symbols: %d, Edges: %d, Boundary: %d",
		len(result.Symbols), len(result.Edges), len(result.BoundarySymbols))
	for _, sym := range result.Symbols {
		t.Logf("  symbol: %s (kind=%s, exported=%v)", sym.ID, sym.Kind, sym.Exported)
	}
	for _, e := range result.Edges {
		t.Logf("  edge: %s -[%s]-> %s (line=%d)", e.SourceID, e.EdgeType, e.TargetID, e.SourceLine)
	}
}

func symbolNames(symbols []Symbol) []string {
	names := make([]string, len(symbols))
	for i, s := range symbols {
		names[i] = s.Name
	}
	return names
}
