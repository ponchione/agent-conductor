package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ponchione/agent-conductor/internal/rag"
)

func main() {
	ctx := context.Background()

	// 1. Create embedder pointing at nomic-embed-code
	embedder := rag.NewEmbedder(rag.EmbedderConfig{
		Endpoint:       "http://localhost:8081/v1",
		TimeoutSeconds: 30,
	})

	// 2. Create store in a temp directory
	tmpDir, err := os.MkdirTemp("", "rag-smoke-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	fmt.Printf("LanceDB temp dir: %s\n", tmpDir)

	store, err := rag.NewStore(ctx, tmpDir)
	if err != nil {
		log.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// 3. Define 5 chunks with real-ish signatures from the orchestrator codebase.
	//    Replace these with actual signatures from your code if you want precision.
	chunks := []rag.Chunk{
		{
			ID:          rag.ChunkID("internal/worker/scope.go", "method", "runScope", 45),
			ProjectName: "agent-conductor",
			FilePath:    "internal/worker/scope.go",
			Language:    "go",
			ChunkType:   "method",
			Name:        "runScope",
			Signature:   "func (w *Worker) runScope(ctx context.Context, wf *database.Workflow, wo models.WorkOrder) error",
			Body:        "func (w *Worker) runScope(ctx context.Context, wf *database.Workflow, wo models.WorkOrder) error { assembledCtx := w.assembler.Assemble(ctx, wo); resp, _, err := w.llm.Complete(ctx, templates.ScopePrompt, assembledCtx) }",
			Description: "Runs the scope phase: assembles repository context and calls the local LLM to produce a context package identifying relevant files for the work order.",
			LineStart:   45,
			LineEnd:     92,
			ContentHash: rag.ContentHashOf("scope-body-placeholder"),
			IndexedAt:   time.Now(),
		},
		{
			ID:          rag.ChunkID("internal/worker/build.go", "method", "runBuild", 30),
			ProjectName: "agent-conductor",
			FilePath:    "internal/worker/build.go",
			Language:    "go",
			ChunkType:   "method",
			Name:        "runBuild",
			Signature:   "func (w *Worker) runBuild(ctx context.Context, wf *database.Workflow, wo models.WorkOrder) error",
			Body:        "func (w *Worker) runBuild(ctx context.Context, wf *database.Workflow, wo models.WorkOrder) error { return w.executor.Run(ctx, wf, wo) }",
			Description: "Runs the build phase: invokes OpenCode with the build agent to execute code generation via Gemini using the scoped context package.",
			LineStart:   30,
			LineEnd:     65,
			ContentHash: rag.ContentHashOf("build-body-placeholder"),
			IndexedAt:   time.Now(),
		},
		{
			ID:          rag.ChunkID("internal/worker/verify.go", "method", "runVerify", 25),
			ProjectName: "agent-conductor",
			FilePath:    "internal/worker/verify.go",
			Language:    "go",
			ChunkType:   "method",
			Name:        "runVerify",
			Signature:   "func (w *Worker) runVerify(ctx context.Context, wf *database.Workflow, wo models.WorkOrder) error",
			Body:        "func (w *Worker) runVerify(ctx context.Context, wf *database.Workflow, wo models.WorkOrder) error { diff := w.git.Diff(ctx, wf.Branch); resp, _, err := w.llm.Complete(ctx, templates.VerifyPrompt, diff) }",
			Description: "Runs the verify phase: captures git diff of changes and calls the local LLM to produce a verification report checking scope drift and acceptance criteria.",
			LineStart:   25,
			LineEnd:     70,
			ContentHash: rag.ContentHashOf("verify-body-placeholder"),
			IndexedAt:   time.Now(),
		},
		{
			ID:          rag.ChunkID("internal/llm/client.go", "method", "Complete", 50),
			ProjectName: "agent-conductor",
			FilePath:    "internal/llm/client.go",
			Language:    "go",
			ChunkType:   "method",
			Name:        "Complete",
			Signature:   "func (c *Client) Complete(ctx context.Context, systemPrompt string, userMessage string) (string, Usage, error)",
			Body:        "func (c *Client) Complete(ctx context.Context, systemPrompt string, userMessage string) (string, Usage, error) { reqBody := chatCompletionRequest{Model: c.modelName, Messages: []message{{Role: \"system\", Content: systemPrompt}, {Role: \"user\", Content: userMessage}}} }",
			Description: "Sends a chat completion request to the local LLM via the OpenAI-compatible API and returns the response text with token usage.",
			LineStart:   50,
			LineEnd:     98,
			ContentHash: rag.ContentHashOf("complete-body-placeholder"),
			IndexedAt:   time.Now(),
		},
		{
			ID:          rag.ChunkID("internal/git/git.go", "method", "DiffBranch", 80),
			ProjectName: "agent-conductor",
			FilePath:    "internal/git/git.go",
			Language:    "go",
			ChunkType:   "method",
			Name:        "DiffBranch",
			Signature:   "func (g *GitManager) DiffBranch(ctx context.Context, branch string) (string, error)",
			Body:        "func (g *GitManager) DiffBranch(ctx context.Context, branch string) (string, error) { cmd := exec.CommandContext(ctx, \"git\", \"diff\", \"main...\"+branch); return string(out), nil }",
			Description: "Runs git diff between main and the specified feature branch, returning the unified diff output as a string.",
			LineStart:   80,
			LineEnd:     105,
			ContentHash: rag.ContentHashOf("diff-body-placeholder"),
			IndexedAt:   time.Now(),
		},
	}

	// 4. Embed all chunks (signature + description as embed text)
	embedTexts := make([]string, len(chunks))
	for i, c := range chunks {
		embedTexts[i] = c.Signature + "\n" + c.Description
	}

	fmt.Println("Embedding 5 chunks...")
	embeddings, err := embedder.EmbedBatch(ctx, embedTexts)
	if err != nil {
		log.Fatalf("EmbedBatch failed: %v", err)
	}
	fmt.Printf("Got %d embeddings, dims=%d\n", len(embeddings), len(embeddings[0]))

	// 5. Write to LanceDB
	fmt.Println("Writing chunks to LanceDB...")
	if err := store.UpsertChunks(ctx, chunks, embeddings); err != nil {
		log.Fatalf("UpsertChunks failed: %v", err)
	}
	fmt.Println("Write OK")

	// 6. Query 1: scope-related
	fmt.Println("\n--- Query 1: 'How does the scope phase assemble context?' ---")
	results1, err := store.VectorSearch(ctx, mustEmbedQuery(ctx, embedder, "How does the scope phase assemble context?"), 3)
	if err != nil {
		log.Fatalf("VectorSearch failed: %v", err)
	}
	for i, r := range results1 {
		fmt.Printf("  #%d [%.4f] %s — %s (%s)\n", i+1, r.Score, r.Chunk.FilePath, r.Chunk.Name, r.Chunk.ChunkType)
	}
	expect(results1, "runScope", "Query 1")

	// 7. Query 2: git-related
	fmt.Println("\n--- Query 2: 'git diff between branches' ---")
	results2, err := store.VectorSearch(ctx, mustEmbedQuery(ctx, embedder, "git diff between branches"), 3)
	if err != nil {
		log.Fatalf("VectorSearch failed: %v", err)
	}
	for i, r := range results2 {
		fmt.Printf("  #%d [%.4f] %s — %s (%s)\n", i+1, r.Score, r.Chunk.FilePath, r.Chunk.Name, r.Chunk.ChunkType)
	}
	expect(results2, "DiffBranch", "Query 2")

	// 8. Query 3: bonus — verify-related
	fmt.Println("\n--- Query 3: 'verify acceptance criteria and check scope drift' ---")
	results3, err := store.VectorSearch(ctx, mustEmbedQuery(ctx, embedder, "verify acceptance criteria and check scope drift"), 3)
	if err != nil {
		log.Fatalf("VectorSearch failed: %v", err)
	}
	for i, r := range results3 {
		fmt.Printf("  #%d [%.4f] %s — %s (%s)\n", i+1, r.Score, r.Chunk.FilePath, r.Chunk.Name, r.Chunk.ChunkType)
	}
	expect(results3, "runVerify", "Query 3")

	// 9. Test upsert idempotency — insert same chunks again, count should stay 5
	fmt.Println("\n--- Upsert idempotency test ---")
	if err := store.UpsertChunks(ctx, chunks, embeddings); err != nil {
		log.Fatalf("Second UpsertChunks failed: %v", err)
	}
	// Search with high K to see if we get duplicates
	all, err := store.VectorSearch(ctx, embeddings[0], 10)
	if err != nil {
		log.Fatalf("VectorSearch for idempotency check failed: %v", err)
	}
	fmt.Printf("After double upsert, search returned %d results (expect <=5)\n", len(all))
	if len(all) > 5 {
		fmt.Println("WARNING: Duplicates detected — upsert delete-before-insert may not be working")
	} else {
		fmt.Println("Upsert idempotency OK")
	}

	// 10. Test DeleteByFilePath
	fmt.Println("\n--- DeleteByFilePath test ---")
	if err := store.DeleteByFilePath(ctx, "internal/git/git.go"); err != nil {
		log.Fatalf("DeleteByFilePath failed: %v", err)
	}
	remaining, err := store.ChunksByFilePath(ctx, "internal/git/git.go")
	if err != nil {
		log.Fatalf("ChunksByFilePath after delete failed: %v", err)
	}
	fmt.Printf("Chunks remaining for git.go: %d (expect 0)\n", len(remaining))

	fmt.Println("\n=== SMOKE TEST COMPLETE ===")
}

func mustEmbedQuery(ctx context.Context, e *rag.Embedder, query string) []float32 {
	vec, err := e.EmbedQuery(ctx, query)
	if err != nil {
		log.Fatalf("EmbedQuery failed for %q: %v", query, err)
	}
	return vec
}

func expect(results []rag.SearchResult, expectedName string, label string) {
	if len(results) == 0 {
		fmt.Printf("  ❌ %s: no results returned\n", label)
		return
	}
	if results[0].Chunk.Name == expectedName {
		fmt.Printf("  ✅ %s: top result is %s as expected\n", label, expectedName)
	} else {
		fmt.Printf("  ⚠️  %s: top result is %s, expected %s\n", label, results[0].Chunk.Name, expectedName)
	}
}
