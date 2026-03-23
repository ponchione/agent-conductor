package graph

import "testing"

// buildTestGraph creates: A->B->C, D->B (two callers of B, B calls C)
func buildTestGraph(t *testing.T) *GraphStore {
	t.Helper()
	store := newTestStore(t)

	store.InsertSymbols([]Symbol{
		{ID: "go:p:function:A", Name: "A", Kind: "function", Language: "go", Package: "p", FilePath: "a.go", LineStart: 1, LineEnd: 10, Signature: "func A()"},
		{ID: "go:p:function:B", Name: "B", Kind: "function", Language: "go", Package: "p", FilePath: "b.go", LineStart: 1, LineEnd: 10, Signature: "func B()"},
		{ID: "go:p:function:C", Name: "C", Kind: "function", Language: "go", Package: "p", FilePath: "c.go", LineStart: 1, LineEnd: 10, Signature: "func C()"},
		{ID: "go:p:function:D", Name: "D", Kind: "function", Language: "go", Package: "p", FilePath: "d.go", LineStart: 1, LineEnd: 10, Signature: "func D()"},
	})

	store.InsertEdges([]Edge{
		{SourceID: "go:p:function:A", TargetID: "go:p:function:B", EdgeType: "CALLS", Confidence: 1.0},
		{SourceID: "go:p:function:D", TargetID: "go:p:function:B", EdgeType: "CALLS", Confidence: 1.0},
		{SourceID: "go:p:function:B", TargetID: "go:p:function:C", EdgeType: "CALLS", Confidence: 1.0},
	})

	return store
}

func TestBlastRadius_Upstream(t *testing.T) {
	store := buildTestGraph(t)
	defer store.Close()

	result, err := store.BlastRadius(BlastRadiusRequest{
		TargetSymbol:  "go:p:function:B",
		Direction:     Upstream,
		MaxDepth:      3,
		Budget:        30,
		MinConfidence: 0.5,
	})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}

	if result.Target.Name != "B" {
		t.Fatalf("expected target B, got %s", result.Target.Name)
	}

	if len(result.Upstream) != 2 {
		t.Fatalf("expected 2 upstream (A, D), got %d", len(result.Upstream))
	}

	// Both should be depth 1
	for _, node := range result.Upstream {
		if node.Depth != 1 {
			t.Errorf("expected depth 1 for %s, got %d", node.Symbol.Name, node.Depth)
		}
	}
}

func TestBlastRadius_Downstream(t *testing.T) {
	store := buildTestGraph(t)
	defer store.Close()

	result, err := store.BlastRadius(BlastRadiusRequest{
		TargetSymbol:  "go:p:function:A",
		Direction:     Downstream,
		MaxDepth:      3,
		Budget:        30,
		MinConfidence: 0.5,
	})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}

	// A calls B, B calls C -> downstream should be B (depth 1), C (depth 2)
	if len(result.Downstream) != 2 {
		t.Fatalf("expected 2 downstream (B, C), got %d", len(result.Downstream))
	}

	if result.Downstream[0].Symbol.Name != "B" {
		t.Errorf("expected first downstream to be B, got %s", result.Downstream[0].Symbol.Name)
	}
	if result.Downstream[0].Depth != 1 {
		t.Errorf("expected B at depth 1, got %d", result.Downstream[0].Depth)
	}
	if result.Downstream[1].Symbol.Name != "C" {
		t.Errorf("expected second downstream to be C, got %s", result.Downstream[1].Symbol.Name)
	}
	if result.Downstream[1].Depth != 2 {
		t.Errorf("expected C at depth 2, got %d", result.Downstream[1].Depth)
	}
}

func TestBlastRadius_Both(t *testing.T) {
	store := buildTestGraph(t)
	defer store.Close()

	result, err := store.BlastRadius(BlastRadiusRequest{
		TargetSymbol:  "go:p:function:B",
		Direction:     Both,
		MaxDepth:      3,
		Budget:        30,
		MinConfidence: 0.5,
	})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}

	// Upstream: A, D call B
	if len(result.Upstream) != 2 {
		t.Fatalf("expected 2 upstream, got %d", len(result.Upstream))
	}
	// Downstream: B calls C
	if len(result.Downstream) != 1 {
		t.Fatalf("expected 1 downstream, got %d", len(result.Downstream))
	}
	if result.Downstream[0].Symbol.Name != "C" {
		t.Errorf("expected downstream C, got %s", result.Downstream[0].Symbol.Name)
	}
}

func TestBlastRadius_Interfaces(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.InsertSymbols([]Symbol{
		{ID: "go:p:type:MyService", Name: "MyService", Kind: "type", Language: "go", Package: "p", FilePath: "svc.go", LineStart: 1, LineEnd: 10},
		{ID: "go:p:interface:ServiceIface", Name: "ServiceIface", Kind: "interface", Language: "go", Package: "p", FilePath: "iface.go", LineStart: 1, LineEnd: 5},
	})
	store.InsertEdges([]Edge{
		{SourceID: "go:p:type:MyService", TargetID: "go:p:interface:ServiceIface", EdgeType: "IMPLEMENTS", Confidence: 1.0},
	})

	result, err := store.BlastRadius(BlastRadiusRequest{
		TargetSymbol:  "go:p:type:MyService",
		Direction:     Both,
		MaxDepth:      3,
		Budget:        30,
		MinConfidence: 0.5,
	})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}

	if len(result.Interfaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(result.Interfaces))
	}
	if result.Interfaces[0].Name != "ServiceIface" {
		t.Errorf("expected ServiceIface, got %s", result.Interfaces[0].Name)
	}
}
