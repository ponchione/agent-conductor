package rag

import (
	"testing"
)

func TestParseTypeScript(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		isTSX     bool
		wantName  string
		wantType  string
		wantSigOK bool // signature is non-empty
	}{
		{
			name: "function declaration",
			source: `function greet(name: string): string {
	return "hello " + name;
}`,
			wantName:  "greet",
			wantType:  "function",
			wantSigOK: true,
		},
		{
			name: "class declaration",
			source: `class UserService {
	private name: string;
	constructor(name: string) {
		this.name = name;
	}
}`,
			wantName:  "UserService",
			wantType:  "class",
			wantSigOK: true,
		},
		{
			name: "interface declaration",
			source: `interface User {
	id: number;
	name: string;
	email: string;
}`,
			wantName:  "User",
			wantType:  "interface",
			wantSigOK: true,
		},
		{
			name:      "type alias",
			source:    `type ID = string | number;`,
			wantName:  "ID",
			wantType:  "type",
			wantSigOK: true,
		},
		{
			name: "enum declaration",
			source: `enum Direction {
	Up,
	Down,
	Left,
	Right,
}`,
			wantName:  "Direction",
			wantType:  "enum",
			wantSigOK: true,
		},
		{
			name: "exported function",
			source: `export function fetchUser(id: number): Promise<User> {
	return api.get("/users/" + id);
}`,
			wantName:  "fetchUser",
			wantType:  "function",
			wantSigOK: true,
		},
		{
			name: "exported arrow function",
			source: `export const add = (a: number, b: number): number => {
	return a + b;
};`,
			wantName:  "add",
			wantType:  "function",
			wantSigOK: true,
		},
		{
			name: "arrow function without export",
			source: `const multiply = (a: number, b: number): number => {
	return a * b;
};`,
			wantName:  "multiply",
			wantType:  "function",
			wantSigOK: true,
		},
		{
			name:  "tsx component",
			isTSX: true,
			source: `const Button = (props: { label: string }) => {
	return <button>{props.label}</button>;
};`,
			wantName:  "Button",
			wantType:  "function",
			wantSigOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, err := parseTypeScript([]byte(tt.source), tt.isTSX)
			if err != nil {
				t.Fatalf("parseTypeScript() error: %v", err)
			}

			if len(chunks) == 0 {
				t.Fatal("parseTypeScript() returned no chunks")
			}

			got := chunks[0]

			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}

			if got.ChunkType != tt.wantType {
				t.Errorf("ChunkType = %q, want %q", got.ChunkType, tt.wantType)
			}

			if tt.wantSigOK && got.Signature == "" {
				t.Error("Signature is empty, want non-empty")
			}

			if got.Body == "" {
				t.Error("Body is empty, want non-empty")
			}

			if got.LineStart < 1 {
				t.Errorf("LineStart = %d, want >= 1", got.LineStart)
			}

			if got.LineEnd < got.LineStart {
				t.Errorf("LineEnd = %d < LineStart = %d", got.LineEnd, got.LineStart)
			}
		})
	}
}

func TestParseTypeScript_MultipleDeclarations(t *testing.T) {
	source := `interface Config {
	port: number;
}

function startServer(config: Config): void {
	console.log("starting on port", config.port);
}

const handler = (req: Request) => {
	return new Response("ok");
};`

	chunks, err := parseTypeScript([]byte(source), false)
	if err != nil {
		t.Fatalf("parseTypeScript() error: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}

	expected := []struct {
		name      string
		chunkType string
	}{
		{"Config", "interface"},
		{"startServer", "function"},
		{"handler", "function"},
	}

	for i, want := range expected {
		if chunks[i].Name != want.name {
			t.Errorf("chunk[%d].Name = %q, want %q", i, chunks[i].Name, want.name)
		}
		if chunks[i].ChunkType != want.chunkType {
			t.Errorf("chunk[%d].ChunkType = %q, want %q", i, chunks[i].ChunkType, want.chunkType)
		}
	}
}
