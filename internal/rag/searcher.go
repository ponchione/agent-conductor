package rag

import (
	"context"
	"log/slog"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	condctx "github.com/ponchione/agent-conductor/internal/context"
	"github.com/ponchione/agent-conductor/internal/models"
)

// DefaultSearcher implements Searcher by embedding the query then running VectorSearch.
type DefaultSearcher struct {
	store    *Store
	embedder *Embedder
}

// NewSearcher creates a DefaultSearcher from the given store and embedder.
func NewSearcher(store *Store, embedder *Embedder) *DefaultSearcher {
	return &DefaultSearcher{store: store, embedder: embedder}
}

// Search embeds the query and performs a vector search against the store.
func (s *DefaultSearcher) Search(ctx context.Context, query string, topK int, filters ...Filter) ([]SearchResult, error) {
	vec, err := s.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return s.store.VectorSearch(ctx, vec, topK, filters...)
}

// SearchStructured implements the context.RAGSearcher interface.
// It runs Search and maps results to structured RAGResult values.
func (s *DefaultSearcher) SearchStructured(ctx context.Context, query string, topK int) ([]condctx.RAGResult, error) {
	results, err := s.Search(ctx, query, topK)
	if err != nil {
		return nil, err
	}
	out := make([]condctx.RAGResult, len(results))
	for i, r := range results {
		out[i] = condctx.RAGResult{
			Function:    r.Chunk.Name,
			File:        r.Chunk.FilePath,
			Description: r.Chunk.Description,
		}
	}
	return out, nil
}

// SearchForWorkOrder implements context.RAGWorkOrderSearcher.
// It performs multi-query expansion from work order fields, deduplicates and
// re-ranks results, then expands one hop through the call graph.
func (s *DefaultSearcher) SearchForWorkOrder(
	ctx context.Context,
	wo *models.WorkOrder,
	maxResults int,
) ([]condctx.EnrichedRAGResult, error) {
	queries := expandQueries(wo)

	// Stage 1: Multi-query search + dedup
	type scored struct {
		result    SearchResult
		hitCount  int
		bestScore float32
	}
	seen := make(map[string]*scored)

	perQueryK := 10
	for _, q := range queries {
		results, err := s.Search(ctx, q, perQueryK)
		if err != nil {
			slog.Warn("query expansion search failed", "query", q, "error", err)
			continue
		}
		for _, r := range results {
			key := r.Chunk.ID
			if existing, ok := seen[key]; ok {
				existing.hitCount++
				if r.Score > existing.bestScore {
					existing.bestScore = r.Score
					existing.result = r
				}
			} else {
				seen[key] = &scored{
					result:    r,
					hitCount:  1,
					bestScore: r.Score,
				}
			}
		}
	}

	// Stage 2: Re-rank by hit count desc, then best score desc
	directHits := make([]*scored, 0, len(seen))
	for _, s := range seen {
		directHits = append(directHits, s)
	}
	sort.Slice(directHits, func(i, j int) bool {
		if directHits[i].hitCount != directHits[j].hitCount {
			return directHits[i].hitCount > directHits[j].hitCount
		}
		return directHits[i].bestScore > directHits[j].bestScore
	})

	// Budget: ~60% direct hits, ~40% dependency hops
	directBudget := (maxResults * 60) / 100
	if directBudget > len(directHits) {
		directBudget = len(directHits)
	}
	hopBudget := maxResults - directBudget

	// Stage 3: Build enriched results from direct hits
	enriched := make([]EnrichedResult, 0, maxResults)
	for i := 0; i < directBudget; i++ {
		h := directHits[i]
		enriched = append(enriched, EnrichedResult{
			SearchResult:  h.result,
			QueryHitCount: h.hitCount,
		})
	}

	// Stage 4: One-hop dependency expansion
	seenIDs := make(map[string]bool, len(enriched))
	for _, e := range enriched {
		seenIDs[e.Chunk.ID] = true
	}

	hops := s.expandDependencies(ctx, enriched, seenIDs, hopBudget)
	enriched = append(enriched, hops...)

	// Stage 5: Map to EnrichedRAGResult
	return toEnrichedResults(enriched, maxResults), nil
}

