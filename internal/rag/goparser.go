package rag

import (
	"fmt"
	"go/ast"
	"go/token"
	goTypes "go/types"
	"log/slog"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// GoASTParser uses go/packages and go/ast to extract rich relationship metadata
// from Go source files. Non-Go files are delegated to TreeSitterParser.
type GoASTParser struct {
	fset       *token.FileSet
	pkgsByFile map[string]*packages.Package // abs file path → package
	allIfaces  []ifaceInfo
	treeSitter *TreeSitterParser
}

// ifaceInfo holds a named interface type for Implements checking.
type ifaceInfo struct {
	name    string // "package.InterfaceName"
	ifaceT  *goTypes.Interface
}

// NewGoASTParser loads all Go packages under rootDir and builds lookup indexes.
// This is expensive once (~seconds) but cheap per-file thereafter.
func NewGoASTParser(rootDir string) (*GoASTParser, error) {
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedDeps |
			packages.NeedImports,
		Dir:  rootDir,
		Fset: fset,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("go/packages load: %w", err)
	}

	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			slog.Warn("go/packages error", "pkg", pkg.PkgPath, "error", e)
		}
	}

	pkgsByFile := make(map[string]*packages.Package)
	var allIfaces []ifaceInfo

	packages.Visit(pkgs, func(pkg *packages.Package) bool {
		if pkg.Types == nil {
			return true
		}

		for _, f := range pkg.GoFiles {
			abs, err := filepath.Abs(f)
			if err == nil {
				pkgsByFile[abs] = pkg
			}
		}

		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*goTypes.TypeName)
			if !ok {
				continue
			}
			iface, ok := tn.Type().Underlying().(*goTypes.Interface)
			if !ok {
				continue
			}
			if iface.NumMethods() == 0 {
				continue // skip empty interfaces
			}
			allIfaces = append(allIfaces, ifaceInfo{
				name:   pkg.PkgPath + "." + name,
				ifaceT: iface,
			})
		}

		return true
	}, nil)

	slog.Info("GoASTParser initialized",
		"packages", len(pkgsByFile),
		"interfaces", len(allIfaces),
	)

	return &GoASTParser{
		fset:       fset,
		pkgsByFile: pkgsByFile,
		allIfaces:  allIfaces,
		treeSitter: NewTreeSitterParser(),
	}, nil
}

// ParseFile extracts chunks from a Go file with rich metadata, or delegates to
// tree-sitter for non-Go files or files not found in loaded packages.
func (p *GoASTParser) ParseFile(filePath, language string, content []byte) ([]RawChunk, error) {
	if language != "go" {
		return p.treeSitter.ParseFile(filePath, language, content)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return p.treeSitter.ParseFile(filePath, language, content)
	}

	pkg, ok := p.pkgsByFile[absPath]
	if !ok || pkg.TypesInfo == nil {
		slog.Debug("file not in loaded packages, falling back to tree-sitter", "path", filePath)
		return p.treeSitter.ParseFile(filePath, language, content)
	}

	var astFile *ast.File
	for _, f := range pkg.Syntax {
		pos := p.fset.Position(f.Pos())
		fAbs, _ := filepath.Abs(pos.Filename)
		if fAbs == absPath {
			astFile = f
			break
		}
	}
	if astFile == nil {
		return p.treeSitter.ParseFile(filePath, language, content)
	}

	fileImports := extractImports(astFile)

	var chunks []RawChunk
	for _, decl := range astFile.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			chunk := p.parseFuncDecl(d, pkg, content, fileImports)
			if chunk != nil {
				chunks = append(chunks, *chunk)
			}
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				typeChunks := p.parseGenDecl(d, pkg, content, fileImports)
				chunks = append(chunks, typeChunks...)
			}
		}
	}

	return chunks, nil
}

// parseFuncDecl extracts a function/method chunk with call graph information.
func (p *GoASTParser) parseFuncDecl(
	decl *ast.FuncDecl,
	pkg *packages.Package,
	content []byte,
	fileImports []string,
) *RawChunk {
	name := decl.Name.Name
	if name == "" {
		return nil
	}

	chunkType := "function"
	if decl.Recv != nil && decl.Recv.NumFields() > 0 {
		chunkType = "method"
	}

	startOff := p.fset.Position(decl.Pos()).Offset
	endOff := p.fset.Position(decl.End()).Offset
	if endOff > len(content) {
		endOff = len(content)
	}

	body := string(content[startOff:endOff])

	var sig string
	if decl.Body != nil {
		bodyOff := p.fset.Position(decl.Body.Pos()).Offset
		sig = strings.TrimRight(string(content[startOff:bodyOff]), " \t\n\r")
	} else {
		sig = strings.TrimRight(body, " \t\n\r")
	}

	if len(body) > MaxBodyLength {
		body = body[:MaxBodyLength]
	}

	calls := p.extractCalls(decl.Body, pkg)

	startPos := p.fset.Position(decl.Pos())
	endPos := p.fset.Position(decl.End())

	return &RawChunk{
		Name:      name,
		Signature: sig,
		Body:      body,
		ChunkType: chunkType,
		LineStart: startPos.Line,
		LineEnd:   endPos.Line,
		Calls:     calls,
		Imports:   fileImports,
	}
}

