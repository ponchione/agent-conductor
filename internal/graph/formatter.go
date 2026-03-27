package graph

import (
	"fmt"
	"strings"
)

// FormatBlastRadiusBlock renders blast radius results as a text block
// suitable for injection into the scope phase's assembled context.
func FormatBlastRadiusBlock(results []*BlastRadiusResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== STRUCTURAL CONTEXT (call graph) ===\n\n")

	for _, result := range results {
		formatSymbolSection(&sb, result)
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatSymbolSection renders one symbol's blast radius.
func formatSymbolSection(sb *strings.Builder, result *BlastRadiusResult) {
	t := result.Target
	fmt.Fprintf(sb, "Target: %s (%s)\n", t.Name, t.Kind)
	fmt.Fprintf(sb, "  File: %s:%d-%d\n", t.FilePath, t.LineStart, t.LineEnd)
	if t.Signature != "" {
		fmt.Fprintf(sb, "  Signature: %s\n", t.Signature)
	}
	sb.WriteString("\n")

	if len(result.Upstream) > 0 {
		sb.WriteString("  UPSTREAM (callers — changes here may require updates to these):\n")
		for _, node := range result.Upstream {
			fmt.Fprintf(sb, "    [depth %d, confidence %.1f, %s] %s\n",
				node.Depth, node.Confidence, node.EdgeType, node.Symbol.Name)
			fmt.Fprintf(sb, "      File: %s:%d\n", node.Symbol.FilePath, node.Symbol.LineStart)
			if node.Symbol.Signature != "" {
				fmt.Fprintf(sb, "      Signature: %s\n", node.Symbol.Signature)
			}
		}
		sb.WriteString("\n")
	}

	if len(result.Downstream) > 0 {
		sb.WriteString("  DOWNSTREAM (callees — this function depends on these):\n")
		for _, node := range result.Downstream {
			fmt.Fprintf(sb, "    [depth %d, confidence %.1f, %s] %s\n",
				node.Depth, node.Confidence, node.EdgeType, node.Symbol.Name)
			fmt.Fprintf(sb, "      File: %s:%d\n", node.Symbol.FilePath, node.Symbol.LineStart)
			if node.Symbol.Signature != "" {
				fmt.Fprintf(sb, "      Signature: %s\n", node.Symbol.Signature)
			}
		}
		sb.WriteString("\n")
	}

	if len(result.Interfaces) > 0 {
		sb.WriteString("  IMPLEMENTS:\n")
		for _, iface := range result.Interfaces {
			fmt.Fprintf(sb, "    %s (%s)\n", iface.Name, iface.Kind)
			fmt.Fprintf(sb, "      File: %s:%d\n", iface.FilePath, iface.LineStart)
		}
		sb.WriteString("\n")
	}
}

// FormatEdgeCountAnnotation returns a short annotation for RAG cross-referencing.
// e.g., "called by 4 functions, calls 2 functions"
func FormatEdgeCountAnnotation(callerCount, calleeCount int) string {
	var parts []string
	if callerCount > 0 {
		parts = append(parts, fmt.Sprintf("called by %d functions", callerCount))
	}
	if calleeCount > 0 {
		parts = append(parts, fmt.Sprintf("calls %d functions", calleeCount))
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
