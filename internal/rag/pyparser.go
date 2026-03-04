package rag

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// parsePython uses tree-sitter to extract top-level functions, classes, and
// methods from Python source code.
func parsePython(content []byte) ([]RawChunk, error) {
	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(sitter.NewLanguage(python.Language())); err != nil {
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
		chunks = append(chunks, extractPyNode(node, content)...)
	}

	return chunks, nil
}

// extractPyNode extracts RawChunks from a single top-level tree-sitter node.
// It handles function_definition, class_definition, and decorated_definition.
func extractPyNode(node *sitter.Node, content []byte) []RawChunk {
	switch node.Kind() {
	case "function_definition":
		return extractPyFunc(node, node, content, "function")
	case "class_definition":
		return extractPyClass(node, node, content)
	case "decorated_definition":
		return extractPyDecorated(node, content)
	default:
		return nil
	}
}

// extractPyDecorated unwraps a decorated_definition and delegates to the
// appropriate extractor, using the decorated_definition's span for line
// numbers and body text.
func extractPyDecorated(outer *sitter.Node, content []byte) []RawChunk {
	for i := uint(0); i < outer.ChildCount(); i++ {
		child := outer.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "function_definition":
			return extractPyFunc(outer, child, content, "function")
		case "class_definition":
			return extractPyClass(outer, child, content)
		}
	}
	return nil
}

// extractPyFunc extracts a single function or method chunk.
// spanNode determines the byte/line range (may be a decorated_definition).
// defNode is the actual function_definition used for name and body field lookup.
func extractPyFunc(spanNode, defNode *sitter.Node, content []byte, chunkType string) []RawChunk {
	nameNode := defNode.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Utf8Text(content)
	if name == "" {
		return nil
	}

	// Signature: from span start to the inner definition's body start.
	bodyField := defNode.ChildByFieldName("body")
	var sig string
	if bodyField != nil {
		sig = strings.TrimRight(string(content[spanNode.StartByte():bodyField.StartByte()]), " \t\n\r")
	} else {
		sig = strings.TrimRight(string(content[spanNode.StartByte():spanNode.EndByte()]), " \t\n\r")
	}

	body := string(content[spanNode.StartByte():spanNode.EndByte()])
	if len(body) > MaxBodyLength {
		body = body[:MaxBodyLength]
	}

	return []RawChunk{{
		Name:      name,
		Signature: sig,
		Body:      body,
		ChunkType: chunkType,
		LineStart: int(spanNode.StartPosition().Row) + 1,
		LineEnd:   int(spanNode.EndPosition().Row) + 1,
	}}
}

// extractPyClass extracts a class chunk plus individual method chunks.
// spanNode determines the byte/line range (may be a decorated_definition).
// defNode is the actual class_definition used for name and body field lookup.
func extractPyClass(spanNode, defNode *sitter.Node, content []byte) []RawChunk {
	nameNode := defNode.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Utf8Text(content)
	if name == "" {
		return nil
	}

	// Class signature: from span start to the inner definition's body start.
	bodyField := defNode.ChildByFieldName("body")
	var sig string
	if bodyField != nil {
		sig = strings.TrimRight(string(content[spanNode.StartByte():bodyField.StartByte()]), " \t\n\r")
	} else {
		sig = strings.TrimRight(string(content[spanNode.StartByte():spanNode.EndByte()]), " \t\n\r")
	}

	classBody := string(content[spanNode.StartByte():spanNode.EndByte()])
	if len(classBody) > MaxBodyLength {
		classBody = classBody[:MaxBodyLength]
	}

	chunks := []RawChunk{{
		Name:      name,
		Signature: sig,
		Body:      classBody,
		ChunkType: "class",
		LineStart: int(spanNode.StartPosition().Row) + 1,
		LineEnd:   int(spanNode.EndPosition().Row) + 1,
	}}

	// Extract methods from the class body block.
	if bodyField != nil {
		for i := uint(0); i < bodyField.ChildCount(); i++ {
			child := bodyField.Child(i)
			if child == nil {
				continue
			}
			switch child.Kind() {
			case "function_definition":
				chunks = append(chunks, extractPyFunc(child, child, content, "method")...)
			case "decorated_definition":
				// Unwrap: find inner function_definition, use decorated span.
				for j := uint(0); j < child.ChildCount(); j++ {
					inner := child.Child(j)
					if inner != nil && inner.Kind() == "function_definition" {
						chunks = append(chunks, extractPyFunc(child, inner, content, "method")...)
						break
					}
				}
			}
		}
	}

	return chunks
}
