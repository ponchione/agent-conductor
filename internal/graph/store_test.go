package graph

import "testing"

func TestNewGraphStore_InMemory(t *testing.T) {
	store, err := NewGraphStore(":memory:")
	if err != nil {
		t.Fatalf("NewGraphStore: %v", err)
	}
	defer store.Close()

	// Verify tables exist by querying them
	var count int
	err = store.db.QueryRow("SELECT count(*) FROM symbols").Scan(&count)
	if err != nil {
		t.Fatalf("symbols table not created: %v", err)
	}
	err = store.db.QueryRow("SELECT count(*) FROM edges").Scan(&count)
	if err != nil {
		t.Fatalf("edges table not created: %v", err)
	}
	err = store.db.QueryRow("SELECT count(*) FROM boundary_symbols").Scan(&count)
	if err != nil {
		t.Fatalf("boundary_symbols table not created: %v", err)
	}
	err = store.db.QueryRow("SELECT count(*) FROM chunk_mapping").Scan(&count)
	if err != nil {
		t.Fatalf("chunk_mapping table not created: %v", err)
	}
}

// newTestStore creates an in-memory GraphStore for testing.
func newTestStore(t *testing.T) *GraphStore {
	t.Helper()
	store, err := NewGraphStore(":memory:")
	if err != nil {
		t.Fatalf("NewGraphStore: %v", err)
	}
	return store
}

func TestInsertSymbols(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	syms := []Symbol{
		{ID: "go:pkg:function:Foo", Name: "Foo", Kind: "function", Language: "go", Package: "pkg", FilePath: "pkg/foo.go", LineStart: 10, LineEnd: 20, Signature: "func Foo()", Exported: true},
		{ID: "go:pkg:method:Bar.Baz", Name: "Baz", Kind: "method", Language: "go", Package: "pkg", FilePath: "pkg/bar.go", LineStart: 5, LineEnd: 15, Signature: "func (b *Bar) Baz()", Exported: true, Receiver: "Bar"},
	}

	if err := store.InsertSymbols(syms); err != nil {
		t.Fatalf("InsertSymbols: %v", err)
	}

	var count int
	store.db.QueryRow("SELECT count(*) FROM symbols").Scan(&count)
	if count != 2 {
		t.Fatalf("expected 2 symbols, got %d", count)
	}
}

func TestInsertEdges(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	syms := []Symbol{
		{ID: "go:pkg:function:Foo", Name: "Foo", Kind: "function", Language: "go", Package: "pkg", FilePath: "pkg/foo.go", LineStart: 1, LineEnd: 10},
		{ID: "go:pkg:function:Bar", Name: "Bar", Kind: "function", Language: "go", Package: "pkg", FilePath: "pkg/bar.go", LineStart: 1, LineEnd: 10},
	}
	store.InsertSymbols(syms)

	edges := []Edge{
		{SourceID: "go:pkg:function:Foo", TargetID: "go:pkg:function:Bar", EdgeType: "CALLS", Confidence: 1.0, SourceLine: 5},
	}

	if err := store.InsertEdges(edges); err != nil {
		t.Fatalf("InsertEdges: %v", err)
	}

	var count int
	store.db.QueryRow("SELECT count(*) FROM edges").Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 edge, got %d", count)
	}
}

func TestInsertBoundarySymbols(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	bounds := []BoundarySymbol{
		{ID: "go:context:function:WithTimeout", Name: "WithTimeout", Kind: "function", Language: "go", Package: "context"},
	}

	if err := store.InsertBoundarySymbols(bounds); err != nil {
		t.Fatalf("InsertBoundarySymbols: %v", err)
	}

	var count int
	store.db.QueryRow("SELECT count(*) FROM boundary_symbols").Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 boundary symbol, got %d", count)
	}
}

func TestGetSymbol(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.InsertSymbols([]Symbol{
		{ID: "go:pkg:function:Foo", Name: "Foo", Kind: "function", Language: "go", Package: "pkg", FilePath: "pkg/foo.go", LineStart: 10, LineEnd: 20, Exported: true},
	})

	sym, err := store.GetSymbol("go:pkg:function:Foo")
	if err != nil {
		t.Fatalf("GetSymbol: %v", err)
	}
	if sym.Name != "Foo" {
		t.Fatalf("expected Foo, got %s", sym.Name)
	}
}

