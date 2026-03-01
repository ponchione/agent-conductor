//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ponchione/agent-conductor/internal/rag"
)

func main() {
	ctx := context.Background()
	store, err := rag.NewStore(ctx, "/home/gernsback/source/.conductor/projects/my-website/rag")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	embedder := rag.NewEmbedder(rag.EmbedderConfig{
		Endpoint: "http://localhost:8081/v1", TimeoutSeconds: 30,
	})

	vec, err := embedder.EmbedQuery(ctx, "navigation sidebar component")
	if err != nil {
		log.Fatal(err)
	}

	results, err := store.VectorSearch(ctx, vec, 5)
	if err != nil {
		log.Fatal(err)
	}

	for i, r := range results {
		fmt.Printf("#%d [%.4f] %s — %s\n  %s\n\n", i+1, r.Score, r.Chunk.FilePath, r.Chunk.Name, r.Chunk.Description)
	}
}
