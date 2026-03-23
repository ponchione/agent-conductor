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
