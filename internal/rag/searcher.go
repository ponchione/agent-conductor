package rag

import "context"

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

// SearchFormatted implements the context.RAGSearcher interface.
// It runs Search and returns the results as a pre-formatted context block.
func (s *DefaultSearcher) SearchFormatted(ctx context.Context, query string, topK int) (string, error) {
	results, err := s.Search(ctx, query, topK)
	if err != nil {
		return "", err
	}
	return FormatResultsBlock(results), nil
}
