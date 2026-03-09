package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
)

// Client is the interface for all LLM completion providers.
type Client interface {
	Complete(ctx context.Context, systemPrompt string, userMessage string) (*CompletionResult, error)
}

// CompletionResult holds the output and metadata from an LLM completion call.
type CompletionResult struct {
	Content   string
	TokensIn  int
	TokensOut int
	LatencyMs int64
	Provider  string
	Model     string
}

// Provider holds the configuration for a single LLM provider endpoint.
type Provider struct {
	Endpoint         string
	Model            string
	APIKey           string
	TimeoutSeconds   int
	MaxContextTokens int
	Temperature      float64
}

// ProviderClient handles communication with an LLM via an OpenAI-compatible API.
type ProviderClient struct {
	provider   Provider
	httpClient *http.Client
}

// NewProviderClient creates a new ProviderClient from the given Provider config.
func NewProviderClient(p Provider) *ProviderClient {
	timeout := p.TimeoutSeconds
	if timeout == 0 {
		timeout = 60
	}
	return &ProviderClient{
		provider: p,
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}
}

// New creates a new ProviderClient using the provided configuration.
// This is a backward-compatible shim that delegates to NewProviderClient.
func New(cfg config.LocalModel) *ProviderClient {
	return NewProviderClient(Provider{
		Endpoint:       cfg.Endpoint,
		Model:          cfg.ModelName,
		Temperature:    cfg.Temperature,
		TimeoutSeconds: cfg.TimeoutSeconds,
	})
}

// Usage holds token counts returned by the LLM API.
// Used internally for JSON deserialization of OpenAI-compatible responses.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// StreamChunk represents a single chunk from a streaming LLM response.
type StreamChunk struct {
	Content string
	Done    bool
	Usage   *Usage
	Error   error
}

// Complete sends a chat completion request to the LLM.
func (c *ProviderClient) Complete(ctx context.Context, systemPrompt string, userMessage string) (*CompletionResult, error) {
	reqBody := chatCompletionRequest{
		Model:       c.provider.Model,
		Temperature: c.provider.Temperature,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := strings.TrimRight(c.provider.Endpoint, "/")
	targetURL, err := url.JoinPath(endpoint, "chat", "completions")
	if err != nil {
		return nil, fmt.Errorf("failed to construct URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if c.provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.provider.APIKey)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var respBody chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	slog.Debug("LLM call complete", "model", c.provider.Model, "duration_ms", duration.Milliseconds())

	if len(respBody.Choices) == 0 {
		return nil, fmt.Errorf("api returned no choices")
	}

	return &CompletionResult{
		Content:   respBody.Choices[0].Message.Content,
		TokensIn:  respBody.Usage.PromptTokens,
		TokensOut: respBody.Usage.CompletionTokens,
		LatencyMs: duration.Milliseconds(),
		Provider:  endpoint,
		Model:     c.provider.Model,
	}, nil
}

// CompleteStream sends a streaming chat completion request and returns a channel
// of StreamChunk values. The channel is always closed by the producer goroutine.
func (c *ProviderClient) CompleteStream(ctx context.Context, systemPrompt string, userMessage string) (<-chan StreamChunk, error) {
	reqBody := streamingChatCompletionRequest{
		Model:       c.provider.Model,
		Temperature: c.provider.Temperature,
		Stream:      true,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := strings.TrimRight(c.provider.Endpoint, "/")
	targetURL, err := url.JoinPath(endpoint, "chat", "completions")
	if err != nil {
		return nil, fmt.Errorf("failed to construct URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if c.provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.provider.APIKey)
	}

	// Use a client without Timeout for streaming — the overall http.Client.Timeout
	// covers the entire transaction including body reads, which kills long-lived
	// SSE connections. Context cancellation handles timeouts instead.
	streamClient := &http.Client{Transport: c.httpClient.Transport}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				return
			}

			var chunk streamChunkResponse
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				ch <- StreamChunk{Error: fmt.Errorf("failed to parse stream chunk: %w", err)}
				return
			}

			sc := StreamChunk{}
			if len(chunk.Choices) > 0 {
				sc.Content = chunk.Choices[0].Delta.Content
				if chunk.Choices[0].FinishReason != nil {
					sc.Done = true
				}
			}
			if chunk.Usage != nil {
				sc.Usage = chunk.Usage
			}

			select {
			case ch <- sc:
			case <-ctx.Done():
				select {
				case ch <- StreamChunk{Error: ctx.Err()}:
				default:
				}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case ch <- StreamChunk{Error: fmt.Errorf("scanner error: %w", err)}:
			default:
			}
		}
	}()

	return ch, nil
}

// RoleResolver maps role names to LLM Client implementations.
type RoleResolver struct {
	clients map[string]Client
	roles   map[string]string
}

// NewRoleResolver creates a RoleResolver. providers maps provider names to Client
// implementations, and roles maps role names to provider names.
func NewRoleResolver(providers map[string]Client, roles map[string]string) *RoleResolver {
	return &RoleResolver{
		clients: providers,
		roles:   roles,
	}
}

// ForRole returns the Client for the given role.
func (r *RoleResolver) ForRole(role string) (Client, error) {
	providerName, ok := r.roles[role]
	if !ok {
		return nil, fmt.Errorf("unknown role %q; available roles: %s", role, joinKeys(r.roles))
	}

	client, ok := r.clients[providerName]
	if !ok {
		return nil, fmt.Errorf("role %q maps to provider %q, but provider not found; available providers: %s",
			role, providerName, joinKeys(r.clients))
	}

	return client, nil
}

// joinKeys returns sorted keys of a map as a comma-separated string.
func joinKeys[M ~map[string]V, V any](m M) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// RAGCompleterAdapter bridges a Client (which returns *CompletionResult) to the
// rag.LLMCompleter interface (which expects (string, error)).
type RAGCompleterAdapter struct {
	Client Client
}

// Complete satisfies rag.LLMCompleter by discarding CompletionResult metadata.
func (a *RAGCompleterAdapter) Complete(ctx context.Context, systemPrompt string, userMessage string) (string, error) {
	result, err := a.Client.Complete(ctx, systemPrompt, userMessage)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// internal structs for JSON marshaling

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
	Stop        []string  `json:"stop,omitempty"`
}

type chatCompletionResponse struct {
	Choices []choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type choice struct {
	Message message `json:"message"`
}

// streamingChatCompletionRequest is like chatCompletionRequest but includes
// the stream field. Kept separate so Complete() never sends "stream":false.
type streamingChatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
}

// streamChunkResponse represents a single SSE chunk from the streaming API.
type streamChunkResponse struct {
	Choices []streamChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"`
}

type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason,omitempty"`
}

type streamDelta struct {
	Content string `json:"content,omitempty"`
}