// extractCalls walks a function body and collects called functions/methods.
func (p *GoASTParser) extractCalls(body *ast.BlockStmt, pkg *packages.Package) []FuncRef {
	if body == nil || pkg.TypesInfo == nil {
		return nil
	}

	seen := make(map[string]bool)
	var calls []FuncRef

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		var ref *FuncRef

		switch fn := call.Fun.(type) {
		case *ast.Ident:
			// Local call: e.g., doSomething()
			ref = p.resolveIdent(fn, pkg)

		case *ast.SelectorExpr:
			// Qualified call: e.g., pkg.Func() or obj.Method()
			ref = p.resolveSelector(fn, pkg)
		}

		if ref != nil {
			key := ref.Package + "." + ref.Name
			if !seen[key] {
				seen[key] = true
				calls = append(calls, *ref)
			}
		}

		return true
	})

	return calls
}

// resolveIdent resolves a simple identifier call (e.g., doSomething()).
func (p *GoASTParser) resolveIdent(ident *ast.Ident, pkg *packages.Package) *FuncRef {
	obj, ok := pkg.TypesInfo.Uses[ident]
	if !ok {
		return nil
	}

	fn, ok := obj.(*goTypes.Func)
	if !ok {
		return nil
	}

	pkgPath := ""
	if fn.Pkg() != nil {
		pkgPath = fn.Pkg().Path()
	}

	return &FuncRef{
		Name:    fn.Name(),
		Package: pkgPath,
	}
}

// resolveSelector resolves a selector expression call (e.g., pkg.Func() or obj.Method()).
func (p *GoASTParser) resolveSelector(sel *ast.SelectorExpr, pkg *packages.Package) *FuncRef {
	if xIdent, ok := sel.X.(*ast.Ident); ok {
		if xObj, ok := pkg.TypesInfo.Uses[xIdent]; ok {
			if pkgName, ok := xObj.(*goTypes.PkgName); ok {
				return &FuncRef{
					Name:    sel.Sel.Name,
					Package: pkgName.Imported().Path(),
				}
			}
		}
	}

	selObj, ok := pkg.TypesInfo.Uses[sel.Sel]
	if !ok {
		return nil
	}

	fn, ok := selObj.(*goTypes.Func)
	if !ok {
		return nil
	}

	pkgPath := ""
	if fn.Pkg() != nil {
		pkgPath = fn.Pkg().Path()
	}

	return &FuncRef{
		Name:    fn.Name(),
		Package: pkgPath,
	}
}

// parseGenDecl extracts type declaration chunks with TypesUsed and Implements metadata.
func (p *GoASTParser) parseGenDecl(
	decl *ast.GenDecl,
	pkg *packages.Package,
	content []byte,
	fileImports []string,
) []RawChunk {
	var chunks []RawChunk

	for _, spec := range decl.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}

		name := ts.Name.Name
		if name == "" {
			continue
		}

		startNode := ast.Node(decl)
		if len(decl.Specs) > 1 {
			startNode = ts
		}

		startOff := p.fset.Position(startNode.Pos()).Offset
		endOff := p.fset.Position(startNode.End()).Offset
		if endOff > len(content) {
			endOff = len(content)
		}

		body := string(content[startOff:endOff])

		sig := body
		if idx := strings.Index(sig, "{"); idx != -1 {
			sig = strings.TrimRight(sig[:idx], " \t\n\r")
		} else if idx := strings.Index(sig, "\n"); idx != -1 {
			sig = sig[:idx]
		}

		if len(body) > MaxBodyLength {
			body = body[:MaxBodyLength]
		}

		typesUsed := p.extractTypesUsed(ts, pkg)
		implements := p.checkImplements(name, pkg)

		startPos := p.fset.Position(startNode.Pos())
		endPos := p.fset.Position(startNode.End())

		chunks = append(chunks, RawChunk{
			Name:       name,
			Signature:  sig,
			Body:       body,
			ChunkType:  "type",
			LineStart:  startPos.Line,
			LineEnd:    endPos.Line,
			TypesUsed:  typesUsed,
			Implements: implements,
			Imports:    fileImports,
		})
	}

	return chunks
}

// extractTypesUsed walks a type spec and collects all referenced type names.
func (p *GoASTParser) extractTypesUsed(ts *ast.TypeSpec, pkg *packages.Package) []string {
	if pkg.TypesInfo == nil {
		return nil
	}

	seen := make(map[string]bool)
	var types []string

	ast.Inspect(ts.Type, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}

		obj, ok := pkg.TypesInfo.Uses[ident]
		if !ok {
			return true
		}

		tn, ok := obj.(*goTypes.TypeName)
		if !ok {
			return true
		}

		qualName := tn.Name()
		if tn.Pkg() != nil {
			qualName = tn.Pkg().Path() + "." + tn.Name()
		}

		if !seen[qualName] {
			seen[qualName] = true
			types = append(types, qualName)
		}

		return true
	})

	return types
}

// checkImplements checks which collected interfaces the named type satisfies.
func (p *GoASTParser) checkImplements(typeName string, pkg *packages.Package) []string {
	if pkg.Types == nil {
		return nil
	}

	obj := pkg.Types.Scope().Lookup(typeName)
	if obj == nil {
		return nil
	}

	typ := obj.Type()
	ptrTyp := goTypes.NewPointer(typ)

	var implements []string
	for _, iface := range p.allIfaces {
		if goTypes.Implements(typ, iface.ifaceT) || goTypes.Implements(ptrTyp, iface.ifaceT) {
			implements = append(implements, iface.name)
		}
	}

	return implements
}

// extractImports returns the import paths from an AST file.
func extractImports(f *ast.File) []string {
	imports := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		imports = append(imports, path)
	}
	return imports
}
