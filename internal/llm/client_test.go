package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
)

func TestClient_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()

		var req chatCompletionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}

		if req.Model != "qwen-local" {
			t.Errorf("expected model qwen-local, got %s", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "system" || req.Messages[0].Content != "System Prompt" {
			t.Errorf("incorrect system message")
		}
		if req.Messages[1].Role != "user" || req.Messages[1].Content != "User Message" {
			t.Errorf("incorrect user message")
		}

		resp := chatCompletionResponse{
			Choices: []choice{
				{Message: message{Role: "assistant", Content: "Success!"}},
			},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.LocalModel{
		Endpoint:       server.URL,
		ModelName:      "qwen-local",
		Temperature:    0.1,
		TimeoutSeconds: 5,
	}
	client := New(cfg)

	ctx := context.Background()
	result, err := client.Complete(ctx, "System Prompt", "User Message")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "Success!" {
		t.Errorf("expected content 'Success!', got %s", result.Content)
	}
	if result.TokensIn != 10 {
		t.Errorf("expected TokensIn 10, got %d", result.TokensIn)
	}
	if result.TokensOut != 5 {
		t.Errorf("expected TokensOut 5, got %d", result.TokensOut)
	}
	if result.LatencyMs < 0 {
		t.Errorf("expected non-negative LatencyMs, got %d", result.LatencyMs)
	}
}

func TestClient_Errors(t *testing.T) {
	t.Run("server_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal Error"))
		}))
		defer server.Close()

		client := New(config.LocalModel{Endpoint: server.URL, TimeoutSeconds: 1})
		_, err := client.Complete(context.Background(), "sys", "user")
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "status 500") {
			t.Errorf("expected status 500 error, got %v", err)
		}
	})

	t.Run("malformed_json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices": [{"message": {"content": "broken"`))
		}))
		defer server.Close()

		client := New(config.LocalModel{Endpoint: server.URL, TimeoutSeconds: 1})
		_, err := client.Complete(context.Background(), "sys", "user")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("empty_choices", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices": []}`))
		}))
		defer server.Close()

		client := New(config.LocalModel{Endpoint: server.URL, TimeoutSeconds: 1})
		_, err := client.Complete(context.Background(), "sys", "user")
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no choices") {
			t.Errorf("expected 'no choices' error, got %v", err)
		}
	})
}

func TestClient_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(config.LocalModel{Endpoint: server.URL, TimeoutSeconds: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Complete(ctx, "sys", "user")
	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "Client.Timeout exceeded") {
		if ctx.Err() != context.DeadlineExceeded {
			t.Errorf("expected deadline exceeded, got %v", err)
		}
	}
}

