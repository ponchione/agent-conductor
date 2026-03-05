package context

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/util"
)

// RAGResult is a single structured search result from the RAG index.
type RAGResult struct {
	Function    string
	File        string
	Description string
}

// RAGSearcher performs semantic search and returns structured results.
// Defined here so the context package stays free of CGo dependencies.
type RAGSearcher interface {
	SearchStructured(ctx context.Context, query string, topK int) ([]RAGResult, error)
}

// EnrichedRAGResult is a search result enriched with relationship metadata
// and multi-query scoring from work-order-aware search.
type EnrichedRAGResult struct {
	Function        string
	File            string
	Description     string
	Calls           []models.CodeRef
	CalledBy        []models.CodeRef
	IsDependencyHop bool
	QueryHitCount   int
	Score           float32
}

// RAGWorkOrderSearcher performs multi-query search driven by work order fields.
// Separate from RAGSearcher to avoid breaking existing consumers.
type RAGWorkOrderSearcher interface {
	SearchForWorkOrder(ctx context.Context, wo *models.WorkOrder, maxResults int) ([]EnrichedRAGResult, error)
}

// Assembler gathers and formats context for the LLM.
type Assembler struct {
	cfg      *config.ProjectConfig
	searcher RAGSearcher // nil if RAG not configured
}

// NewAssembler creates a new context assembler. searcher may be nil.
func NewAssembler(cfg *config.ProjectConfig, searcher RAGSearcher) *Assembler {
	return &Assembler{
		cfg:      cfg,
		searcher: searcher,
	}
}

// searchRelevantCode performs RAG search using the best available method.
// It tries RAGWorkOrderSearcher first (multi-query expansion), falling back
// to the basic RAGSearcher interface.
func (a *Assembler) searchRelevantCode(ctx context.Context, wo *models.WorkOrder) []models.RelevantCode {
	if a.searcher == nil {
		return nil
	}

	if wos, ok := a.searcher.(RAGWorkOrderSearcher); ok {
		maxResults := 30
		if a.cfg.Index.MaxRAGResults > 0 {
			maxResults = a.cfg.Index.MaxRAGResults
		}
		enriched, err := wos.SearchForWorkOrder(ctx, wo, maxResults)
		if err != nil {
			slog.Warn("enriched RAG search failed, falling back to basic", "error", err)
		} else {
			results := make([]models.RelevantCode, len(enriched))
			for i, r := range enriched {
				results[i] = models.RelevantCode{
					Function:        r.Function,
					File:            r.File,
					Description:     r.Description,
					Calls:           r.Calls,
					CalledBy:        r.CalledBy,
					IsDependencyHop: r.IsDependencyHop,
					QueryHitCount:   r.QueryHitCount,
				}
			}
			return results
		}
	}

	results, err := a.searcher.SearchStructured(ctx, wo.Title, 10)
	if err != nil {
		slog.Warn("RAG search failed, continuing without", "error", err)
		return nil
	}
	out := make([]models.RelevantCode, len(results))
	for i, r := range results {
		out[i] = models.RelevantCode{
			Function:    r.Function,
			File:        r.File,
			Description: r.Description,
		}
	}
	return out
}

