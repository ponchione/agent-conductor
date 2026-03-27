package graph

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// TSAnalyzer wraps the TypeScript analyzer subprocess.
type TSAnalyzer struct {
	nodePath   string
	scriptPath string // path to analyze.ts
}

// NewTSAnalyzer verifies node/npx are on PATH and locates the analysis script.
func NewTSAnalyzer() (*TSAnalyzer, error) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("TypeScript analyzer requires node on PATH: %w", err)
	}

	// Locate the analyze.ts script relative to this Go source file.
	_, thisFile, _, _ := runtime.Caller(0)
	scriptPath := filepath.Join(filepath.Dir(thisFile), "ts-analyzer", "analyze.ts")

	return &TSAnalyzer{
		nodePath:   nodePath,
		scriptPath: scriptPath,
	}, nil
}

// ndjsonLine is the union of all NDJSON line types from the TS script.
type ndjsonLine struct {
	Type       string  `json:"type"`
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Package    string  `json:"package"`
	FilePath   string  `json:"file_path"`
	LineStart  int     `json:"line_start"`
	LineEnd    int     `json:"line_end"`
	Signature  string  `json:"signature"`
	Exported   bool    `json:"exported"`
	SourceID   string  `json:"source_id"`
	TargetID   string  `json:"target_id"`
	EdgeType   string  `json:"edge_type"`
	Confidence float64 `json:"confidence"`
	SourceLine int     `json:"source_line"`
}

// Analyze runs the TS analyzer subprocess and parses NDJSON output.
func (a *TSAnalyzer) Analyze(projectRoot, tsconfigPath string) (*AnalysisResult, error) {
	projectRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}

	if tsconfigPath == "" {
		tsconfigPath = filepath.Join(projectRoot, "tsconfig.json")
	} else {
		tsconfigPath, err = filepath.Abs(tsconfigPath)
		if err != nil {
			return nil, fmt.Errorf("resolve tsconfig path: %w", err)
		}
	}

	npxPath, err := exec.LookPath("npx")
	if err != nil {
		return nil, fmt.Errorf("npx not on PATH: %w", err)
	}

	cmd := exec.Command(npxPath, "tsx", a.scriptPath, projectRoot, tsconfigPath)
	cmd.Dir = filepath.Dir(a.scriptPath)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ts analyzer: %w", err)
	}

	result := &AnalysisResult{}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line buffer

	for scanner.Scan() {
		var line ndjsonLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			slog.Warn("ts analyzer: invalid NDJSON line", "error", err, "line", scanner.Text())
			continue
		}

		switch line.Type {
		case "symbol":
			result.Symbols = append(result.Symbols, Symbol{
				ID:        line.ID,
				Name:      line.Name,
				Kind:      line.Kind,
				Language:  "typescript",
				Package:   line.Package,
				FilePath:  line.FilePath,
				LineStart: line.LineStart,
				LineEnd:   line.LineEnd,
				Signature: line.Signature,
				Exported:  line.Exported,
			})
		case "edge":
			result.Edges = append(result.Edges, Edge{
				SourceID:   line.SourceID,
				TargetID:   line.TargetID,
				EdgeType:   line.EdgeType,
				Confidence: line.Confidence,
				SourceLine: line.SourceLine,
			})
		case "boundary":
			result.BoundarySymbols = append(result.BoundarySymbols, BoundarySymbol{
				ID:       line.ID,
				Name:     line.Name,
				Kind:     line.Kind,
				Language: "typescript",
				Package:  line.Package,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading ts analyzer output: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		stderr := stderrBuf.String()
		return nil, fmt.Errorf("ts analyzer failed: %w\nstderr: %s", err, stderr)
	}

	slog.Info("TypeScript analysis complete",
		"symbols", len(result.Symbols),
		"edges", len(result.Edges),
		"boundary", len(result.BoundarySymbols),
		"duration", time.Since(start),
	)

	return result, nil
}
