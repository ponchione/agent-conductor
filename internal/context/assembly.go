package context

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/models"
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

	// Prefer work-order-aware search if the searcher supports it.
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

	// Fallback: basic title-only search.
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
	sb.WriteString(fmt.Sprintf("Title: %s\n", wo.Title))
	sb.WriteString(fmt.Sprintf("Target module: %s\n", wo.TargetModule))
	if wo.ReferenceModule != "" {
		sb.WriteString(fmt.Sprintf("Reference module: %s\n", wo.ReferenceModule))
	}
	sb.WriteString(fmt.Sprintf("Type: %s\n", wo.Type))
	if len(wo.AcceptanceCriteria) > 0 {
		sb.WriteString("\nAcceptance criteria:\n")
		for _, ac := range wo.AcceptanceCriteria {
			sb.WriteString(fmt.Sprintf("  - %s\n", ac))
		}
	}
	if len(wo.Constraints) > 0 {
		sb.WriteString("\nConstraints:\n")
		for _, c := range wo.Constraints {
			sb.WriteString(fmt.Sprintf("  - %s\n", c))
		}
	}
	if len(wo.KnownFiles) > 0 {
		sb.WriteString("\nKnown files:\n")
		for _, kf := range wo.KnownFiles {
			sb.WriteString(fmt.Sprintf("  - %s\n", kf))
		}
	}
	sb.WriteString("\n")

	// RAG results
	relevantCode := a.searchRelevantCode(ctx, wo)
	if len(relevantCode) > 0 {
		sb.WriteString("=== SEMANTICALLY RELEVANT CODE (via RAG) ===\n")
		for _, r := range relevantCode {
			marker := ""
			if r.IsDependencyHop {
				marker = " [dependency-hop]"
			}
			sb.WriteString(fmt.Sprintf("  %s — %s%s\n    %s\n", r.File, r.Function, marker, r.Description))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// Assemble builds the structured FullContextPackage after the scope LLM returns.
// It maps work order fields, scope LLM output, and RAG results into a single JSON-serializable struct.
func (a *Assembler) Assemble(ctx context.Context, wo *models.WorkOrder, scopePkg *models.ContextPackage, branchName string) (*models.FullContextPackage, error) {
	relevantCode := a.searchRelevantCode(ctx, wo)

	// Build reference module note
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
