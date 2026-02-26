package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DescriptionSystemPrompt is the system prompt for the local LLM when
// generating semantic descriptions during indexing.
const DescriptionSystemPrompt = `You are a code documentation assistant. Given a file containing source code,
produce a brief semantic description for each function/method/type in the file.

Respond ONLY in valid JSON with this schema:
[
  {"name": "FunctionName", "description": "1-2 sentence description of what this does and why"}
]

Rules:
- One entry per function, method, or type declaration
- Descriptions should capture INTENT, not just restate the signature
- Focus on what the code does in the context of the application
- Keep each description under 50 words`

// MaxDescriptionFileLength is the maximum character length of file content
// sent to the local LLM for description generation.
const MaxDescriptionFileLength = 4000

// descriptionEntry is a single item in the LLM's JSON response.
type descriptionEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// LLMCompleter is the interface the describer needs from an LLM client.
// The real llm.Client.Complete returns (string, llm.Usage, error) — use
// an adapter at the wiring point in main.go to satisfy this interface:
//
//	type llmAdapter struct{ client *llm.Client }
//	func (a *llmAdapter) Complete(ctx context.Context, sys, user string) (string, error) {
//	    text, _, err := a.client.Complete(ctx, sys, user)
//	    return text, err
//	}
type LLMCompleter interface {
	Complete(ctx context.Context, systemPrompt string, userMessage string) (string, error)
}

// Describer generates semantic descriptions for code chunks using
// a local LLM.
type Describer struct {
	llm LLMCompleter
}

// NewDescriber creates a Describer backed by the given LLM client.
func NewDescriber(llm LLMCompleter) *Describer {
	return &Describer{llm: llm}
}

// DescribeFile sends the file content to the local LLM and returns
// a map of function name → description. One LLM call per file.
//
// If the LLM returns invalid JSON or the call fails, returns an error.
// Callers should handle gracefully — descriptions are a quality boost,
// not a hard requirement. Chunks can be indexed without descriptions.
func (d *Describer) DescribeFile(ctx context.Context, filePath string, fileContent string) (map[string]string, error) {
	content := fileContent
	if len(content) > MaxDescriptionFileLength {
		content = content[:MaxDescriptionFileLength]
	}

	userMsg := fmt.Sprintf("File: %s\n\n```\n%s\n```", filePath, content)

	raw, err := d.llm.Complete(ctx, DescriptionSystemPrompt, userMsg)
	if err != nil {
		return nil, fmt.Errorf("llm call for %s: %w", filePath, err)
	}

	raw = stripCodeFence(raw)

	var entries []descriptionEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("parse descriptions for %s: %w (raw: %.200s)", filePath, err, raw)
	}

	result := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.Name != "" && e.Description != "" {
			result[e.Name] = e.Description
		}
	}

	return result, nil
}

// stripCodeFence removes markdown ```json ... ``` wrapping that local
// models sometimes add despite being told to return only JSON.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}
	return s
}