// expandQueries derives search queries from work order fields.
func expandQueries(wo *models.WorkOrder) []string {
	queries := []string{wo.Title}

	if wo.TargetModule != "" {
		queries = append(queries, wo.TargetModule)
	}

	for _, ac := range wo.AcceptanceCriteria {
		if isBuildTestCriterion(ac) {
			continue
		}
		if len(strings.Fields(ac)) <= 4 {
			continue
		}
		queries = append(queries, ac)
	}

	for _, kf := range wo.KnownFiles {
		dir := filepath.Dir(kf)
		base := strings.TrimSuffix(filepath.Base(kf), filepath.Ext(kf))
		if dir != "." {
			queries = append(queries, dir+" "+base)
		} else {
			queries = append(queries, base)
		}
	}

	return queries
}

// buildTestPattern matches acceptance criteria that are about running go build/test.
var buildTestPattern = regexp.MustCompile(`(?i)\bgo\s+(build|test|vet)\b|\bpasses\b|\bcompiles\b`)

// isBuildTestCriterion returns true for criteria about build/test commands
// that would not yield useful search queries.
func isBuildTestCriterion(ac string) bool {
	return buildTestPattern.MatchString(ac)
}

// expandDependencies performs one-hop expansion through the call graph.
// For each direct-hit chunk, it looks up functions in Calls and CalledBy
// via the store's name index.
func (s *DefaultSearcher) expandDependencies(
	ctx context.Context,
	directHits []EnrichedResult,
	seenIDs map[string]bool,
	budget int,
) []EnrichedResult {
	var hops []EnrichedResult

	for _, hit := range directHits {
		if len(hops) >= budget {
			break
		}

		// Expand Calls
		for _, ref := range hit.Chunk.Calls {
			if len(hops) >= budget {
				break
			}
			chunks, err := s.store.ChunksByName(ctx, ref.Name)
			if err != nil {
				slog.Debug("dependency hop lookup failed", "name", ref.Name, "error", err)
				continue
			}
			for _, c := range chunks {
				if len(hops) >= budget {
					break
				}
				if seenIDs[c.ID] {
					continue
				}
				seenIDs[c.ID] = true
				hops = append(hops, EnrichedResult{
					SearchResult:    SearchResult{Chunk: c, Score: 0},
					IsDependencyHop: true,
				})
			}
		}

		// Expand CalledBy
		for _, ref := range hit.Chunk.CalledBy {
			if len(hops) >= budget {
				break
			}
			chunks, err := s.store.ChunksByName(ctx, ref.Name)
			if err != nil {
				slog.Debug("dependency hop lookup failed", "name", ref.Name, "error", err)
				continue
			}
			for _, c := range chunks {
				if len(hops) >= budget {
					break
				}
				if seenIDs[c.ID] {
					continue
				}
				seenIDs[c.ID] = true
				hops = append(hops, EnrichedResult{
					SearchResult:    SearchResult{Chunk: c, Score: 0},
					IsDependencyHop: true,
				})
			}
		}
	}

	return hops
}

// toEnrichedResults maps internal EnrichedResults to the context package's
// EnrichedRAGResult type, converting FuncRef → CodeRef.
func toEnrichedResults(results []EnrichedResult, maxResults int) []condctx.EnrichedRAGResult {
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	out := make([]condctx.EnrichedRAGResult, len(results))
	for i, r := range results {
		out[i] = condctx.EnrichedRAGResult{
			Function:        r.Chunk.Name,
			File:            r.Chunk.FilePath,
			Description:     r.Chunk.Description,
			Calls:           funcRefsToCodeRefs(r.Chunk.Calls),
			CalledBy:        funcRefsToCodeRefs(r.Chunk.CalledBy),
			IsDependencyHop: r.IsDependencyHop,
			QueryHitCount:   r.QueryHitCount,
			Score:           r.Score,
		}
	}
	return out
}

// funcRefsToCodeRefs converts a slice of rag.FuncRef to models.CodeRef.
func funcRefsToCodeRefs(refs []FuncRef) []models.CodeRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]models.CodeRef, len(refs))
	for i, r := range refs {
		out[i] = models.CodeRef{
			Name:    r.Name,
			Package: r.Package,
			File:    r.File,
		}
	}
	return out
}
