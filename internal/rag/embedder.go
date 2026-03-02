package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	// QueryPrefix is prepended to search queries before embedding.
	QueryPrefix = "Represent this query for searching relevant code: "

	// DefaultEmbeddingDims is the expected output dimensionality of nomic-embed-code.
	DefaultEmbeddingDims = 3584

	// DefaultEmbedBatchSize is the maximum number of texts per batch request.
	DefaultEmbedBatchSize = 32
)

// EmbedderConfig holds configuration for the embedding HTTP client.
type EmbedderConfig struct {
	// Endpoint is the base URL for the llama.cpp embedding server.
	Endpoint string

	// TimeoutSeconds is the HTTP request timeout. Defaults to 30.
	TimeoutSeconds int

	// BatchSize is the max texts per batch request. Defaults to DefaultEmbedBatchSize.
	BatchSize int
}

// Embedder calls the llama.cpp /v1/embeddings endpoint to produce
// vector representations of code chunks and queries.
type Embedder struct {
	endpoint  string
	client    *http.Client
	batchSize int
}

// NewEmbedder creates an Embedder from the given config.
func NewEmbedder(cfg EmbedderConfig) *Embedder {
	timeout := cfg.TimeoutSeconds
	if timeout == 0 {
		timeout = 30
	}
	batchSize := cfg.BatchSize
	if batchSize == 0 {
		batchSize = DefaultEmbedBatchSize
	}
	return &Embedder{
		endpoint: cfg.Endpoint,
		client: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
		batchSize: batchSize,
	}
}

// embeddingRequest is the JSON body sent to /v1/embeddings.
type embeddingRequest struct {
	Input any `json:"input"`
}

// embeddingResponse is the JSON response from /v1/embeddings.
type embeddingResponse struct {
	Data []embeddingData `json:"data"`
}

type embeddingData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// Embed produces a vector for a single text (document chunk).
// No prefix is applied — this is for indexing, not querying.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.post(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("embed single: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embed single: empty response")
	}
	return resp.Data[0].Embedding, nil
}

// EmbedBatch produces vectors for multiple texts (document chunks).
// No prefix is applied. If the input exceeds the batch size, it is
// split into multiple requests automatically.
func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	embeddings := make([][]float32, len(texts))

	for start := 0; start < len(texts); start += e.batchSize {
		end := min(start+e.batchSize, len(texts))
		batch := texts[start:end]

		resp, err := e.post(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("embed batch [%d:%d]: %w", start, end, err)
		}

		if len(resp.Data) != len(batch) {
			return nil, fmt.Errorf("embed batch [%d:%d]: expected %d embeddings, got %d",
				start, end, len(batch), len(resp.Data))
		}

		for _, d := range resp.Data {
			embeddings[start+d.Index] = d.Embedding
		}
	}

	return embeddings, nil
}

// EmbedQuery produces a vector for a search query.
// The nomic-embed-code query prefix is prepended automatically.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	resp, err := e.post(ctx, QueryPrefix+query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embed query: empty response")
	}
	return resp.Data[0].Embedding, nil
}

// post sends a request to the /v1/embeddings endpoint.
// input can be a string (single) or []string (batch).
func (e *Embedder) post(ctx context.Context, input any) (*embeddingResponse, error) {
	jsonBody, err := json.Marshal(embeddingRequest{Input: input})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	targetURL, err := url.JoinPath(e.endpoint, "embeddings")
	if err != nil {
		return nil, fmt.Errorf("failed to construct URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body json.RawMessage
		json.NewDecoder(resp.Body).Decode(&body)
		return nil, fmt.Errorf("api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
