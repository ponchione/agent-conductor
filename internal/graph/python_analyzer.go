package graph

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// PythonAnalyzer extracts symbols and edges from Python files using tree-sitter.
type PythonAnalyzer struct {
	projectRoot  string
	includeGlobs []string
	excludeGlobs []string
}

// NewPythonAnalyzer creates a new Python analyzer.
func NewPythonAnalyzer(projectRoot string, include, exclude []string) *PythonAnalyzer {
	return &PythonAnalyzer{
		projectRoot:  projectRoot,
		includeGlobs: include,
		excludeGlobs: exclude,
	}
}

// parsedPyFile holds parsed symbols and imports for a single Python file.
type parsedPyFile struct {
	relPath    string
	modulePath string
	symbols    []Symbol
	imports    []pyImport
	classes    map[string][]string // className -> method names
}

// pyImport represents a parsed Python import statement.
type pyImport struct {
	modulePath string   // "lib.auth.service"
	names      []string // imported names (empty for "import X")
}

// pythonModulePath converts a file path to a Python module dotpath.
// e.g., "lib/auth/service.py" -> "lib.auth.service"
// e.g., "lib/__init__.py" -> "lib"
func pythonModulePath(relPath string) string {
	relPath = strings.TrimSuffix(relPath, ".py")
	relPath = strings.TrimSuffix(relPath, "/__init__")
	return strings.ReplaceAll(relPath, "/", ".")
}

// pythonSymbolID creates a deterministic symbol ID for Python.
// Format: py:{module.dotpath}:{kind}:{name}
func pythonSymbolID(modulePath, kind, name string) string {
	return "py:" + modulePath + ":" + kind + ":" + name
}

// walkPythonFiles collects all Python files under the project root,
// skipping common non-project directories.
func (a *PythonAnalyzer) walkPythonFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(a.projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == "venv" || base == ".venv" || base == "node_modules" ||
				base == "__pycache__" || base == ".git" || base == ".tox" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".py") {
			return nil
		}

		rel, err := filepath.Rel(a.projectRoot, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		files = append(files, rel)
		return nil
	})
	return files, err
}

// newPythonParser creates a tree-sitter parser configured for Python.
func newPythonParser() (*sitter.Parser, error) {
	parser := sitter.NewParser()
	if err := parser.SetLanguage(sitter.NewLanguage(python.Language())); err != nil {
		parser.Close()
		return nil, fmt.Errorf("set python language: %w", err)
	}
	return parser, nil
}

// extractPySymbols parses a Python file and extracts symbols and imports.
func (a *PythonAnalyzer) extractPySymbols(relPath string, content []byte) (*parsedPyFile, error) {
	parser, err := newPythonParser()
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	tree := parser.Parse(content, nil)
	defer tree.Close()

	root := tree.RootNode()
	modPath := pythonModulePath(relPath)

	pf := &parsedPyFile{
		relPath:    relPath,
		modulePath: modPath,
		classes:    make(map[string][]string),
	}

	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}

		switch child.Kind() {
		case "function_definition":
			a.extractFunction(child, content, modPath, relPath, pf)

		case "class_definition":
			a.extractClass(child, content, modPath, relPath, pf)

		case "decorated_definition":
			a.extractDecorated(child, content, modPath, relPath, pf)

		case "import_statement", "import_from_statement":
			imp := parsePyImport(child, content)
			if imp != nil {
				pf.imports = append(pf.imports, *imp)
			}
		}
	}

	return pf, nil
}

// extractFunction extracts a top-level function symbol.
func (a *PythonAnalyzer) extractFunction(node *sitter.Node, content []byte, modPath, relPath string, pf *parsedPyFile) {
	name := pyNodeFieldText(node, "name", content)
	if name == "" {
		return
	}
	pf.symbols = append(pf.symbols, Symbol{
		ID:        pythonSymbolID(modPath, "function", name),
		Name:      name,
		Kind:      "function",
		Language:  "python",
		Package:   modPath,
		FilePath:  relPath,
		LineStart: int(node.StartPosition().Row) + 1,
		LineEnd:   int(node.EndPosition().Row) + 1,
		Signature: extractPySignature(node, content),
		Exported:  !strings.HasPrefix(name, "_"),
	})
}

