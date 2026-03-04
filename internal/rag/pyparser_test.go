package rag

import (
	"strings"
	"testing"
)

func TestParsePython_TopLevelFunction(t *testing.T) {
	source := `def greet(name: str) -> str:
    return "hello " + name
`
	chunks, err := parsePython([]byte(source))
	if err != nil {
		t.Fatalf("parsePython() error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	c := chunks[0]
	if c.Name != "greet" {
		t.Errorf("Name = %q, want %q", c.Name, "greet")
	}
	if c.ChunkType != "function" {
		t.Errorf("ChunkType = %q, want %q", c.ChunkType, "function")
	}
	if !strings.Contains(c.Signature, "def greet") {
		t.Errorf("Signature %q missing 'def greet'", c.Signature)
	}
	if !strings.Contains(c.Signature, "-> str") {
		t.Errorf("Signature %q missing return type annotation", c.Signature)
	}
	if c.LineStart != 1 {
		t.Errorf("LineStart = %d, want 1", c.LineStart)
	}
}

func TestParsePython_AsyncFunction(t *testing.T) {
	source := `async def fetch_data(url: str) -> bytes:
    async with aiohttp.ClientSession() as session:
        resp = await session.get(url)
        return await resp.read()
`
	chunks, err := parsePython([]byte(source))
	if err != nil {
		t.Fatalf("parsePython() error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	c := chunks[0]
	if c.Name != "fetch_data" {
		t.Errorf("Name = %q, want %q", c.Name, "fetch_data")
	}
	if c.ChunkType != "function" {
		t.Errorf("ChunkType = %q, want %q", c.ChunkType, "function")
	}
	if !strings.Contains(c.Signature, "async def") {
		t.Errorf("Signature %q missing 'async def'", c.Signature)
	}
}

func TestParsePython_DecoratedFunction(t *testing.T) {
	source := `@app.route("/api/data")
@login_required
def get_data():
    return jsonify(data)
`
	chunks, err := parsePython([]byte(source))
	if err != nil {
		t.Fatalf("parsePython() error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	c := chunks[0]
	if c.Name != "get_data" {
		t.Errorf("Name = %q, want %q", c.Name, "get_data")
	}
	if c.ChunkType != "function" {
		t.Errorf("ChunkType = %q, want %q", c.ChunkType, "function")
	}
	if !strings.Contains(c.Signature, "@app.route") {
		t.Errorf("Signature %q missing decorator", c.Signature)
	}
	if !strings.Contains(c.Signature, "@login_required") {
		t.Errorf("Signature %q missing stacked decorator", c.Signature)
	}
	// Line range should include decorator lines.
	if c.LineStart != 1 {
		t.Errorf("LineStart = %d, want 1", c.LineStart)
	}
}

func TestParsePython_ClassWithMethods(t *testing.T) {
	source := `class UserService:
    def __init__(self, db):
        self.db = db

    def get_user(self, user_id: int) -> dict:
        return self.db.find(user_id)

    @staticmethod
    def validate(data: dict) -> bool:
        return "name" in data

    @classmethod
    def from_config(cls, config: dict) -> "UserService":
        return cls(config["db"])
`
	chunks, err := parsePython([]byte(source))
	if err != nil {
		t.Fatalf("parsePython() error: %v", err)
	}

	// Expect: class + __init__ + get_user + validate + from_config = 5
	if len(chunks) != 5 {
		t.Fatalf("got %d chunks, want 5; chunks: %v", len(chunks), chunkNames(chunks))
	}

	// Class chunk.
	if chunks[0].Name != "UserService" {
		t.Errorf("chunk[0].Name = %q, want %q", chunks[0].Name, "UserService")
	}
	if chunks[0].ChunkType != "class" {
		t.Errorf("chunk[0].ChunkType = %q, want %q", chunks[0].ChunkType, "class")
	}

	// Methods.
	wantMethods := []string{"__init__", "get_user", "validate", "from_config"}
	for i, want := range wantMethods {
		c := chunks[i+1]
		if c.Name != want {
			t.Errorf("chunk[%d].Name = %q, want %q", i+1, c.Name, want)
		}
		if c.ChunkType != "method" {
			t.Errorf("chunk[%d].ChunkType = %q, want %q", i+1, c.ChunkType, "method")
		}
	}

	// Decorated methods should include decorators in signature.
	validateChunk := chunks[3]
	if !strings.Contains(validateChunk.Signature, "@staticmethod") {
		t.Errorf("validate signature %q missing @staticmethod", validateChunk.Signature)
	}
	fromConfigChunk := chunks[4]
	if !strings.Contains(fromConfigChunk.Signature, "@classmethod") {
		t.Errorf("from_config signature %q missing @classmethod", fromConfigChunk.Signature)
	}
}

func TestParsePython_DataclassDecoratedClass(t *testing.T) {
	source := `@dataclass
class Config:
    host: str
    port: int = 8080
`
	chunks, err := parsePython([]byte(source))
	if err != nil {
		t.Fatalf("parsePython() error: %v", err)
	}
	if len(chunks) < 1 {
		t.Fatal("parsePython() returned no chunks")
	}
	c := chunks[0]
	if c.Name != "Config" {
		t.Errorf("Name = %q, want %q", c.Name, "Config")
	}
	if c.ChunkType != "class" {
		t.Errorf("ChunkType = %q, want %q", c.ChunkType, "class")
	}
	if !strings.Contains(c.Signature, "@dataclass") {
		t.Errorf("Signature %q missing @dataclass decorator", c.Signature)
	}
	if c.LineStart != 1 {
		t.Errorf("LineStart = %d, want 1", c.LineStart)
	}
}

func TestParsePython_MultiLineSignature(t *testing.T) {
	source := `def process(
    items: list[str],
    count: int,
    verbose: bool = False,
) -> list[str]:
    return [i for i in items[:count]]
`
	chunks, err := parsePython([]byte(source))
	if err != nil {
		t.Fatalf("parsePython() error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	c := chunks[0]
	if c.Name != "process" {
		t.Errorf("Name = %q, want %q", c.Name, "process")
	}
	if !strings.Contains(c.Signature, "verbose: bool") {
		t.Errorf("Signature %q missing multi-line parameter", c.Signature)
	}
	if !strings.Contains(c.Signature, "-> list[str]") {
		t.Errorf("Signature %q missing return type", c.Signature)
	}
}

func TestParsePython_MultipleTopLevel(t *testing.T) {
	source := `def foo():
    pass

class Bar:
    def baz(self):
        pass

def qux():
    pass
`
	chunks, err := parsePython([]byte(source))
	if err != nil {
		t.Fatalf("parsePython() error: %v", err)
	}
	// foo + Bar + baz + qux = 4
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks, want 4; chunks: %v", len(chunks), chunkNames(chunks))
	}

	expected := []struct {
		name      string
		chunkType string
	}{
		{"foo", "function"},
		{"Bar", "class"},
		{"baz", "method"},
		{"qux", "function"},
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

// chunkNames returns a slice of chunk names for debug output.
func chunkNames(chunks []RawChunk) []string {
	names := make([]string, len(chunks))
	for i, c := range chunks {
		names[i] = c.Name + "(" + c.ChunkType + ")"
	}
	return names
}
