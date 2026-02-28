package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DescriptionSystemPrompt is the system prompt for the local LLM when
// generating semantic descriptions during indexing.
const DescriptionSystemPrompt = `You are a code documentation assistant. Given a file containing source code
and optional relationship context, produce a brief semantic description for each
function/method/type in the file.

Respond ONLY in valid JSON with this schema:
[
  {"name": "FunctionName", "description": "1-3 sentence description"}
]

Rules:
- One entry per function, method, or type declaration
- Descriptions should capture INTENT, not just restate the signature
- When relationship context is provided, mention key relationships:
  who calls this function, what it delegates to, and what types it uses
- Focus on what the code does in the context of the application
- Keep each description under 60 words`

// MaxDescriptionFileLength is the maximum character length of file content
// sent to the local LLM for description generation.
const MaxDescriptionFileLength = 6000

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

// FormatRelationshipContext builds a structured relationship section from
// chunk metadata for appending to file content before description generation.
// Returns "" if no chunk has any relationship metadata.
func FormatRelationshipContext(chunks []Chunk) string {
	var b strings.Builder

	for _, c := range chunks {
		if c.Name == "" {
			continue
		}
		if len(c.Calls) == 0 && len(c.CalledBy) == 0 && len(c.TypesUsed) == 0 && len(c.Implements) == 0 {
			continue
		}

		b.WriteString("Function: ")
		b.WriteString(c.Name)
		b.WriteByte('\n')

		if len(c.Calls) > 0 {
			b.WriteString("  Calls: ")
			b.WriteString(formatFuncRefs(c.Calls))
			b.WriteByte('\n')
		}
		if len(c.CalledBy) > 0 {
			b.WriteString("  Called by: ")
			b.WriteString(formatFuncRefs(c.CalledBy))
			b.WriteByte('\n')
		}
		if len(c.TypesUsed) > 0 {
			b.WriteString("  Types used: ")
			b.WriteString(strings.Join(c.TypesUsed, ", "))
			b.WriteByte('\n')
		}
		if len(c.Implements) > 0 {
			b.WriteString("  Implements: ")
			b.WriteString(strings.Join(c.Implements, ", "))
			b.WriteByte('\n')
		}

		b.WriteByte('\n')
	}

	if b.Len() == 0 {
		return ""
	}

	return "=== RELATIONSHIP CONTEXT ===\n" + b.String()
}

// formatFuncRefs formats a slice of FuncRef as "Name (Package), Name2 (Package2)".
func formatFuncRefs(refs []FuncRef) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		if r.Package != "" {
			parts[i] = r.Name + " (" + r.Package + ")"
		} else {
			parts[i] = r.Name
		}
	}
	return strings.Join(parts, ", ")
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