// extractClass extracts a class symbol and its methods.
func (a *PythonAnalyzer) extractClass(node *sitter.Node, content []byte, modPath, relPath string, pf *parsedPyFile) {
	className := pyNodeFieldText(node, "name", content)
	if className == "" {
		return
	}

	pf.symbols = append(pf.symbols, Symbol{
		ID:        pythonSymbolID(modPath, "class", className),
		Name:      className,
		Kind:      "class",
		Language:  "python",
		Package:   modPath,
		FilePath:  relPath,
		LineStart: int(node.StartPosition().Row) + 1,
		LineEnd:   int(node.EndPosition().Row) + 1,
		Signature: "class " + className,
		Exported:  !strings.HasPrefix(className, "_"),
	})

	body := node.ChildByFieldName("body")
	if body == nil {
		return
	}

	for j := uint(0); j < body.ChildCount(); j++ {
		member := body.Child(j)
		if member == nil {
			continue
		}

		switch member.Kind() {
		case "function_definition":
			a.extractMethod(member, member, content, modPath, relPath, className, pf)
		case "decorated_definition":
			// Unwrap: find inner function_definition, use decorated span.
			for k := uint(0); k < member.ChildCount(); k++ {
				inner := member.Child(k)
				if inner != nil && inner.Kind() == "function_definition" {
					a.extractMethod(member, inner, content, modPath, relPath, className, pf)
					break
				}
			}
		}
	}
}

// extractMethod extracts a method symbol from a class body.
// spanNode determines the line range (may be a decorated_definition).
// defNode is the actual function_definition for field lookup.
func (a *PythonAnalyzer) extractMethod(spanNode, defNode *sitter.Node, content []byte, modPath, relPath, className string, pf *parsedPyFile) {
	methodName := pyNodeFieldText(defNode, "name", content)
	if methodName == "" {
		return
	}

	pf.classes[className] = append(pf.classes[className], methodName)
	pf.symbols = append(pf.symbols, Symbol{
		ID:        pythonSymbolID(modPath, "method", className+"."+methodName),
		Name:      methodName,
		Kind:      "method",
		Language:  "python",
		Package:   modPath,
		FilePath:  relPath,
		LineStart: int(spanNode.StartPosition().Row) + 1,
		LineEnd:   int(spanNode.EndPosition().Row) + 1,
		Signature: extractPySignature(defNode, content),
		Exported:  !strings.HasPrefix(methodName, "_"),
		Receiver:  className,
	})
}

// extractDecorated handles decorated_definition at the top-level scope.
func (a *PythonAnalyzer) extractDecorated(node *sitter.Node, content []byte, modPath, relPath string, pf *parsedPyFile) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "function_definition":
			name := pyNodeFieldText(child, "name", content)
			if name == "" {
				return
			}
			pf.symbols = append(pf.symbols, Symbol{
				ID:        pythonSymbolID(modPath, "function", name),
				Name:      name,
				Kind:      "function",
				Language:  "python",
				Package:   modPath,
				FilePath:  relPath,
				LineStart: int(node.StartPosition().Row) + 1,
				LineEnd:   int(node.EndPosition().Row) + 1,
				Signature: extractPySignature(child, content),
				Exported:  !strings.HasPrefix(name, "_"),
			})
			return
		case "class_definition":
			a.extractClass(child, content, modPath, relPath, pf)
			return
		}
	}
}

// pyNodeFieldText extracts the text content of a named child field.
func pyNodeFieldText(node *sitter.Node, fieldName string, content []byte) string {
	child := node.ChildByFieldName(fieldName)
	if child == nil {
		return ""
	}
	return string(content[child.StartByte():child.EndByte()])
}

