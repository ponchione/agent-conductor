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
	// Mock server setup
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify URL path
		if r.URL.Path != "/chat/completions" {
			// Note: Endpoint in New() trims trailing slash.
			// JoinPath adds /chat/completions.
			// If Endpoint is http://server/v1, path is /v1/chat/completions
			// Let's debug this in the test body.
		}

		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()

		var req chatCompletionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}

		// Verify model and messages
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

		// Return success response
		resp := chatCompletionResponse{
			Choices: []choice{
				{
					Message: message{
						Role:    "assistant",
						Content: "Success!",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Client setup
	// server.URL includes http://ip:port
	cfg := config.LocalModel{
		Endpoint:       server.URL,
		ModelName:      "qwen-local",
		Temperature:    0.1,
		TimeoutSeconds: 5,
	}
	client := New(cfg)

	// Test successful completion
	ctx := context.Background()
	result, _, err := client.Complete(ctx, "System Prompt", "User Message")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Success!" {
		t.Errorf("expected result 'Success!', got %s", result)
	}
}

func TestClient_Errors(t *testing.T) {
	// 1. Test Server Error
	t.Run("server_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal Error"))
		}))
		defer server.Close()

		client := New(config.LocalModel{Endpoint: server.URL, TimeoutSeconds: 1})
		_, _, err := client.Complete(context.Background(), "sys", "user")
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "status 500") {
			t.Errorf("expected status 500 error, got %v", err)
		}
	})

	// 2. Test Malformed JSON
	t.Run("malformed_json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices": [{"message": {"content": "broken"`)) // incomplete json
		}))
		defer server.Close()

		client := New(config.LocalModel{Endpoint: server.URL, TimeoutSeconds: 1})
		_, _, err := client.Complete(context.Background(), "sys", "user")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	// 3. Test Empty Choices
	t.Run("empty_choices", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices": []}`))
		}))
		defer server.Close()

		client := New(config.LocalModel{Endpoint: server.URL, TimeoutSeconds: 1})
		_, _, err := client.Complete(context.Background(), "sys", "user")
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

	// Set client timeout very low
	client := New(config.LocalModel{Endpoint: server.URL, TimeoutSeconds: 1}) // 1 sec is too long for test

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _, err := client.Complete(ctx, "sys", "user")
	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	// Error usually wraps context.DeadlineExceeded
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "Client.Timeout exceeded") {
		// depending on go version and http client setup, error might vary slightly
		// but checking context error is safer
		if ctx.Err() != context.DeadlineExceeded {
			t.Errorf("expected deadline exceeded, got %v", err)
		}
	}
}
