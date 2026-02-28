package rag

import (
	"context"
	"strings"
	"testing"
)

func TestFormatRelationshipContext(t *testing.T) {
	chunks := []Chunk{
		{
			Name: "Login",
			Calls: []FuncRef{
				{Name: "ValidateToken", Package: "auth"},
				{Name: "InsertSession", Package: "repo"},
			},
			CalledBy: []FuncRef{
				{Name: "HandleLogin", Package: "handler"},
			},
			TypesUsed:  []string{"LoginRequest", "Session"},
			Implements: []string{"Authenticator"},
		},
		{
			Name: "Logout",
			Calls: []FuncRef{
				{Name: "DeleteSession", Package: "repo"},
			},
			CalledBy: []FuncRef{
				{Name: "HandleLogout", Package: "handler"},
			},
		},
	}

	got := FormatRelationshipContext(chunks)

	if !strings.HasPrefix(got, "=== RELATIONSHIP CONTEXT ===\n") {
		t.Fatalf("missing header, got:\n%s", got)
	}
	for _, want := range []string{
		"Function: Login",
		"Calls: ValidateToken (auth), InsertSession (repo)",
		"Called by: HandleLogin (handler)",
		"Types used: LoginRequest, Session",
		"Implements: Authenticator",
		"Function: Logout",
		"Calls: DeleteSession (repo)",
		"Called by: HandleLogout (handler)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestFormatRelationshipContext_NoRelationships(t *testing.T) {
	chunks := []Chunk{
		{Name: "Foo"},
		{Name: "Bar"},
		{Name: ""},
	}

	got := FormatRelationshipContext(chunks)
	if got != "" {
		t.Errorf("expected empty string for chunks with no relationships, got:\n%s", got)
	}
}

func TestFormatRelationshipContext_PartialRelationships(t *testing.T) {
	chunks := []Chunk{
		{
			Name: "Process",
			Calls: []FuncRef{
				{Name: "Validate"},
				{Name: "Save", Package: "store"},
			},
		},
	}

	got := FormatRelationshipContext(chunks)

	if !strings.Contains(got, "Calls: Validate, Save (store)") {
		t.Errorf("missing Calls line, got:\n%s", got)
	}
	if strings.Contains(got, "Called by:") {
		t.Error("should not contain Called by when empty")
	}
	if strings.Contains(got, "Types used:") {
		t.Error("should not contain Types used when empty")
	}
	if strings.Contains(got, "Implements:") {
		t.Error("should not contain Implements when empty")
	}
}

func TestFormatRelationshipContext_MixedChunks(t *testing.T) {
	chunks := []Chunk{
		{Name: "NoRels"},
		{
			Name:      "HasRels",
			TypesUsed: []string{"Config"},
		},
		{Name: "AlsoNoRels"},
	}

	got := FormatRelationshipContext(chunks)

	if !strings.Contains(got, "Function: HasRels") {
		t.Error("missing HasRels")
	}
	if strings.Contains(got, "NoRels") {
		t.Error("should not contain NoRels")
	}
	if strings.Contains(got, "AlsoNoRels") {
		t.Error("should not contain AlsoNoRels")
	}
}

// mockLLM captures the prompts passed to Complete and returns a canned response.
type mockLLM struct {
	systemPrompt string
	userMessage  string
	response     string
}

func (m *mockLLM) Complete(_ context.Context, sys, user string) (string, error) {
	m.systemPrompt = sys
	m.userMessage = user
	return m.response, nil
}

func TestDescribeFile_WithRelationshipContext(t *testing.T) {
	mock := &mockLLM{
		response: `[{"name": "Login", "description": "Authenticates user and creates session"}]`,
	}
	d := NewDescriber(mock)

	content := "func Login(req LoginRequest) (*Session, error) { ... }"
	relCtx := FormatRelationshipContext([]Chunk{
		{
			Name:  "Login",
			Calls: []FuncRef{{Name: "InsertSession", Package: "repo"}},
			CalledBy: []FuncRef{{Name: "HandleLogin", Package: "handler"}},
		},
	})
	enriched := content + "\n\n" + relCtx

	descs, err := d.DescribeFile(context.Background(), "auth/service.go", enriched)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the LLM received the relationship context.
	if !strings.Contains(mock.userMessage, "=== RELATIONSHIP CONTEXT ===") {
		t.Error("LLM user message should contain relationship context")
	}
	if !strings.Contains(mock.userMessage, "Called by: HandleLogin (handler)") {
		t.Error("LLM user message should contain CalledBy info")
	}

	// Verify description was mapped correctly.
	desc, ok := descs["Login"]
	if !ok {
		t.Fatal("missing description for Login")
	}
	if desc != "Authenticates user and creates session" {
		t.Errorf("unexpected description: %s", desc)
	}
}
