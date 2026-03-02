package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
)

// Client handles communication with a local LLM via an OpenAI-compatible API.
type Client struct {
	endpoint       string
	modelName      string
	temperature    float64
	timeoutSeconds int
	httpClient     *http.Client
}

// New creates a new LLM client using the provided configuration.
func New(cfg config.LocalModel) *Client {
	return &Client{
		endpoint:       strings.TrimRight(cfg.Endpoint, "/"),
		modelName:      cfg.ModelName,
		temperature:    cfg.Temperature,
		timeoutSeconds: cfg.TimeoutSeconds,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
		},
	}
}

// Usage holds token counts returned by the LLM API.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Complete sends a chat completion request to the LLM.
func (c *Client) Complete(ctx context.Context, systemPrompt string, userMessage string) (string, Usage, error) {
	reqBody := chatCompletionRequest{
		Model:       c.modelName,
		Temperature: c.temperature,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", Usage{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	targetURL, err := url.JoinPath(c.endpoint, "chat", "completions")
	if err != nil {
		return "", Usage{}, fmt.Errorf("failed to construct URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", Usage{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", Usage{}, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", Usage{}, fmt.Errorf("api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var respBody chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return "", Usage{}, fmt.Errorf("failed to decode response: %w", err)
	}

	slog.Debug("LLM call complete", "model", c.modelName, "duration_ms", duration.Milliseconds())

	if len(respBody.Choices) == 0 {
		return "", Usage{}, fmt.Errorf("api returned no choices")
	}

	return respBody.Choices[0].Message.Content, respBody.Usage, nil
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