func TestGetSymbolsByFile(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.InsertSymbols([]Symbol{
		{ID: "go:pkg:function:Foo", Name: "Foo", Kind: "function", Language: "go", Package: "pkg", FilePath: "pkg/foo.go", LineStart: 1, LineEnd: 10},
		{ID: "go:pkg:function:Bar", Name: "Bar", Kind: "function", Language: "go", Package: "pkg", FilePath: "pkg/foo.go", LineStart: 12, LineEnd: 20},
		{ID: "go:pkg:function:Baz", Name: "Baz", Kind: "function", Language: "go", Package: "pkg", FilePath: "pkg/other.go", LineStart: 1, LineEnd: 10},
	})

	syms, err := store.GetSymbolsByFile("pkg/foo.go")
	if err != nil {
		t.Fatalf("GetSymbolsByFile: %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(syms))
	}
}

func TestGetSymbolsByName(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.InsertSymbols([]Symbol{
		{ID: "go:a:function:Foo", Name: "Foo", Kind: "function", Language: "go", Package: "a", FilePath: "a/foo.go", LineStart: 1, LineEnd: 10},
		{ID: "go:b:function:Foo", Name: "Foo", Kind: "function", Language: "go", Package: "b", FilePath: "b/foo.go", LineStart: 1, LineEnd: 10},
	})

	syms, err := store.GetSymbolsByName("Foo")
	if err != nil {
		t.Fatalf("GetSymbolsByName: %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(syms))
	}
}

func TestGetEdgesFrom(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.InsertSymbols([]Symbol{
		{ID: "go:pkg:function:A", Name: "A", Kind: "function", Language: "go", Package: "pkg", FilePath: "a.go", LineStart: 1, LineEnd: 10},
		{ID: "go:pkg:function:B", Name: "B", Kind: "function", Language: "go", Package: "pkg", FilePath: "b.go", LineStart: 1, LineEnd: 10},
	})
	store.InsertEdges([]Edge{
		{SourceID: "go:pkg:function:A", TargetID: "go:pkg:function:B", EdgeType: "CALLS", Confidence: 1.0},
	})

	edges, err := store.GetEdgesFrom("go:pkg:function:A")
	if err != nil {
		t.Fatalf("GetEdgesFrom: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
}

func TestGetEdgesTo(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.InsertSymbols([]Symbol{
		{ID: "go:pkg:function:A", Name: "A", Kind: "function", Language: "go", Package: "pkg", FilePath: "a.go", LineStart: 1, LineEnd: 10},
		{ID: "go:pkg:function:B", Name: "B", Kind: "function", Language: "go", Package: "pkg", FilePath: "b.go", LineStart: 1, LineEnd: 10},
	})
	store.InsertEdges([]Edge{
		{SourceID: "go:pkg:function:A", TargetID: "go:pkg:function:B", EdgeType: "CALLS", Confidence: 1.0},
	})

	edges, err := store.GetEdgesTo("go:pkg:function:B")
	if err != nil {
		t.Fatalf("GetEdgesTo: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
}

func TestChunkMapping(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.InsertSymbols([]Symbol{
		{ID: "go:pkg:function:Foo", Name: "Foo", Kind: "function", Language: "go", Package: "pkg", FilePath: "foo.go", LineStart: 1, LineEnd: 10},
	})

	if err := store.InsertChunkMappings("go:pkg:function:Foo", []string{"chunk-abc", "chunk-def"}); err != nil {
		t.Fatalf("InsertChunkMappings: %v", err)
	}

	chunks, err := store.GetChunkMappingsForSymbol("go:pkg:function:Foo")
	if err != nil {
		t.Fatalf("GetChunkMappingsForSymbol: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunk mappings, got %d", len(chunks))
	}
}

func TestSetAndGetMeta(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	if err := store.SetMeta("indexed_at", "2026-03-23T10:00:00Z"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	val, err := store.GetMeta("indexed_at")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if val != "2026-03-23T10:00:00Z" {
		t.Fatalf("expected timestamp, got %s", val)
	}
}

func TestDropAndRecreate(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Insert data
	store.InsertSymbols([]Symbol{
		{ID: "go:pkg:function:Foo", Name: "Foo", Kind: "function", Language: "go", Package: "pkg", FilePath: "foo.go", LineStart: 1, LineEnd: 10},
	})

	var count int
	store.db.QueryRow("SELECT count(*) FROM symbols").Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 symbol before drop, got %d", count)
	}

	// Drop and recreate
	if err := store.DropAndRecreate(); err != nil {
		t.Fatalf("DropAndRecreate: %v", err)
	}

	// Verify tables exist but are empty
	store.db.QueryRow("SELECT count(*) FROM symbols").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 symbols after drop, got %d", count)
	}
}