// AssembleScopePrompt builds a minimal text prompt for the scope LLM.
// It includes the work order header and any RAG results as text.
func (a *Assembler) AssembleScopePrompt(ctx context.Context, wo *models.WorkOrder) (string, error) {
	var sb strings.Builder

	sb.WriteString("=== WORK ORDER ===\n")
	fmt.Fprintf(&sb, "Title: %s\n", wo.Title)
	fmt.Fprintf(&sb, "Target module: %s\n", wo.TargetModule)
	if wo.ReferenceModule != "" {
		fmt.Fprintf(&sb, "Reference module: %s\n", wo.ReferenceModule)
	}
	fmt.Fprintf(&sb, "Type: %s\n", wo.Type)
	if len(wo.AcceptanceCriteria) > 0 {
		sb.WriteString("\nAcceptance criteria:\n")
		for _, ac := range wo.AcceptanceCriteria {
			fmt.Fprintf(&sb, "  - %s\n", ac)
		}
	}
	if len(wo.Constraints) > 0 {
		sb.WriteString("\nConstraints:\n")
		for _, c := range wo.Constraints {
			fmt.Fprintf(&sb, "  - %s\n", c)
		}
	}
	if len(wo.KnownFiles) > 0 {
		sb.WriteString("\nKnown files:\n")
		for _, kf := range wo.KnownFiles {
			fmt.Fprintf(&sb, "  - %s\n", kf)
		}
	}
	sb.WriteString("\n")

	if convSection := buildConventions(a.cfg.Conventions); convSection != "" {
		sb.WriteString("=== PROJECT CONVENTIONS ===\n")
		sb.WriteString(convSection)
		sb.WriteString("\n")
	}

	treeStr, totalFiles := a.buildFileTree()
	sb.WriteString("=== PROJECT FILE TREE ===\n")
	if treeStr != "" {
		sb.WriteString(treeStr)
		treeLines := strings.Count(treeStr, "\n")
		if totalFiles > treeLines {
			fmt.Fprintf(&sb, "... (%d more files not shown)\n", totalFiles-treeLines)
		}
	} else {
		sb.WriteString("(no matching files)\n")
	}
	sb.WriteString("\n")

	relevantCode := a.searchRelevantCode(ctx, wo)
	if len(relevantCode) > 0 {
		sb.WriteString("=== SEMANTICALLY RELEVANT CODE (via RAG) ===\n")
		for _, r := range relevantCode {
			marker := ""
			if r.IsDependencyHop {
				marker = " [dependency-hop]"
			}
			fmt.Fprintf(&sb, "  %s — %s%s\n    %s\n", r.File, r.Function, marker, r.Description)
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// Assemble builds the structured FullContextPackage after the scope LLM returns.
// It maps work order fields, scope LLM output, and RAG results into a single JSON-serializable struct.
func (a *Assembler) Assemble(ctx context.Context, wo *models.WorkOrder, scopePkg *models.ContextPackage, branchName string) (*models.FullContextPackage, error) {
	relevantCode := a.searchRelevantCode(ctx, wo)

	var refNote string
	if wo.ReferenceModule != "" {
		refNote = fmt.Sprintf(
			"Use %s as an architectural reference for patterns, conventions, and structure.",
			wo.ReferenceModule,
		)
	}

	full := &models.FullContextPackage{
		WorkOrder: models.WorkOrderContext{
			Title:              wo.Title,
			Type:               wo.Type,
			TargetModule:       wo.TargetModule,
			ReferenceModule:    wo.ReferenceModule,
			AcceptanceCriteria: wo.AcceptanceCriteria,
			Constraints:        wo.Constraints,
			KnownFiles:         wo.KnownFiles,
		},
		Scope: models.ScopeContext{
			FilesToModify:       scopePkg.FilesToModify,
			FilesToReference:    scopePkg.FilesToReference,
			RelevantCode:        relevantCode,
			Summary:             scopePkg.Summary,
			EstimatedComplexity: scopePkg.EstimatedComplexity,
		},
		Directives: models.Directives{
			BranchName:          branchName,
			ReferenceModuleNote: refNote,
		},
	}

	return full, nil
}

// buildFileTree walks the project directory and returns an indented file tree
// string filtered by the configured include/exclude globs, plus the total
// number of matching files (which may exceed the tree lines if capped).
func (a *Assembler) buildFileTree() (string, int) {
	root := a.cfg.Project.Path
	if _, err := os.Stat(root); err != nil {
		slog.Warn("project path not accessible for file tree", "path", root, "error", err)
		return "", 0
	}

	var matched []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		if !util.MatchesAnyGlob(a.cfg.Index.Include, relPath) {
			return nil
		}
		if util.MatchesAnyGlob(a.cfg.Index.Exclude, relPath) {
			return nil
		}
		matched = append(matched, relPath)
		return nil
	})

	sort.Strings(matched)
	totalFiles := len(matched)
	if totalFiles == 0 {
		return "", 0
	}

	cap := a.cfg.Index.MaxTreeLines
	if cap <= 0 {
		cap = 200
	}

	var sb strings.Builder
	emittedDirs := make(map[string]bool)
	fileLines := 0

	for _, rel := range matched {
		if fileLines >= cap {
			break
		}

		parts := strings.Split(rel, "/")
		// Emit directory lines for any new parent directories.
		for depth := 0; depth < len(parts)-1; depth++ {
			dirKey := strings.Join(parts[:depth+1], "/")
			if !emittedDirs[dirKey] {
				emittedDirs[dirKey] = true
				indent := strings.Repeat("  ", depth)
				fmt.Fprintf(&sb, "%s%s/\n", indent, parts[depth])
			}
		}

		// Emit the file line.
		indent := strings.Repeat("  ", len(parts)-1)
		fmt.Fprintf(&sb, "%s%s\n", indent, parts[len(parts)-1])
		fileLines++
	}

	return sb.String(), totalFiles
}

// Target describes an investigation target for context gathering.
// Defined here to avoid importing the scope package.
type Target struct {
	Path      string
	Rationale string
}

// PreScopeBundle holds pre-computed context gathered before any LLM calls.
// Used as input to the decompose step.
type PreScopeBundle struct {
	FileTree      string `json:"file_tree"`
	Conventions   string `json:"conventions"`
	RecallSummary string `json:"recall_summary"`
}

// TargetBundle holds context gathered for a specific investigation target.
// Used as input to the analyze step.
type TargetBundle struct {
	Files      []FileContent `json:"files"`
	RAGChunks  []RAGResult   `json:"rag_chunks"`
	Signatures []string      `json:"signatures"`
}

// FileContent pairs a file path with its content.
type FileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// GatherPreScope collects project-level context needed before any LLM calls.
// It reuses the file tree and convention logic from AssembleScopePrompt.
func (a *Assembler) GatherPreScope(ctx context.Context, wo *models.WorkOrder) (*PreScopeBundle, error) {
	treeStr, _ := a.buildFileTree()
	conventions := buildConventions(a.cfg.Conventions)
	return &PreScopeBundle{
		FileTree:    treeStr,
		Conventions: conventions,
	}, nil
}

// GatherForTarget collects context for a specific investigation target.
// It reads files from the target's directory, extracts function signatures,
// and queries RAG filtered to that path prefix.
func (a *Assembler) GatherForTarget(ctx context.Context, wo *models.WorkOrder, target Target) (*TargetBundle, error) {
	files := a.readTargetFiles(target.Path)
	signatures := extractSignatures(files)
	chunks := a.searchForTarget(ctx, target)

	return &TargetBundle{
		Files:      files,
		RAGChunks:  chunks,
		Signatures: signatures,
	}, nil
}

// maxTargetBytes caps total file content read per target to avoid unbounded memory.
const maxTargetBytes = 512 * 1024 // 512 KiB

// readTargetFiles walks the target directory and returns file paths with content,
// filtered by the project's include/exclude globs.
func (a *Assembler) readTargetFiles(targetPath string) []FileContent {
	root := filepath.Join(a.cfg.Project.Path, targetPath)
	if _, err := os.Stat(root); err != nil {
		slog.Warn("target path not accessible", "path", root, "error", err)
		return nil
	}

	var files []FileContent
	totalBytes := 0

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(a.cfg.Project.Path, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		if len(a.cfg.Index.Include) > 0 && !util.MatchesAnyGlob(a.cfg.Index.Include, relPath) {
			return nil
		}
		if util.MatchesAnyGlob(a.cfg.Index.Exclude, relPath) {
			return nil
		}

		data, err := readFileCapped(path, maxTargetBytes-totalBytes)
		if err != nil {
			return nil
		}
		totalBytes += len(data)
		files = append(files, FileContent{Path: relPath, Content: string(data)})

		if totalBytes >= maxTargetBytes {
			return io.EOF // stop walking
		}
		return nil
	})

	return files
}

