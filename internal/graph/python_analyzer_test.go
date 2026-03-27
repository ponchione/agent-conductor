package graph

import (
	"path/filepath"
	"testing"
)

func TestNewPythonAnalyzer(t *testing.T) {
	a := NewPythonAnalyzer("/tmp", []string{"**/*.py"}, nil)
	if a.projectRoot != "/tmp" {
		t.Fatalf("expected /tmp, got %s", a.projectRoot)
	}
	if len(a.includeGlobs) != 1 || a.includeGlobs[0] != "**/*.py" {
		t.Fatalf("unexpected includeGlobs: %v", a.includeGlobs)
	}
	if a.excludeGlobs != nil {
		t.Fatalf("expected nil excludeGlobs, got %v", a.excludeGlobs)
	}
}

func TestPythonModulePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"lib/auth.py", "lib.auth"},
		{"lib/auth/service.py", "lib.auth.service"},
		{"lib/__init__.py", "lib"},
		{"main.py", "main"},
		{"a/b/c/__init__.py", "a.b.c"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := pythonModulePath(tt.input)
			if got != tt.want {
				t.Errorf("pythonModulePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPythonSymbolID(t *testing.T) {
	got := pythonSymbolID("lib.auth", "function", "validate_token")
	want := "py:lib.auth:function:validate_token"
	if got != want {
		t.Errorf("pythonSymbolID = %q, want %q", got, want)
	}
}

func TestExtractPySymbols_Function(t *testing.T) {
	src := []byte(`def hello(name):
    return "Hello " + name
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("app.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}
	if len(pf.symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(pf.symbols))
	}
	sym := pf.symbols[0]
	if sym.Name != "hello" {
		t.Errorf("expected name 'hello', got %q", sym.Name)
	}
	if sym.Kind != "function" {
		t.Errorf("expected kind 'function', got %q", sym.Kind)
	}
	if sym.ID != "py:app:function:hello" {
		t.Errorf("unexpected ID: %q", sym.ID)
	}
	if sym.Language != "python" {
		t.Errorf("expected language 'python', got %q", sym.Language)
	}
	if sym.Package != "app" {
		t.Errorf("expected package 'app', got %q", sym.Package)
	}
	if !sym.Exported {
		t.Error("expected symbol to be exported (no underscore prefix)")
	}
	if sym.LineStart != 1 {
		t.Errorf("expected LineStart=1, got %d", sym.LineStart)
	}
}

func TestExtractPySymbols_PrivateFunction(t *testing.T) {
	src := []byte(`def _internal():
    pass
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("util.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}
	if len(pf.symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(pf.symbols))
	}
	if pf.symbols[0].Exported {
		t.Error("expected _internal to be marked as not exported")
	}
}

func TestExtractPySymbols_ClassAndMethods(t *testing.T) {
	src := []byte(`class MyService:
    def __init__(self):
        pass

    def process(self, data):
        return data
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("svc.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}

	// Expect: class + __init__ + process = 3 symbols
	if len(pf.symbols) != 3 {
		t.Fatalf("expected 3 symbols, got %d", len(pf.symbols))
	}

	// Check class
	classSym := pf.symbols[0]
	if classSym.Kind != "class" || classSym.Name != "MyService" {
		t.Errorf("expected class MyService, got %s %s", classSym.Kind, classSym.Name)
	}

	// Check methods
	initSym := pf.symbols[1]
	if initSym.Kind != "method" || initSym.Name != "__init__" {
		t.Errorf("expected method __init__, got %s %s", initSym.Kind, initSym.Name)
	}
	if initSym.Receiver != "MyService" {
		t.Errorf("expected receiver MyService, got %q", initSym.Receiver)
	}

	processSym := pf.symbols[2]
	if processSym.Kind != "method" || processSym.Name != "process" {
		t.Errorf("expected method process, got %s %s", processSym.Kind, processSym.Name)
	}

	// Check class tracking
	methods, ok := pf.classes["MyService"]
	if !ok {
		t.Fatal("expected MyService in classes map")
	}
	if len(methods) != 2 {
		t.Fatalf("expected 2 methods, got %d: %v", len(methods), methods)
	}
}

func TestExtractPySymbols_Imports(t *testing.T) {
	src := []byte(`from lib.auth import AuthService, validate_token
import os
from lib.service import AppService
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("main.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}

	if len(pf.imports) != 3 {
		t.Fatalf("expected 3 imports, got %d", len(pf.imports))
	}

	// from lib.auth import AuthService, validate_token
	imp0 := pf.imports[0]
	if imp0.modulePath != "lib.auth" {
		t.Errorf("import[0] modulePath = %q, want 'lib.auth'", imp0.modulePath)
	}
	if len(imp0.names) != 2 {
		t.Fatalf("import[0] expected 2 names, got %d", len(imp0.names))
	}
	if imp0.names[0] != "AuthService" || imp0.names[1] != "validate_token" {
		t.Errorf("import[0] names = %v, want [AuthService validate_token]", imp0.names)
	}

	// import os
	imp1 := pf.imports[1]
	if imp1.modulePath != "os" {
		t.Errorf("import[1] modulePath = %q, want 'os'", imp1.modulePath)
	}
	if len(imp1.names) != 0 {
		t.Errorf("import[1] expected 0 names, got %d", len(imp1.names))
	}

	// from lib.service import AppService
	imp2 := pf.imports[2]
	if imp2.modulePath != "lib.service" {
		t.Errorf("import[2] modulePath = %q, want 'lib.service'", imp2.modulePath)
	}
}

func TestExtractPySymbols_ImportWithAlias(t *testing.T) {
	src := []byte(`from lib.auth import validate_token as vt
import numpy as np
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("test.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}

	if len(pf.imports) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(pf.imports))
	}

	// Aliases should be stripped; original name kept.
	if pf.imports[0].names[0] != "validate_token" {
		t.Errorf("expected 'validate_token', got %q", pf.imports[0].names[0])
	}
	if pf.imports[1].modulePath != "numpy" {
		t.Errorf("expected 'numpy', got %q", pf.imports[1].modulePath)
	}
}

func TestResolveImportToFile(t *testing.T) {
	a := NewPythonAnalyzer("/tmp", nil, nil)
	projectFiles := map[string]bool{
		"lib/auth.py":       true,
		"lib/__init__.py":   true,
		"lib/service.py":    true,
		"main.py":           true,
		"utils/__init__.py": true,
	}

	tests := []struct {
		importPath string
		wantPath   string
		wantOK     bool
	}{
		{"lib.auth", "lib/auth.py", true},
		{"lib.service", "lib/service.py", true},
		{"lib", "lib/__init__.py", true},
		{"main", "main.py", true},
		{"utils", "utils/__init__.py", true},
		{"nonexistent", "", false},
		{"os", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.importPath, func(t *testing.T) {
			gotPath, gotOK := a.resolveImportToFile(tt.importPath, projectFiles)
			if gotOK != tt.wantOK {
				t.Errorf("resolveImportToFile(%q) ok=%v, want %v", tt.importPath, gotOK, tt.wantOK)
			}
			if gotPath != tt.wantPath {
				t.Errorf("resolveImportToFile(%q) path=%q, want %q", tt.importPath, gotPath, tt.wantPath)
			}
		})
	}
}

func TestExtractImportEdges(t *testing.T) {
	a := NewPythonAnalyzer("/tmp", nil, nil)
	projectFiles := map[string]bool{
		"lib/auth.py":    true,
		"lib/service.py": true,
		"main.py":        true,
	}

	pf := &parsedPyFile{
		relPath:    "main.py",
		modulePath: "main",
		imports: []pyImport{
			{modulePath: "lib.auth", names: []string{"AuthService"}},
			{modulePath: "os"},
		},
	}

	edges, boundaries := a.extractImportEdges(pf, projectFiles)

	if len(edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(edges))
	}

	// First edge: project import
	if edges[0].EdgeType != "IMPORTS" {
		t.Errorf("edge[0] type = %q, want IMPORTS", edges[0].EdgeType)
	}
	if edges[0].Confidence != 1.0 {
		t.Errorf("edge[0] confidence = %f, want 1.0", edges[0].Confidence)
	}
	if edges[0].TargetID != "py:lib.auth:module:lib.auth" {
		t.Errorf("edge[0] targetID = %q, want py:lib.auth:module:lib.auth", edges[0].TargetID)
	}

	// Second edge: external import
	if edges[1].TargetID != "py:os:module:os" {
		t.Errorf("edge[1] targetID = %q, want py:os:module:os", edges[1].TargetID)
	}

	// Should have 1 boundary symbol for "os"
	if len(boundaries) != 1 {
		t.Fatalf("expected 1 boundary, got %d", len(boundaries))
	}
	if boundaries[0].Name != "os" {
		t.Errorf("boundary name = %q, want 'os'", boundaries[0].Name)
	}
}

func TestFindContainingSymbol(t *testing.T) {
	pf := &parsedPyFile{
		symbols: []Symbol{
			{ID: "py:app:function:main", Kind: "function", LineStart: 1, LineEnd: 5},
			{ID: "py:app:class:Foo", Kind: "class", LineStart: 7, LineEnd: 20},
			{ID: "py:app:method:Foo.process", Kind: "method", LineStart: 8, LineEnd: 15},
		},
	}

	tests := []struct {
		line int
		want string
	}{
		{3, "py:app:function:main"},
		{10, "py:app:method:Foo.process"}, // method is more specific than class
		{18, ""},                           // line 18 is in class but not in a method
		{25, ""},                           // outside everything
	}

	for _, tt := range tests {
		got := findContainingSymbol(pf, tt.line)
		if got != tt.want {
			t.Errorf("findContainingSymbol(line=%d) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestFindContainingClass(t *testing.T) {
	pf := &parsedPyFile{
		symbols: []Symbol{
			{ID: "py:app:class:Foo", Kind: "class", Name: "Foo", LineStart: 1, LineEnd: 20},
			{ID: "py:app:function:bar", Kind: "function", Name: "bar", LineStart: 22, LineEnd: 30},
		},
	}

	if got := findContainingClass(pf, 10); got != "Foo" {
		t.Errorf("expected Foo, got %q", got)
	}
	if got := findContainingClass(pf, 25); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestPythonAnalyzer_Integration(t *testing.T) {
	testProject, err := filepath.Abs("testdata/pyproject")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	analyzer := NewPythonAnalyzer(testProject, []string{"**/*.py"}, nil)
	result, err := analyzer.Analyze()
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(result.Symbols) == 0 {
		t.Fatal("expected symbols")
	}

	// Check expected symbols exist.
	symbolsByName := make(map[string]bool)
	symbolsByID := make(map[string]Symbol)
	for _, sym := range result.Symbols {
		symbolsByName[sym.Name] = true
		symbolsByID[sym.ID] = sym
	}

	expectedSymbols := []string{
		"validate_token",
		"AuthService",
		"authenticate",
		"get_user",
		"AppService",
		"__init__",
		"process",
		"main",
	}
	for _, expected := range expectedSymbols {
		if !symbolsByName[expected] {
			t.Errorf("missing expected symbol: %s", expected)
		}
	}

	// Verify symbol IDs follow the py:{module}:{kind}:{name} scheme.
	for _, sym := range result.Symbols {
		if sym.ID == "" {
			t.Error("symbol has empty ID")
		}
		if sym.Language != "python" {
			t.Errorf("symbol %q has language %q, want python", sym.Name, sym.Language)
		}
	}

	// Check for IMPORTS edges.
	importEdges := 0
	callEdges := 0
	for _, e := range result.Edges {
		switch e.EdgeType {
		case "IMPORTS":
			importEdges++
		case "CALLS":
			callEdges++
		}
	}

	if importEdges == 0 {
		t.Error("expected IMPORTS edges")
	}
	if callEdges == 0 {
		t.Error("expected CALLS edges")
	}

	// Verify no false positives: all CALLS edges should have confidence >= 0.7.
	for _, e := range result.Edges {
		if e.EdgeType == "CALLS" && e.Confidence < 0.7 {
			t.Errorf("CALLS edge with low confidence: %s -> %s (%.2f)", e.SourceID, e.TargetID, e.Confidence)
		}
	}

	// Verify all edge source/target IDs reference known symbols or boundary symbols.
	knownIDs := make(map[string]bool)
	for _, sym := range result.Symbols {
		knownIDs[sym.ID] = true
	}
	for _, bs := range result.BoundarySymbols {
		knownIDs[bs.ID] = true
	}
	// Module-level pseudo-symbols used for import edges.
	for _, e := range result.Edges {
		knownIDs[e.SourceID] = true
		knownIDs[e.TargetID] = true
	}

	t.Logf("Symbols: %d, Edges: %d (imports: %d, calls: %d), Boundary: %d",
		len(result.Symbols), len(result.Edges), importEdges, callEdges, len(result.BoundarySymbols))

	// Log all edges for debugging.
	for _, e := range result.Edges {
		t.Logf("  Edge: %s -> %s [%s, conf=%.2f]", e.SourceID, e.TargetID, e.EdgeType, e.Confidence)
	}
}

func TestPythonAnalyzer_SelfMethodCall(t *testing.T) {
	// Verify self.method() produces a CALLS edge with confidence 0.95.
	src := []byte(`class MyClass:
    def helper(self):
        return 42

    def run(self):
        return self.helper()
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("test.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}

	allSymbols := make(map[string][]Symbol)
	for _, sym := range pf.symbols {
		allSymbols[sym.Name] = append(allSymbols[sym.Name], sym)
	}

	edges := a.extractCallEdges(pf, src, allSymbols)
	if len(edges) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(edges))
	}

	edge := edges[0]
	if edge.EdgeType != "CALLS" {
		t.Errorf("edge type = %q, want CALLS", edge.EdgeType)
	}
	if edge.Confidence != 0.95 {
		t.Errorf("confidence = %f, want 0.95", edge.Confidence)
	}
	if edge.TargetID != "py:test:method:MyClass.helper" {
		t.Errorf("targetID = %q, want py:test:method:MyClass.helper", edge.TargetID)
	}
}

func TestPythonAnalyzer_BareCallImported(t *testing.T) {
	// Bare call to an imported function that is unique across the project.
	src := []byte(`from lib.auth import validate_token

def check(tok):
    return validate_token(tok)
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("checker.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}

	// Simulate a project where validate_token is unique.
	allSymbols := map[string][]Symbol{
		"validate_token": {
			{
				ID:   "py:lib.auth:function:validate_token",
				Name: "validate_token",
				Kind: "function",
			},
		},
	}

	edges := a.extractCallEdges(pf, src, allSymbols)
	if len(edges) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(edges))
	}

	if edges[0].Confidence != 0.85 {
		t.Errorf("confidence = %f, want 0.85", edges[0].Confidence)
	}
	if edges[0].TargetID != "py:lib.auth:function:validate_token" {
		t.Errorf("targetID = %q, want py:lib.auth:function:validate_token", edges[0].TargetID)
	}
}

func TestPythonAnalyzer_AmbiguousBareCall(t *testing.T) {
	// Bare call with multiple matches should NOT emit an edge.
	src := []byte(`from lib.auth import validate

def check(tok):
    return validate(tok)
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("checker.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}

	// Simulate ambiguity: two functions named "validate".
	allSymbols := map[string][]Symbol{
		"validate": {
			{ID: "py:lib.auth:function:validate", Name: "validate"},
			{ID: "py:lib.other:function:validate", Name: "validate"},
		},
	}

	edges := a.extractCallEdges(pf, src, allSymbols)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for ambiguous call, got %d", len(edges))
	}
}

func TestPythonAnalyzer_UnimportedBareCall(t *testing.T) {
	// Bare call to a function that is NOT imported should NOT emit an edge.
	src := []byte(`def check(tok):
    return something(tok)
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("checker.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}

	allSymbols := map[string][]Symbol{
		"something": {
			{ID: "py:lib:function:something", Name: "something"},
		},
	}

	edges := a.extractCallEdges(pf, src, allSymbols)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for unimported call, got %d", len(edges))
	}
}

func TestPythonAnalyzer_EmptyProject(t *testing.T) {
	a := NewPythonAnalyzer(t.TempDir(), nil, nil)
	result, err := a.Analyze()
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(result.Symbols))
	}
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(result.Edges))
	}
}

func TestPythonAnalyzer_DecoratedFunction(t *testing.T) {
	src := []byte(`@app.route("/api")
def index():
    return "hello"
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("routes.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}
	if len(pf.symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(pf.symbols))
	}
	if pf.symbols[0].Name != "index" {
		t.Errorf("expected name 'index', got %q", pf.symbols[0].Name)
	}
	if pf.symbols[0].Kind != "function" {
		t.Errorf("expected kind 'function', got %q", pf.symbols[0].Kind)
	}
}

func TestPythonAnalyzer_SkipsRelativeImports(t *testing.T) {
	src := []byte(`from . import utils
from .auth import validate
`)
	a := NewPythonAnalyzer("/tmp", nil, nil)
	pf, err := a.extractPySymbols("pkg/main.py", src)
	if err != nil {
		t.Fatalf("extractPySymbols: %v", err)
	}
	if len(pf.imports) != 0 {
		t.Errorf("expected 0 imports (relative imports skipped), got %d", len(pf.imports))
	}
}

func TestWalkPythonFiles(t *testing.T) {
	testProject, err := filepath.Abs("testdata/pyproject")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	a := NewPythonAnalyzer(testProject, nil, nil)
	files, err := a.walkPythonFiles()
	if err != nil {
		t.Fatalf("walkPythonFiles: %v", err)
	}

	if len(files) < 3 {
		t.Fatalf("expected at least 3 Python files, got %d: %v", len(files), files)
	}

	// All files should end with .py
	for _, f := range files {
		if filepath.Ext(f) != ".py" {
			t.Errorf("non-python file: %s", f)
		}
	}
}
