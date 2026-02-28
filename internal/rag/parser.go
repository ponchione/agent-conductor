package rag

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// Parser is the interface for turning source files into structural chunks.
type Parser interface {
	ParseFile(filePath, language string, content []byte) ([]RawChunk, error)
}

// TreeSitterParser wraps the existing tree-sitter-based parsing logic.
type TreeSitterParser struct{}

// NewTreeSitterParser returns a new TreeSitterParser.
func NewTreeSitterParser() *TreeSitterParser { return &TreeSitterParser{} }

// ParseFile dispatches to language-specific tree-sitter parsers or the fallback splitter.
func (p *TreeSitterParser) ParseFile(filePath, language string, content []byte) ([]RawChunk, error) {
	return ParseFile(filePath, language, content)
}

// ParseFile turns raw file bytes into a slice of RawChunks.
// It dispatches to a language-specific parser or falls back to a sliding-window
// splitter. It never returns an error for unsupported languages.
// Kept as a package-level function for backward compatibility (smokeparser).
func ParseFile(filePath string, language string, content []byte) ([]RawChunk, error) {
	switch language {
	case "go":
		return parseGo(content)
	case "markdown", "md":
		return parseMarkdown(content)
	default:
		return parseFallback(filePath, content), nil
	}
}

// parseGo uses tree-sitter to extract top-level functions, methods, and type
// declarations from Go source code.
func parseGo(content []byte) ([]RawChunk, error) {
	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(sitter.NewLanguage(golang.Language())); err != nil {
		return nil, fmt.Errorf("tree-sitter set language: %w", err)
	}

	tree := parser.Parse(content, nil)
	defer tree.Close()

	root := tree.RootNode()
	childCount := root.ChildCount()

	var chunks []RawChunk
	for i := uint(0); i < childCount; i++ {
		node := root.Child(i)
		if node == nil {
			continue
		}

		var chunkType string
		switch node.Kind() {
		case "function_declaration":
			chunkType = "function"
		case "method_declaration":
			chunkType = "method"
		case "type_declaration":
			chunkType = "type"
		default:
			continue
		}

		name := extractGoName(node, chunkType, content)
		if name == "" {
			continue
		}

		sig := extractGoSignature(node, chunkType, content)
		body := string(content[node.StartByte():node.EndByte()])
		if len(body) > MaxBodyLength {
			body = body[:MaxBodyLength]
		}

		chunks = append(chunks, RawChunk{
			Name:      name,
			Signature: sig,
			Body:      body,
			ChunkType: chunkType,
			LineStart: int(node.StartPosition().Row) + 1,
			LineEnd:   int(node.EndPosition().Row) + 1,
		})
	}

	return chunks, nil
}

func extractGoName(node *sitter.Node, chunkType string, content []byte) string {
	if chunkType == "type" {
		// Find nested type_spec child, then its "name" field
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child == nil {
				continue
			}
			if child.Kind() == "type_spec" {
				nameNode := child.ChildByFieldName("name")
				if nameNode != nil {
					return nameNode.Utf8Text(content)
				}
			}
		}
		return ""
	}
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	return nameNode.Utf8Text(content)
}

func extractGoSignature(node *sitter.Node, chunkType string, content []byte) string {
	if chunkType == "type" {
		text := string(content[node.StartByte():node.EndByte()])
		if idx := strings.Index(text, "{"); idx != -1 {
			return strings.TrimRight(text[:idx], " \t\n\r")
		}
		if idx := strings.Index(text, "\n"); idx != -1 {
			return text[:idx]
		}
		return text
	}

	// function or method: everything up to the body block
	bodyNode := node.ChildByFieldName("body")
	if bodyNode == nil {
		return strings.TrimRight(string(content[node.StartByte():node.EndByte()]), " \t\n\r")
	}
	return strings.TrimRight(string(content[node.StartByte():bodyNode.StartByte()]), " \t\n\r")
}

// parseMarkdown splits content on level-2 headers ("## ") into sections.
// Content before the first "## " header is skipped.
func parseMarkdown(content []byte) ([]RawChunk, error) {
	lines := strings.Split(string(content), "\n")

	var chunks []RawChunk
	var currentName string
	var bodyLines []string
	var headerLineStart int
	inSection := false

	for lineIdx, line := range lines {
		lineNum := lineIdx + 1 // 1-indexed

		if strings.HasPrefix(line, "## ") {
			// Flush previous section
			if inSection && currentName != "" {
				body := strings.Join(bodyLines, "\n")
				if len(body) > MaxBodyLength {
					body = body[:MaxBodyLength]
				}
				chunks = append(chunks, RawChunk{
					Name:      currentName,
					ChunkType: "section",
					Body:      body,
					LineStart: headerLineStart,
					LineEnd:   lineIdx, // last line of previous section (lineIdx is 0-based next header)
				})
			}

			currentName = strings.TrimRight(strings.TrimPrefix(line, "## "), "\r\n")
			bodyLines = nil
			headerLineStart = lineNum
			inSection = true
		} else if inSection {
			bodyLines = append(bodyLines, line)
		}
	}

	// Flush the last section
	if inSection && currentName != "" {
		body := strings.Join(bodyLines, "\n")
		if len(body) > MaxBodyLength {
			body = body[:MaxBodyLength]
		}
		chunks = append(chunks, RawChunk{
			Name:      currentName,
			ChunkType: "section",
			Body:      body,
			LineStart: headerLineStart,
			LineEnd:   len(lines),
		})
	}

	return chunks, nil
}

// parseFallback splits content into overlapping 40-line windows with a 20-line step.
func parseFallback(filePath string, content []byte) []RawChunk {
	lines := strings.Split(string(content), "\n")
	var chunks []RawChunk

	for start := 0; start < len(lines); start += 20 {
		end := start + 40
		if end > len(lines) {
			end = len(lines)
		}
		body := strings.Join(lines[start:end], "\n")
		if len(body) > MaxBodyLength {
			body = body[:MaxBodyLength]
		}
		chunks = append(chunks, RawChunk{
			Name:      fmt.Sprintf("%s:%d-%d", filePath, start+1, end),
			ChunkType: "section",
			Body:      body,
			LineStart: start + 1,
			LineEnd:   end,
		})
	}

	return chunks
}