// readFileCapped reads up to maxBytes from path.
func readFileCapped(path string, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, io.EOF
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(maxBytes)))
}

// extractSignatures scans file content for Go function, method, and type
// declaration lines. Simple string extraction — no parser dependency.
func extractSignatures(files []FileContent) []string {
	var sigs []string
	for _, fc := range files {
		scanner := bufio.NewScanner(strings.NewReader(fc.Content))
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "type ") {
				sigs = append(sigs, trimmed)
			}
		}
	}
	return sigs
}

// searchForTarget queries RAG and post-filters results to the target path prefix.
func (a *Assembler) searchForTarget(ctx context.Context, target Target) []RAGResult {
	if a.searcher == nil {
		return nil
	}

	topK := 20
	if a.cfg.Index.MaxRAGResults > 0 {
		topK = a.cfg.Index.MaxRAGResults
	}

	results, err := a.searcher.SearchStructured(ctx, target.Rationale, topK)
	if err != nil {
		slog.Warn("RAG search for target failed", "target", target.Path, "error", err)
		return nil
	}

	prefix := target.Path
	var filtered []RAGResult
	for _, r := range results {
		if strings.HasPrefix(r.File, prefix) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// buildConventions formats the project conventions into labeled lines.
// Returns an empty string if no conventions are configured.
func buildConventions(conv config.Conventions) string {
	if conv.ModulePath == "" && len(conv.ModuleStructure) == 0 &&
		conv.SharedPath == "" && conv.SQLPath == "" && conv.DocsPath == "" {
		return ""
	}

	var sb strings.Builder
	if conv.ModulePath != "" {
		fmt.Fprintf(&sb, "Module path: %s\n", conv.ModulePath)
	}
	if len(conv.ModuleStructure) > 0 {
		fmt.Fprintf(&sb, "Module structure: %s\n", strings.Join(conv.ModuleStructure, ", "))
	}
	if conv.SharedPath != "" {
		fmt.Fprintf(&sb, "Shared path: %s\n", conv.SharedPath)
	}
	if conv.SQLPath != "" {
		fmt.Fprintf(&sb, "SQL path: %s\n", conv.SQLPath)
	}
	if conv.DocsPath != "" {
		fmt.Fprintf(&sb, "Docs path: %s\n", conv.DocsPath)
	}
	return sb.String()
}
