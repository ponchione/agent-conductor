package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type blockingStreamBody struct {
	ctx    context.Context
	data   string
	offset int
}

func (b *blockingStreamBody) Read(p []byte) (int, error) {
	if b.offset < len(b.data) {
		n := copy(p, b.data[b.offset:])
		b.offset += n
		return n, nil
	}
	<-b.ctx.Done()
	return 0, io.EOF
}

func (b *blockingStreamBody) Close() error {
	return nil
}

func newProviderClientForTest(provider Provider, transport roundTripFunc) *ProviderClient {
	if provider.Endpoint == "" {
		provider.Endpoint = "http://example.test"
	}
	if provider.Model == "" {
		provider.Model = "test-model"
	}
	return newProviderClientWithHTTPClient(provider, &http.Client{Transport: transport})
}

func jsonResponse(statusCode int, body any) *http.Response {
	payload, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(payload))),
	}
}

func textResponse(statusCode int, body string, contentType string) *http.Response {
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func decodeJSONBody(t *testing.T, req *http.Request, dst any) {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	defer req.Body.Close()
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
}

func TestClient_Complete(t *testing.T) {
	client := newProviderClientForTest(Provider{
		Endpoint:       "http://example.test/v1",
		Model:          "qwen-local",
		Temperature:    0.1,
		TimeoutSeconds: 5,
	}, func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://example.test/v1/chat/completions" {
			t.Fatalf("request URL = %q", r.URL.String())
		}

		var req chatCompletionRequest
		decodeJSONBody(t, r, &req)

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

		return jsonResponse(http.StatusOK, chatCompletionResponse{
			Choices: []choice{
				{Message: message{Role: "assistant", Content: "Success!"}},
			},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5},
		}), nil
	})

	result, err := client.Complete(context.Background(), "System Prompt", "User Message")
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
		client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
			return textResponse(http.StatusInternalServerError, "Internal Error", "text/plain"), nil
		})

		_, err := client.Complete(context.Background(), "sys", "user")
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "status 500") {
			t.Errorf("expected status 500 error, got %v", err)
		}
	})

	t.Run("malformed_json", func(t *testing.T) {
		client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
			return textResponse(http.StatusOK, `{"choices": [{"message": {"content": "broken"`, "application/json"), nil
		})

		_, err := client.Complete(context.Background(), "sys", "user")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("empty_choices", func(t *testing.T) {
		client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
			return textResponse(http.StatusOK, `{"choices": []}`, "application/json"), nil
		})

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
	client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})

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
		client := newProviderClientForTest(Provider{
			APIKey: "sk-secret-123",
		}, func(r *http.Request) (*http.Response, error) {
			gotAuth = r.Header.Get("Authorization")
			return jsonResponse(http.StatusOK, chatCompletionResponse{
				Choices: []choice{{Message: message{Role: "assistant", Content: "ok"}}},
				Usage:   Usage{PromptTokens: 1, CompletionTokens: 1},
			}), nil
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
		client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
			gotAuth = r.Header.Get("Authorization")
			return jsonResponse(http.StatusOK, chatCompletionResponse{
				Choices: []choice{{Message: message{Role: "assistant", Content: "ok"}}},
				Usage:   Usage{PromptTokens: 1, CompletionTokens: 1},
			}), nil
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

	client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
		var req streamingChatCompletionRequest
		decodeJSONBody(t, r, &req)
		if !req.Stream {
			t.Error("expected stream=true in request")
		}
		return textResponse(http.StatusOK, sseBody, "text/event-stream"), nil
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

func TestCompleteStream_ServerError(t *testing.T) {
	client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
		return textResponse(http.StatusInternalServerError, "Internal Error", "text/plain"), nil
	})

	_, err := client.CompleteStream(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("expected status 500 error, got %v", err)
	}
}

func TestCompleteStream_MalformedChunk(t *testing.T) {
	client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
		return textResponse(http.StatusOK, "data: {invalid json}\n\n", "text/event-stream"), nil
	})

	ch, err := client.CompleteStream(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotError bool
	for chunk := range ch {
		if chunk.Error != nil {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected an error chunk for malformed JSON")
	}
}

func TestCompleteStream_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
		if r.Context() == nil {
			t.Fatal("expected request context")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: &blockingStreamBody{
				ctx:  r.Context(),
				data: "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n",
			},
		}, nil
	})

	ch, err := client.CompleteStream(ctx, "sys", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunk := <-ch
	if chunk.Content != "Hi" {
		t.Errorf("expected 'Hi', got %q", chunk.Content)
	}

	cancel()

	for range ch {
	}
}

func TestCompleteStream_NoUsageInFinalChunk(t *testing.T) {
	sseBody := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hi"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
		return textResponse(http.StatusOK, sseBody, "text/event-stream"), nil
	})

	ch, err := client.CompleteStream(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if !chunks[1].Done {
		t.Error("expected final chunk Done=true")
	}
	if chunks[1].Usage != nil {
		t.Error("expected nil Usage when server omits it")
	}
}

func TestCompleteStream_EmptyLines(t *testing.T) {
	client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
		return textResponse(http.StatusOK, "\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n", "text/event-stream"), nil
	})

	ch, err := client.CompleteStream(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != "ok" {
		t.Errorf("Content = %q, want %q", chunks[0].Content, "ok")
	}
}

func TestComplete_TransportError(t *testing.T) {
	expected := errors.New("transport failed")
	client := newProviderClientForTest(Provider{}, func(r *http.Request) (*http.Response, error) {
		return nil, expected
	})

	_, err := client.Complete(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), expected.Error()) {
		t.Fatalf("expected error to contain %q, got %v", expected.Error(), err)
	}
}
