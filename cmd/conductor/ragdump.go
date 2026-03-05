package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/rag"
	"github.com/spf13/cobra"
)

var (
	ragDumpStats bool
	ragDumpFile  string
	ragDumpName  string
)

var ragDumpCmd = &cobra.Command{
	Use:   "rag-dump",
	Short: "Inspect the RAG database contents",
	Long: `Dump and inspect what's stored in the RAG vector database.

Three mutually exclusive modes:
  --stats          Aggregate statistics (languages, types, top files)
  --file <path>    Show all chunks for a specific file
  --name <symbol>  Show all chunks matching a symbol name`,
	RunE: runRAGDump,
}

func init() {
	ragDumpCmd.Flags().BoolVar(&ragDumpStats, "stats", false, "Show aggregate database statistics")
	ragDumpCmd.Flags().StringVar(&ragDumpFile, "file", "", "Show chunks for a specific file path")
	ragDumpCmd.Flags().StringVar(&ragDumpName, "name", "", "Show chunks matching a symbol name")
}

func runRAGDump(cmd *cobra.Command, args []string) error {
	flagCount := 0
	if ragDumpStats {
		flagCount++
	}
	if ragDumpFile != "" {
		flagCount++
	}
	if ragDumpName != "" {
		flagCount++
	}
	if flagCount == 0 {
		return fmt.Errorf("one of --stats, --file, or --name is required")
	}
	if flagCount > 1 {
		return fmt.Errorf("--stats, --file, and --name are mutually exclusive")
	}

	if err := config.EnsureDataDirs(cfg); err != nil {
		return err
	}

	ctx := context.Background()
	store, err := rag.NewStore(ctx, filepath.Join(cfg.Project.DataDir, "rag"))
	if err != nil {
		return fmt.Errorf("open RAG store: %w", err)
	}
	defer store.Close()

	switch {
	case ragDumpStats:
		return dumpStats(ctx, store)
	case ragDumpFile != "":
		return dumpFile(ctx, store, ragDumpFile)
	case ragDumpName != "":
		return dumpName(ctx, store, ragDumpName)
	}
	return nil
}

func dumpStats(ctx context.Context, store *rag.Store) error {
	count, err := store.Count(ctx)
	if err != nil {
		return fmt.Errorf("count: %w", err)
	}

	chunks, err := store.SelectMetadata(ctx)
	if err != nil {
		return fmt.Errorf("select metadata: %w", err)
	}

	fmt.Println("RAG Database Stats")
	fmt.Printf("  Total chunks: %d\n", count)

	if len(chunks) > 0 {
		latest := chunks[0].IndexedAt
		for _, c := range chunks[1:] {
			if c.IndexedAt.After(latest) {
				latest = c.IndexedAt
			}
		}
		fmt.Printf("  Indexed at:   %s\n", latest.Format("2006-01-02 15:04:05"))
	}

	// By language
	langCounts := map[string]int{}
	typeCounts := map[string]int{}
	fileCounts := map[string]int{}
	for _, c := range chunks {
		langCounts[c.Language]++
		typeCounts[c.ChunkType]++
		fileCounts[c.FilePath]++
	}

	fmt.Println()
	fmt.Println("By language:")
	printSortedCounts(langCounts)

	fmt.Println()
	fmt.Println("By chunk type:")
	printSortedCounts(typeCounts)

	fmt.Println()
	fmt.Println("Top 10 files by chunk count:")
	printTopFiles(fileCounts, 10)

	return nil
}

func dumpFile(ctx context.Context, store *rag.Store, filePath string) error {
	chunks, err := store.ChunksByFilePath(ctx, filePath)
	if err != nil {
		return fmt.Errorf("query file: %w", err)
	}
	if len(chunks) == 0 {
		fmt.Printf("No chunks found for %s\n", filePath)
		return nil
	}

	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].LineStart < chunks[j].LineStart
	})

	fmt.Printf("%s (%d chunks)\n", filePath, len(chunks))
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	for _, c := range chunks {
		desc := c.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Fprintf(w, "  L%d-%d\t%s\t%s\t%q\n", c.LineStart, c.LineEnd, c.ChunkType, c.Name, desc)
	}
	w.Flush()
	return nil
}

func dumpName(ctx context.Context, store *rag.Store, name string) error {
	chunks, err := store.ChunksByName(ctx, name)
	if err != nil {
		return fmt.Errorf("query name: %w", err)
	}
	if len(chunks) == 0 {
		fmt.Printf("No chunks named %q\n", name)
		return nil
	}

	fmt.Printf("Chunks named %q:\n", name)
	for _, c := range chunks {
		desc := c.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Printf("  %s:%d-%d  %s  %q\n", c.FilePath, c.LineStart, c.LineEnd, c.ChunkType, desc)

		if len(c.Calls) > 0 {
			names := make([]string, len(c.Calls))
			for i, ref := range c.Calls {
				names[i] = ref.Name
			}
			fmt.Printf("    calls:     [%s]\n", strings.Join(names, ", "))
		}
		if len(c.CalledBy) > 0 {
			names := make([]string, len(c.CalledBy))
			for i, ref := range c.CalledBy {
				names[i] = ref.Name
			}
			fmt.Printf("    called_by: [%s]\n", strings.Join(names, ", "))
		}
	}
	return nil
}

type countEntry struct {
	key   string
	count int
}

func printSortedCounts(m map[string]int) {
	entries := make([]countEntry, 0, len(m))
	for k, v := range m {
		entries = append(entries, countEntry{k, v})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	for _, e := range entries {
		fmt.Fprintf(w, "  %s\t%d\n", e.key, e.count)
	}
	w.Flush()
}

func printTopFiles(m map[string]int, limit int) {
	entries := make([]countEntry, 0, len(m))
	for k, v := range m {
		entries = append(entries, countEntry{k, v})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	for _, e := range entries {
		fmt.Fprintf(w, "  %s\t%d\n", e.key, e.count)
	}
	w.Flush()
}
