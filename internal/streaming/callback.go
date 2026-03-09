package streaming

import (
	"fmt"
	"io"
)

// ConsoleCallback returns an event callback that writes human-readable
// output to w. Tool results are suppressed (too noisy for console).
func ConsoleCallback(w io.Writer) func(StreamEvent) {
	return func(ev StreamEvent) {
		switch ev.Type {
		case "assistant":
			if ev.Content != "" {
				fmt.Fprint(w, ev.Content)
			}
		case "tool_use":
			fmt.Fprintf(w, "\nTool: %s(%s)\n", ev.ToolName, ev.ToolInput)
		case "tool_result":
			// Suppressed -- too noisy for console output.
		case "result":
			if ev.Usage != nil {
				fmt.Fprintf(w, "\n--- Done: %d tokens in, %d tokens out, $%.4f ---\n",
					ev.Usage.InputTokens, ev.Usage.OutputTokens, ev.Usage.CostUSD)
			}
		}
	}
}