// extractPySignature extracts the def line as a signature (up to the colon).
func extractPySignature(node *sitter.Node, content []byte) string {
	text := string(content[node.StartByte():node.EndByte()])
	if idx := strings.Index(text, ":"); idx != -1 {
		return strings.TrimSpace(text[:idx])
	}
	if idx := strings.Index(text, "\n"); idx != -1 {
		return strings.TrimSpace(text[:idx])
	}
	end := len(text)
	if end > 200 {
		end = 200
	}
	return strings.TrimSpace(text[:end])
}

// parsePyImport parses an import or import_from statement node.
func parsePyImport(node *sitter.Node, content []byte) *pyImport {
	text := strings.TrimSpace(string(content[node.StartByte():node.EndByte()]))

	if strings.HasPrefix(text, "from ") {
		// from X import Y, Z
		parts := strings.SplitN(text, " import ", 2)
		if len(parts) != 2 {
			return nil
		}
		modPath := strings.TrimSpace(strings.TrimPrefix(parts[0], "from "))
		if modPath == "" {
			return nil
		}

		// Skip relative imports (start with .)
		if strings.HasPrefix(modPath, ".") {
			return nil
		}

		namesPart := parts[1]
		// Handle parenthesized imports: from X import (Y, Z)
		namesPart = strings.TrimPrefix(namesPart, "(")
		namesPart = strings.TrimSuffix(namesPart, ")")
		namesPart = strings.TrimSpace(namesPart)

		names := strings.Split(namesPart, ",")
		var cleaned []string
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" || n == "*" {
				continue
			}
			// Handle "as" aliases — use the original name
			if idx := strings.Index(n, " as "); idx != -1 {
				n = strings.TrimSpace(n[:idx])
			}
			cleaned = append(cleaned, n)
		}
		return &pyImport{modulePath: modPath, names: cleaned}
	}

	if strings.HasPrefix(text, "import ") {
		modPath := strings.TrimSpace(strings.TrimPrefix(text, "import "))
		if modPath == "" {
			return nil
		}
		// Handle "as" alias
		if idx := strings.Index(modPath, " as "); idx != -1 {
			modPath = strings.TrimSpace(modPath[:idx])
		}
		return &pyImport{modulePath: modPath}
	}

	return nil
}

// --- Import Resolution ---

// resolveImportToFile checks if an import dotpath maps to a file in the project.
func (a *PythonAnalyzer) resolveImportToFile(importPath string, projectFiles map[string]bool) (string, bool) {
	// Convert dotpath to file path: lib.auth -> lib/auth.py
	filePath := strings.ReplaceAll(importPath, ".", "/") + ".py"
	if projectFiles[filePath] {
		return filePath, true
	}

	// Try as package: lib.auth -> lib/auth/__init__.py
	initPath := strings.ReplaceAll(importPath, ".", "/") + "/__init__.py"
	if projectFiles[initPath] {
		return initPath, true
	}

	return "", false
}

// extractImportEdges creates IMPORTS edges from parsed imports.
func (a *PythonAnalyzer) extractImportEdges(pf *parsedPyFile, projectFiles map[string]bool) ([]Edge, []BoundarySymbol) {
	var edges []Edge
	var boundaries []BoundarySymbol

	sourceID := pythonSymbolID(pf.modulePath, "module", pf.modulePath)

	for _, imp := range pf.imports {
		targetPath, isProject := a.resolveImportToFile(imp.modulePath, projectFiles)

		if isProject {
			targetModPath := pythonModulePath(targetPath)
			targetID := pythonSymbolID(targetModPath, "module", targetModPath)
			edges = append(edges, Edge{
				SourceID:   sourceID,
				TargetID:   targetID,
				EdgeType:   "IMPORTS",
				Confidence: 1.0,
			})
		} else {
			targetID := pythonSymbolID(imp.modulePath, "module", imp.modulePath)
			edges = append(edges, Edge{
				SourceID:   sourceID,
				TargetID:   targetID,
				EdgeType:   "IMPORTS",
				Confidence: 1.0,
			})
			boundaries = append(boundaries, BoundarySymbol{
				ID:       targetID,
				Name:     imp.modulePath,
				Kind:     "module",
				Language: "python",
				Package:  imp.modulePath,
			})
		}
	}

	return edges, boundaries
}

