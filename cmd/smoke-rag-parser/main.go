//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/ponchione/agent-conductor/internal/rag"
)

func main() {
	content, err := os.ReadFile("internal/llm/client.go")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
		os.Exit(1)
	}

	parser := rag.NewTreeSitterParser()
	chunks, err := parser.ParseFile("internal/llm/client.go", "go", content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ParseFile error: %v\n", err)
		os.Exit(1)
	}

	typeCounts := make(map[string]int)
	for _, c := range chunks {
		typeCounts[c.ChunkType]++
		fmt.Printf("Name:      %s\n", c.Name)
		fmt.Printf("ChunkType: %s\n", c.ChunkType)
		fmt.Printf("Signature: %s\n", c.Signature)
		fmt.Printf("Lines:     %d-%d\n", c.LineStart, c.LineEnd)
		fmt.Println("---")
	}

	fmt.Printf("\nTotal chunks: %d\n", len(chunks))
	for t, n := range typeCounts {
		fmt.Printf("  %s: %d\n", t, n)
	}

	// Assertions
	ok := true
	if typeCounts["function"] < 1 {
		fmt.Fprintln(os.Stderr, "FAIL: expected at least one 'function' chunk")
		ok = false
	}
	if typeCounts["method"] < 1 {
		fmt.Fprintln(os.Stderr, "FAIL: expected at least one 'method' chunk")
		ok = false
	}
	if !ok {
		os.Exit(1)
	}
	fmt.Println("PASS: assertions satisfied")
}
