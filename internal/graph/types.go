package graph

// Symbol represents a named code entity in the project source tree.
type Symbol struct {
	ID        string // deterministic: {language}:{package}:{kind}:{name}
	Name      string
	Kind      string // function, method, type, interface, class, component, module
	Language  string // go, typescript, python
	Package   string // Go: full package path. TS: module path. Python: module dotpath.
	FilePath  string // relative to project root
	LineStart int
	LineEnd   int
	Signature string
	Exported  bool
	Receiver  string // Go methods only: receiver type name
}

// Edge represents a structural relationship between two symbols.
type Edge struct {
	SourceID   string  // FK to Symbol.ID
	TargetID   string  // FK to Symbol.ID or BoundarySymbol.ID
	EdgeType   string  // CALLS, IMPORTS, IMPLEMENTS, EMBEDS, EXTENDS, INSTANTIATES
	Confidence float64 // 0.0-1.0
	SourceLine int     // line in source file where reference occurs
	Metadata   string  // JSON blob for edge-type-specific data
}

// BoundarySymbol is an external symbol (stdlib, dependencies) that
// acts as a terminal node in blast radius queries.
type BoundarySymbol struct {
	ID       string
	Name     string
	Kind     string
	Language string
	Package  string
}

// Direction controls blast radius traversal direction.
type Direction int

const (
	Upstream   Direction = iota // what depends on this symbol (callers, importers)
	Downstream                 // what this symbol depends on (callees, imports)
	Both
)

// BlastRadiusRequest configures a blast radius query.
type BlastRadiusRequest struct {
	TargetSymbol  string    // symbol ID or name (fuzzy-matched)
	Direction     Direction
	MaxDepth      int       // default: 3
	Budget        int       // default: 30
	MinConfidence float64   // default: 0.5
	EdgeTypes     []string  // filter: only these edge types (default: all)
	IncludeTests  bool      // include test files (default: false)
}

// BlastRadiusResult holds the complete blast radius for a target symbol.
type BlastRadiusResult struct {
	Target     Symbol
	Upstream   []BlastRadiusNode // sorted by depth ASC, confidence DESC
	Downstream []BlastRadiusNode
	Interfaces []Symbol          // interfaces this symbol satisfies
}

// BlastRadiusNode is a single node in the blast radius graph.
type BlastRadiusNode struct {
	Symbol     Symbol
	Depth      int      // hops from target
	EdgeType   string   // how this node connects
	Confidence float64  // edge confidence
	Path       []string // symbol IDs forming path from target
}

// AnalysisResult holds the output of a language analyzer.
type AnalysisResult struct {
	Symbols         []Symbol
	Edges           []Edge
	BoundarySymbols []BoundarySymbol
}

// Merge combines another AnalysisResult into this one.
func (r *AnalysisResult) Merge(other *AnalysisResult) {
	r.Symbols = append(r.Symbols, other.Symbols...)
	r.Edges = append(r.Edges, other.Edges...)
	r.BoundarySymbols = append(r.BoundarySymbols, other.BoundarySymbols...)
}

// GraphQuerier is the read interface consumed by scope integration.
type GraphQuerier interface {
	BlastRadius(req BlastRadiusRequest) (*BlastRadiusResult, error)
	GetSymbolsByFile(filePath string) ([]Symbol, error)
	GetSymbolsByName(name string) ([]Symbol, error)
	GetChunkMappingsForSymbol(symbolID string) ([]string, error)
}