// --- Call Edge Extraction ---

// extractCallEdges resolves function calls with calibrated confidence.
func (a *PythonAnalyzer) extractCallEdges(
	pf *parsedPyFile,
	content []byte,
	allSymbols map[string][]Symbol,
) []Edge {
	parser, err := newPythonParser()
	if err != nil {
		slog.Warn("python: failed to create parser for call extraction", "error", err)
		return nil
	}
	defer parser.Close()

	tree := parser.Parse(content, nil)
	defer tree.Close()

	root := tree.RootNode()
	var edges []Edge

	// Build imported names set for this file: name -> source module dotpath
	importedNames := make(map[string]string)
	for _, imp := range pf.imports {
		for _, name := range imp.names {
			importedNames[name] = imp.modulePath
		}
	}

	a.walkForCalls(root, content, pf, importedNames, allSymbols, &edges)
	return edges
}

// walkForCalls recursively walks the AST looking for call expressions.
func (a *PythonAnalyzer) walkForCalls(
	node *sitter.Node,
	content []byte,
	pf *parsedPyFile,
	importedNames map[string]string,
	allSymbols map[string][]Symbol,
	edges *[]Edge,
) {
	if node == nil {
		return
	}

	if node.Kind() == "call" {
		a.resolveCall(node, content, pf, importedNames, allSymbols, edges)
	}

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child != nil {
			a.walkForCalls(child, content, pf, importedNames, allSymbols, edges)
		}
	}
}

// resolveCall attempts to resolve a single call expression to a CALLS edge.
func (a *PythonAnalyzer) resolveCall(
	node *sitter.Node,
	content []byte,
	pf *parsedPyFile,
	importedNames map[string]string,
	allSymbols map[string][]Symbol,
	edges *[]Edge,
) {
	fnNode := node.ChildByFieldName("function")
	if fnNode == nil {
		return
	}

	sourceLine := int(node.StartPosition().Row) + 1
	containingSymbol := findContainingSymbol(pf, sourceLine)
	if containingSymbol == "" {
		return
	}

	switch fnNode.Kind() {
	case "identifier":
		// Bare call: e.g., validate_token(token)
		fnName := string(content[fnNode.StartByte():fnNode.EndByte()])
		if fnName == "" {
			return
		}

		// Only resolve if the name is imported AND unique across the project.
		if _, imported := importedNames[fnName]; !imported {
			return
		}
		matches := allSymbols[fnName]
		if len(matches) != 1 {
			// Multiple matches or zero matches -> ambiguous -> silence
			return
		}
		*edges = append(*edges, Edge{
			SourceID:   containingSymbol,
			TargetID:   matches[0].ID,
			EdgeType:   "CALLS",
			Confidence: 0.85,
			SourceLine: sourceLine,
		})

	case "attribute":
		a.resolveAttributeCall(fnNode, content, pf, importedNames, allSymbols, edges, containingSymbol, sourceLine)
	}
}

// resolveAttributeCall handles obj.method() style calls.
func (a *PythonAnalyzer) resolveAttributeCall(
	fnNode *sitter.Node,
	content []byte,
	pf *parsedPyFile,
	importedNames map[string]string,
	allSymbols map[string][]Symbol,
	edges *[]Edge,
	containingSymbol string,
	sourceLine int,
) {
	objNode := fnNode.ChildByFieldName("object")
	attrNode := fnNode.ChildByFieldName("attribute")
	if objNode == nil || attrNode == nil {
		return
	}

	objName := string(content[objNode.StartByte():objNode.EndByte()])
	methodName := string(content[attrNode.StartByte():attrNode.EndByte()])

	if objName == "self" {
		// Self-method call: self.method()
		// Find containing class for this method
		containingClass := findContainingClass(pf, sourceLine)
		if containingClass == "" {
			return
		}
		methods, ok := pf.classes[containingClass]
		if !ok {
			return
		}
		for _, m := range methods {
			if m == methodName {
				targetID := pythonSymbolID(pf.modulePath, "method", containingClass+"."+methodName)
				*edges = append(*edges, Edge{
					SourceID:   containingSymbol,
					TargetID:   targetID,
					EdgeType:   "CALLS",
					Confidence: 0.95,
					SourceLine: sourceLine,
				})
				return
			}
		}
		return
	}

	// Qualified call: module.func() or import_name.something()
	// Only resolve if the object is a known import name.
	sourceModule, ok := importedNames[objName]
	if !ok {
		return
	}

	matches := allSymbols[methodName]
	for _, m := range matches {
		if m.Package == sourceModule || strings.HasPrefix(m.Package, sourceModule) {
			*edges = append(*edges, Edge{
				SourceID:   containingSymbol,
				TargetID:   m.ID,
				EdgeType:   "CALLS",
				Confidence: 0.7,
				SourceLine: sourceLine,
			})
			return
		}
	}
}

