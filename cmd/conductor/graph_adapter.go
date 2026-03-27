package main

import (
	"github.com/ponchione/agent-conductor/internal/config"
	condctx "github.com/ponchione/agent-conductor/internal/context"
	"github.com/ponchione/agent-conductor/internal/graph"
)

// graphQuerierAdapter wraps a GraphStore to satisfy condctx.GraphQuerier.
type graphQuerierAdapter struct {
	store *graph.GraphStore
	cfg   *config.GraphConfig
}

func newGraphQuerierAdapter(store *graph.GraphStore, cfg *config.GraphConfig) condctx.GraphQuerier {
	return &graphQuerierAdapter{store: store, cfg: cfg}
}

func (a *graphQuerierAdapter) BlastRadius(targetSymbol string, direction int, maxDepth, budget int, minConfidence float64, includeTests bool) (condctx.BlastRadiusFormatted, error) {
	req := graph.BlastRadiusRequest{
		TargetSymbol:  targetSymbol,
		Direction:     graph.Direction(direction),
		MaxDepth:      maxDepth,
		Budget:        budget,
		MinConfidence: minConfidence,
		IncludeTests:  includeTests,
	}

	result, err := a.store.BlastRadius(req)
	if err != nil {
		return condctx.BlastRadiusFormatted{}, err
	}

	formatted := condctx.BlastRadiusFormatted{
		TargetName:  result.Target.Name,
		TargetKind:  result.Target.Kind,
		TargetFile:  result.Target.FilePath,
		TargetLines: [2]int{result.Target.LineStart, result.Target.LineEnd},
		TargetSig:   result.Target.Signature,
	}

	for _, n := range result.Upstream {
		formatted.Upstream = append(formatted.Upstream, condctx.NodeInfo{
			Name:       n.Symbol.Name,
			Kind:       n.Symbol.Kind,
			FilePath:   n.Symbol.FilePath,
			LineStart:  n.Symbol.LineStart,
			Signature:  n.Symbol.Signature,
			Depth:      n.Depth,
			EdgeType:   n.EdgeType,
			Confidence: n.Confidence,
		})
	}

	for _, n := range result.Downstream {
		formatted.Downstream = append(formatted.Downstream, condctx.NodeInfo{
			Name:       n.Symbol.Name,
			Kind:       n.Symbol.Kind,
			FilePath:   n.Symbol.FilePath,
			LineStart:  n.Symbol.LineStart,
			Signature:  n.Symbol.Signature,
			Depth:      n.Depth,
			EdgeType:   n.EdgeType,
			Confidence: n.Confidence,
		})
	}

	for _, iface := range result.Interfaces {
		formatted.Interfaces = append(formatted.Interfaces, condctx.SymbolInfo{
			Name:      iface.Name,
			Kind:      iface.Kind,
			FilePath:  iface.FilePath,
			LineStart: iface.LineStart,
			Signature: iface.Signature,
		})
	}

	return formatted, nil
}

func (a *graphQuerierAdapter) GetSymbolsForFile(filePath string) ([]condctx.SymbolInfo, error) {
	syms, err := a.store.GetSymbolsByFile(filePath)
	if err != nil {
		return nil, err
	}

	result := make([]condctx.SymbolInfo, len(syms))
	for i, s := range syms {
		result[i] = condctx.SymbolInfo{
			Name:      s.Name,
			Kind:      s.Kind,
			FilePath:  s.FilePath,
			LineStart: s.LineStart,
			Signature: s.Signature,
		}
	}
	return result, nil
}
