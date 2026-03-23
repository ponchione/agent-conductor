package graph

import (
	"fmt"
	"testing"
)

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

func TestBlastRadius_BudgetEnforcement(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Create a star graph: 10 functions all call Center
	syms := []Symbol{
		{ID: "go:p:function:Center", Name: "Center", Kind: "function", Language: "go", Package: "p", FilePath: "center.go", LineStart: 1, LineEnd: 10},
	}
	var edges []Edge
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("go:p:function:Caller%d", i)
		syms = append(syms, Symbol{ID: id, Name: fmt.Sprintf("Caller%d", i), Kind: "function", Language: "go", Package: "p", FilePath: fmt.Sprintf("caller%d.go", i), LineStart: 1, LineEnd: 10})
		edges = append(edges, Edge{SourceID: id, TargetID: "go:p:function:Center", EdgeType: "CALLS", Confidence: 1.0})
	}
	store.InsertSymbols(syms)
	store.InsertEdges(edges)

	result, err := store.BlastRadius(BlastRadiusRequest{
		TargetSymbol:  "go:p:function:Center",
		Direction:     Upstream,
		MaxDepth:      3,
		Budget:        5, // only 5 of the 10 callers
		MinConfidence: 0.5,
	})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if len(result.Upstream) != 5 {
		t.Fatalf("expected budget cap of 5, got %d", len(result.Upstream))
	}
}

func TestBlastRadius_BoundarySymbolsTerminal(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.InsertSymbols([]Symbol{
		{ID: "go:p:function:Foo", Name: "Foo", Kind: "function", Language: "go", Package: "p", FilePath: "foo.go", LineStart: 1, LineEnd: 10},
	})
	store.InsertBoundarySymbols([]BoundarySymbol{
		{ID: "go:fmt:function:Println", Name: "Println", Kind: "function", Language: "go", Package: "fmt"},
	})
	store.InsertEdges([]Edge{
		{SourceID: "go:p:function:Foo", TargetID: "go:fmt:function:Println", EdgeType: "CALLS", Confidence: 1.0},
	})

	result, err := store.BlastRadius(BlastRadiusRequest{
		TargetSymbol:  "go:p:function:Foo",
		Direction:     Downstream,
		MaxDepth:      3,
		Budget:        30,
		MinConfidence: 0.5,
	})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}

	// Boundary symbols are excluded from downstream traversal
	if len(result.Downstream) != 0 {
		t.Fatalf("expected 0 downstream (boundary excluded), got %d", len(result.Downstream))
	}
}

func TestBlastRadius_CycleDetection(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// A->B->C->A (cycle)
	store.InsertSymbols([]Symbol{
		{ID: "go:p:function:A", Name: "A", Kind: "function", Language: "go", Package: "p", FilePath: "a.go", LineStart: 1, LineEnd: 10},
		{ID: "go:p:function:B", Name: "B", Kind: "function", Language: "go", Package: "p", FilePath: "b.go", LineStart: 1, LineEnd: 10},
		{ID: "go:p:function:C", Name: "C", Kind: "function", Language: "go", Package: "p", FilePath: "c.go", LineStart: 1, LineEnd: 10},
	})
	store.InsertEdges([]Edge{
		{SourceID: "go:p:function:A", TargetID: "go:p:function:B", EdgeType: "CALLS", Confidence: 1.0},
		{SourceID: "go:p:function:B", TargetID: "go:p:function:C", EdgeType: "CALLS", Confidence: 1.0},
		{SourceID: "go:p:function:C", TargetID: "go:p:function:A", EdgeType: "CALLS", Confidence: 1.0},
	})

	// Should not infinite loop
	result, err := store.BlastRadius(BlastRadiusRequest{
		TargetSymbol:  "go:p:function:A",
		Direction:     Downstream,
		MaxDepth:      10,
		Budget:        30,
		MinConfidence: 0.5,
	})
	if err != nil {
		t.Fatalf("BlastRadius with cycle: %v", err)
	}

	// Should find B and C but not revisit A
	if len(result.Downstream) != 2 {
		t.Fatalf("expected 2 downstream with cycle, got %d", len(result.Downstream))
	}
}

func TestBlastRadius_MinConfidenceFilter(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.InsertSymbols([]Symbol{
		{ID: "go:p:function:A", Name: "A", Kind: "function", Language: "go", Package: "p", FilePath: "a.go", LineStart: 1, LineEnd: 10},
		{ID: "go:p:function:B", Name: "B", Kind: "function", Language: "go", Package: "p", FilePath: "b.go", LineStart: 1, LineEnd: 10},
		{ID: "go:p:function:C", Name: "C", Kind: "function", Language: "go", Package: "p", FilePath: "c.go", LineStart: 1, LineEnd: 10},
	})
	store.InsertEdges([]Edge{
		{SourceID: "go:p:function:A", TargetID: "go:p:function:B", EdgeType: "CALLS", Confidence: 1.0},
		{SourceID: "go:p:function:A", TargetID: "go:p:function:C", EdgeType: "CALLS", Confidence: 0.3}, // below threshold
	})

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

	// C should be excluded (confidence 0.3 < threshold 0.5)
	if len(result.Downstream) != 1 {
		t.Fatalf("expected 1 downstream (filtered by confidence), got %d", len(result.Downstream))
	}
}

