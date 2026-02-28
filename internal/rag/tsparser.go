package rag

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// parseTypeScript uses tree-sitter to extract top-level declarations from
// TypeScript or TSX source code. When isTSX is true, the TSX grammar is used.
func parseTypeScript(content []byte, isTSX bool) ([]RawChunk, error) {
	parser := sitter.NewParser()
	defer parser.Close()

	var lang *sitter.Language
	if isTSX {
		lang = sitter.NewLanguage(typescript.LanguageTSX())
	} else {
		lang = sitter.NewLanguage(typescript.LanguageTypescript())
	}

	if err := parser.SetLanguage(lang); err != nil {
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
		chunks = append(chunks, extractTSNode(node, content)...)
	}

	return chunks, nil
}

// extractTSNode extracts RawChunks from a single top-level tree-sitter node.
// It handles function, class, interface, type alias, enum declarations,
// export statements (unwrapping the inner declaration), and lexical
// declarations that contain arrow functions or function expressions.
func extractTSNode(node *sitter.Node, content []byte) []RawChunk {
	kind := node.Kind()

	switch kind {
	case "function_declaration":
		return extractNamedDecl(node, content, "function")

	case "class_declaration":
		return extractNamedDecl(node, content, "class")

	case "interface_declaration":
		return extractNamedDecl(node, content, "interface")

	case "type_alias_declaration":
		return extractTypeAlias(node, content)

	case "enum_declaration":
		return extractNamedDecl(node, content, "enum")

	case "export_statement":
		// Unwrap the inner declaration and recurse.
		for j := uint(0); j < node.ChildCount(); j++ {
			child := node.Child(j)
			if child == nil {
				continue
			}
			if chunks := extractTSNode(child, content); len(chunks) > 0 {
				// Use the export statement's span for line numbers.
				for k := range chunks {
					chunks[k].LineStart = int(node.StartPosition().Row) + 1
					chunks[k].LineEnd = int(node.EndPosition().Row) + 1
				}
				return chunks
			}
		}
		return nil

	case "lexical_declaration":
		return extractLexicalDecl(node, content)

	default:
		return nil
	}
}

// extractNamedDecl handles declarations that have a "name" field and a body
// delimited by "{". Works for function, class, interface, and enum declarations.
func extractNamedDecl(node *sitter.Node, content []byte, chunkType string) []RawChunk {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Utf8Text(content)
	if name == "" {
		return nil
	}

	text := string(content[node.StartByte():node.EndByte()])
	sig := extractTSSignature(text, chunkType)

	body := text
	if len(body) > MaxBodyLength {
		body = body[:MaxBodyLength]
	}

	return []RawChunk{{
		Name:      name,
		Signature: sig,
		Body:      body,
		ChunkType: chunkType,
		LineStart: int(node.StartPosition().Row) + 1,
		LineEnd:   int(node.EndPosition().Row) + 1,
	}}
}

// extractTypeAlias handles type_alias_declaration where the entire text
// serves as both signature and body.
func extractTypeAlias(node *sitter.Node, content []byte) []RawChunk {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Utf8Text(content)
	if name == "" {
		return nil
	}

	text := string(content[node.StartByte():node.EndByte()])
	if len(text) > MaxBodyLength {
		text = text[:MaxBodyLength]
	}

	return []RawChunk{{
		Name:      name,
		Signature: text,
		Body:      text,
		ChunkType: "type",
		LineStart: int(node.StartPosition().Row) + 1,
		LineEnd:   int(node.EndPosition().Row) + 1,
	}}
}

// extractLexicalDecl handles const/let/var declarations that may contain
// arrow functions or function expressions (e.g., `const foo = () => {}`).
func extractLexicalDecl(node *sitter.Node, content []byte) []RawChunk {
	var chunks []RawChunk

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || child.Kind() != "variable_declarator" {
			continue
		}

		nameNode := child.ChildByFieldName("name")
		valueNode := child.ChildByFieldName("value")
		if nameNode == nil || valueNode == nil {
			continue
		}

		valKind := valueNode.Kind()
		if valKind != "arrow_function" && valKind != "function" {
			continue
		}

		name := nameNode.Utf8Text(content)
		if name == "" {
			continue
		}

		// Use the full lexical_declaration span for body/lines.
		text := string(content[node.StartByte():node.EndByte()])
		sig := extractTSSignature(text, "function")

		body := text
		if len(body) > MaxBodyLength {
			body = body[:MaxBodyLength]
		}

		chunks = append(chunks, RawChunk{
			Name:      name,
			Signature: sig,
			Body:      body,
			ChunkType: "function",
			LineStart: int(node.StartPosition().Row) + 1,
			LineEnd:   int(node.EndPosition().Row) + 1,
		})
	}

	return chunks
}

// extractTSSignature returns the signature portion of a declaration's text.
// For type aliases, the full text is the signature (handled by caller).
// For everything else, it's the text up to the first "{".
func extractTSSignature(text, _ string) string {
	if idx := strings.Index(text, "{"); idx != -1 {
		return strings.TrimRight(text[:idx], " \t\n\r")
	}
	// No body brace found — use the first line.
	if idx := strings.Index(text, "\n"); idx != -1 {
		return text[:idx]
	}
	return text
}