// findContainingSymbol returns the symbol ID of the function/method containing
// the given line. Returns the most specific (innermost) match.
func findContainingSymbol(pf *parsedPyFile, line int) string {
	var best Symbol
	for _, sym := range pf.symbols {
		if sym.Kind != "function" && sym.Kind != "method" {
			continue
		}
		if line >= sym.LineStart && line <= sym.LineEnd {
			if best.ID == "" || sym.LineStart > best.LineStart {
				best = sym
			}
		}
	}
	return best.ID
}

// findContainingClass returns the class name containing the given line.
func findContainingClass(pf *parsedPyFile, line int) string {
	for _, sym := range pf.symbols {
		if sym.Kind != "class" {
			continue
		}
		if line >= sym.LineStart && line <= sym.LineEnd {
			return sym.Name
		}
	}
	return ""
}

// --- Analyze Orchestrator ---

// Analyze extracts all symbols and edges from Python files in the project.
func (a *PythonAnalyzer) Analyze() (*AnalysisResult, error) {
	files, err := a.walkPythonFiles()
	if err != nil {
		return nil, fmt.Errorf("walk python files: %w", err)
	}

	if len(files) == 0 {
		return &AnalysisResult{}, nil
	}

	projectFiles := make(map[string]bool, len(files))
	for _, f := range files {
		projectFiles[f] = true
	}

	// Pass 1: Parse all files, extract symbols and imports.
	var parsedFiles []*parsedPyFile
	fileContents := make(map[string][]byte)
	for _, relPath := range files {
		absPath := filepath.Join(a.projectRoot, relPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			slog.Warn("python: failed to read file", "path", relPath, "error", err)
			continue
		}

		pf, err := a.extractPySymbols(relPath, content)
		if err != nil {
			slog.Warn("python: failed to parse", "path", relPath, "error", err)
			continue
		}
		parsedFiles = append(parsedFiles, pf)
		fileContents[relPath] = content
	}

	// Build global symbol index: name -> []Symbol across entire project.
	allSymbols := make(map[string][]Symbol)
	result := &AnalysisResult{}
	for _, pf := range parsedFiles {
		result.Symbols = append(result.Symbols, pf.symbols...)
		for _, sym := range pf.symbols {
			allSymbols[sym.Name] = append(allSymbols[sym.Name], sym)
		}
	}

	// Pass 2: Extract import edges and call edges.
	for _, pf := range parsedFiles {
		importEdges, boundaries := a.extractImportEdges(pf, projectFiles)
		result.Edges = append(result.Edges, importEdges...)
		result.BoundarySymbols = append(result.BoundarySymbols, boundaries...)

		content, ok := fileContents[pf.relPath]
		if !ok {
			continue
		}

		callEdges := a.extractCallEdges(pf, content, allSymbols)
		result.Edges = append(result.Edges, callEdges...)
	}

	slog.Info("Python analysis complete",
		"files", len(parsedFiles),
		"symbols", len(result.Symbols),
		"edges", len(result.Edges),
		"boundary", len(result.BoundarySymbols),
	)

	return result, nil
}