func TestIntegration_FullGraphLifecycle(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Simulate a small Go project:
	// HandleLogin -> CreateSession -> FindUserByEmail
	//                              -> sessions.Create
	//                              -> context.WithTimeout (boundary)
	// HandleRegister -> CreateSession
	// CreateSession implements SessionCreator

	result := &AnalysisResult{
		Symbols: []Symbol{
			{ID: "go:api:function:HandleLogin", Name: "HandleLogin", Kind: "function", Language: "go", Package: "api", FilePath: "internal/api/auth.go", LineStart: 45, LineEnd: 70, Signature: "func HandleLogin(w http.ResponseWriter, r *http.Request)", Exported: true},
			{ID: "go:api:function:HandleRegister", Name: "HandleRegister", Kind: "function", Language: "go", Package: "api", FilePath: "internal/api/auth.go", LineStart: 72, LineEnd: 100, Signature: "func HandleRegister(w http.ResponseWriter, r *http.Request)", Exported: true},
			{ID: "go:auth:method:Service.CreateSession", Name: "CreateSession", Kind: "method", Language: "go", Package: "auth", FilePath: "internal/auth/service.go", LineStart: 45, LineEnd: 82, Signature: "func (s *Service) CreateSession(ctx context.Context, req SessionRequest) (*Session, error)", Exported: true, Receiver: "Service"},
			{ID: "go:auth:method:Repository.FindUserByEmail", Name: "FindUserByEmail", Kind: "method", Language: "go", Package: "auth", FilePath: "internal/auth/repository.go", LineStart: 31, LineEnd: 50, Signature: "func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*User, error)", Exported: true, Receiver: "Repository"},
			{ID: "go:session:method:Store.Create", Name: "Create", Kind: "method", Language: "go", Package: "session", FilePath: "internal/session/store.go", LineStart: 18, LineEnd: 40, Signature: "func (s *Store) Create(ctx context.Context, userID string) (*Session, error)", Exported: true, Receiver: "Store"},
			{ID: "go:auth:interface:SessionCreator", Name: "SessionCreator", Kind: "interface", Language: "go", Package: "auth", FilePath: "internal/auth/interfaces.go", LineStart: 5, LineEnd: 8, Signature: "type SessionCreator interface", Exported: true},
		},
		Edges: []Edge{
			{SourceID: "go:api:function:HandleLogin", TargetID: "go:auth:method:Service.CreateSession", EdgeType: "CALLS", Confidence: 1.0, SourceLine: 55},
			{SourceID: "go:api:function:HandleRegister", TargetID: "go:auth:method:Service.CreateSession", EdgeType: "CALLS", Confidence: 1.0, SourceLine: 85},
			{SourceID: "go:auth:method:Service.CreateSession", TargetID: "go:auth:method:Repository.FindUserByEmail", EdgeType: "CALLS", Confidence: 1.0, SourceLine: 52},
			{SourceID: "go:auth:method:Service.CreateSession", TargetID: "go:session:method:Store.Create", EdgeType: "CALLS", Confidence: 1.0, SourceLine: 60},
			{SourceID: "go:auth:method:Service.CreateSession", TargetID: "go:context:function:WithTimeout", EdgeType: "CALLS", Confidence: 1.0, SourceLine: 48},
			{SourceID: "go:auth:method:Service.CreateSession", TargetID: "go:auth:interface:SessionCreator", EdgeType: "IMPLEMENTS", Confidence: 1.0},
		},
		BoundarySymbols: []BoundarySymbol{
			{ID: "go:context:function:WithTimeout", Name: "WithTimeout", Kind: "function", Language: "go", Package: "context"},
		},
	}

	// Store everything
	if err := store.StoreAnalysisResult(result); err != nil {
		t.Fatalf("StoreAnalysisResult: %v", err)
	}

	// Blast radius on CreateSession
	br, err := store.BlastRadius(BlastRadiusRequest{
		TargetSymbol:  "go:auth:method:Service.CreateSession",
		Direction:     Both,
		MaxDepth:      3,
		Budget:        30,
		MinConfidence: 0.5,
	})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}

	// Upstream: HandleLogin, HandleRegister
	if len(br.Upstream) != 2 {
		t.Errorf("expected 2 upstream callers, got %d", len(br.Upstream))
	}

	// Downstream: FindUserByEmail, Store.Create (boundary excluded)
	if len(br.Downstream) != 2 {
		t.Errorf("expected 2 downstream callees, got %d", len(br.Downstream))
	}

	// Interfaces: SessionCreator
	if len(br.Interfaces) != 1 {
		t.Errorf("expected 1 interface, got %d", len(br.Interfaces))
	}

	// Verify symbols by file
	syms, err := store.GetSymbolsByFile("internal/api/auth.go")
	if err != nil {
		t.Fatalf("GetSymbolsByFile: %v", err)
	}
	if len(syms) != 2 {
		t.Errorf("expected 2 symbols in auth.go, got %d", len(syms))
	}

	// Verify drop and recreate
	if err := store.DropAndRecreate(); err != nil {
		t.Fatalf("DropAndRecreate: %v", err)
	}
	syms, _ = store.GetSymbolsByFile("internal/api/auth.go")
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols after drop, got %d", len(syms))
	}
}
