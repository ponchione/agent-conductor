package llm

import (
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
)

func TestNewClientFromProviderRequiresExplicitType(t *testing.T) {
	_, err := NewClientFromProvider(config.ProviderConfig{
		Endpoint: "http://localhost:8080/v1",
		Model:    "deepseek-r1",
	}, ".")
	if err == nil {
		t.Fatal("NewClientFromProvider() error = nil, want explicit type failure")
	}
	if !strings.Contains(err.Error(), `unknown provider type ""`) {
		t.Fatalf("NewClientFromProvider() error = %q, want unknown empty provider type", err)
	}
}

func TestNewClientFromProviderAcceptsOpenAIProvider(t *testing.T) {
	client, err := NewClientFromProvider(config.ProviderConfig{
		Type:     "openai",
		Endpoint: "http://localhost:8080/v1",
		Model:    "deepseek-r1",
	}, ".")
	if err != nil {
		t.Fatalf("NewClientFromProvider() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewClientFromProvider() returned nil client")
	}
}
