package llm

import "strings"

// CleanLLMResponse extracts the first JSON object from a raw LLM response.
func CleanLLMResponse(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end < start {
		return strings.TrimSpace(raw)
	}
	return raw[start : end+1]
}
