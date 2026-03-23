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