func TestProviderClient_AuthorizationHeader(t *testing.T) {
	t.Run("with_api_key", func(t *testing.T) {
		var gotAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			resp := chatCompletionResponse{
				Choices: []choice{{Message: message{Role: "assistant", Content: "ok"}}},
				Usage:   Usage{PromptTokens: 1, CompletionTokens: 1},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewProviderClient(Provider{
			Endpoint: server.URL,
			Model:    "test-model",
			APIKey:   "sk-secret-123",
		})

		_, err := client.Complete(context.Background(), "sys", "user")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotAuth != "Bearer sk-secret-123" {
			t.Errorf("expected 'Bearer sk-secret-123', got %q", gotAuth)
		}
	})

	t.Run("without_api_key", func(t *testing.T) {
		var gotAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			resp := chatCompletionResponse{
				Choices: []choice{{Message: message{Role: "assistant", Content: "ok"}}},
				Usage:   Usage{PromptTokens: 1, CompletionTokens: 1},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewProviderClient(Provider{
			Endpoint: server.URL,
			Model:    "test-model",
		})

		_, err := client.Complete(context.Background(), "sys", "user")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotAuth != "" {
			t.Errorf("expected no Authorization header, got %q", gotAuth)
		}
	})
}

type mockClient struct {
	content string
	err     error
}

func (m *mockClient) Complete(_ context.Context, _, _ string) (*CompletionResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &CompletionResult{Content: m.content, TokensIn: 10, TokensOut: 5}, nil
}

func TestRoleResolver_ForRole(t *testing.T) {
	clientA := &mockClient{content: "from-a"}
	clientB := &mockClient{content: "from-b"}

	providers := map[string]Client{
		"provider-a": clientA,
		"provider-b": clientB,
	}
	roles := map[string]string{
		"scope":  "provider-a",
		"verify": "provider-b",
	}

	resolver := NewRoleResolver(providers, roles)

	t.Run("happy_path", func(t *testing.T) {
		c, err := resolver.ForRole("scope")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result, err := c.Complete(context.Background(), "sys", "user")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Content != "from-a" {
			t.Errorf("expected 'from-a', got %q", result.Content)
		}
	})

	t.Run("unknown_role", func(t *testing.T) {
		_, err := resolver.ForRole("build")
		if err == nil {
			t.Fatal("expected error for unknown role")
		}
		if !strings.Contains(err.Error(), "unknown role") {
			t.Errorf("expected 'unknown role' error, got %v", err)
		}
		if !strings.Contains(err.Error(), "scope") {
			t.Errorf("expected error to list available roles, got %v", err)
		}
	})

	t.Run("dangling_provider", func(t *testing.T) {
		danglingRoles := map[string]string{
			"scope": "provider-missing",
		}
		r := NewRoleResolver(providers, danglingRoles)
		_, err := r.ForRole("scope")
		if err == nil {
			t.Fatal("expected error for dangling provider")
		}
		if !strings.Contains(err.Error(), "provider-missing") {
			t.Errorf("expected error to mention missing provider, got %v", err)
		}
	})
}

func TestRAGCompleterAdapter(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		adapter := &RAGCompleterAdapter{Client: &mockClient{content: "hello"}}
		text, err := adapter.Complete(context.Background(), "sys", "user")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if text != "hello" {
			t.Errorf("expected 'hello', got %q", text)
		}
	})

	t.Run("error", func(t *testing.T) {
		adapter := &RAGCompleterAdapter{Client: &mockClient{err: io.EOF}}
		_, err := adapter.Complete(context.Background(), "sys", "user")
		if err == nil {
			t.Fatal("expected error")
		}
		if err != io.EOF {
			t.Errorf("expected io.EOF, got %v", err)
		}
	})
}

func TestCompleteStream_HappyPath(t *testing.T) {
	sseBody := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"choices":[{"delta":{"content":" world"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`,
		`data: [DONE]`,
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req streamingChatCompletionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("failed to parse request: %v", err)
		}
		if !req.Stream {
			t.Error("expected stream=true in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	client := NewProviderClient(Provider{
		Endpoint: server.URL,
		Model:    "test-model",
	})

	ch, err := client.CompleteStream(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunk[0].Content = %q, want %q", chunks[0].Content, "Hello")
	}
	if chunks[1].Content != " world" {
		t.Errorf("chunk[1].Content = %q, want %q", chunks[1].Content, " world")
	}
	if !chunks[2].Done {
		t.Error("expected final chunk to have Done=true")
	}
	if chunks[2].Usage == nil {
		t.Fatal("expected final chunk to have Usage set")
	}
	if chunks[2].Usage.PromptTokens != 10 {
		t.Errorf("Usage.PromptTokens = %d, want 10", chunks[2].Usage.PromptTokens)
	}
	if chunks[2].Usage.CompletionTokens != 2 {
		t.Errorf("Usage.CompletionTokens = %d, want 2", chunks[2].Usage.CompletionTokens)
	}
	for _, chunk := range chunks {
		if chunk.Error != nil {
			t.Errorf("unexpected error in chunk: %v", chunk.Error)
		}
	}
}
