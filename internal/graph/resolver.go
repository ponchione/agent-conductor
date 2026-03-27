package graph

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
)

// Resolver orchestrates language detection and dispatches to analyzers.
type Resolver struct {
	projectRoot string
	cfg         *config.GraphConfig
}

// NewResolver creates a new language resolver.
func NewResolver(projectRoot string, cfg *config.GraphConfig) *Resolver {
	return &Resolver{
		projectRoot: projectRoot,
		cfg:         cfg,
	}
}

type detectedLanguages struct {
	hasGo         bool
	hasTypeScript bool
	hasPython     bool
	tsconfigPath  string
}

// detectLanguages checks which languages are present in the project.
func (r *Resolver) detectLanguages() detectedLanguages {
	dl := detectedLanguages{}

	// Go: go.mod exists
	if _, err := os.Stat(filepath.Join(r.projectRoot, "go.mod")); err == nil {
		dl.hasGo = true
	}

	// TypeScript: tsconfig.json exists
	tsconfigPath := r.cfg.Analyzers.TypeScript.TsconfigPath
	if tsconfigPath == "" {
		tsconfigPath = "tsconfig.json"
	}
	if _, err := os.Stat(filepath.Join(r.projectRoot, tsconfigPath)); err == nil {
		dl.hasTypeScript = true
		dl.tsconfigPath = filepath.Join(r.projectRoot, tsconfigPath)
	}

	// Python: .py files exist (skip venv and node_modules)
	filepath.WalkDir(r.projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == "venv" || base == ".venv" || base == "node_modules" ||
				base == "__pycache__" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".py") {
			dl.hasPython = true
			return filepath.SkipAll
		}
		return nil
	})

	return dl
}

// Analyze detects languages and dispatches to appropriate analyzers.
func (r *Resolver) Analyze() (*AnalysisResult, error) {
	dl := r.detectLanguages()
	result := &AnalysisResult{}

	slog.Info("detected languages",
		"go", dl.hasGo,
		"typescript", dl.hasTypeScript,
		"python", dl.hasPython,
	)

	// Go first (fastest, most accurate)
	if dl.hasGo && r.cfg.Analyzers.Go.Enabled {
		slog.Info("running Go analyzer")
		analyzer, err := NewGoAnalyzer(r.projectRoot)
		if err != nil {
			slog.Warn("Go analyzer init failed, skipping", "error", err)
		} else {
			goResult, err := analyzer.Analyze()
			if err != nil {
				slog.Warn("Go analysis failed", "error", err)
			} else {
				result.Merge(goResult)
			}
		}
	}

	// TypeScript second (subprocess overhead)
	if dl.hasTypeScript && r.cfg.Analyzers.TypeScript.Enabled {
		slog.Info("running TypeScript analyzer")
		analyzer, err := NewTSAnalyzer()
		if err != nil {
			slog.Warn("TS analyzer init failed, skipping", "error", err)
		} else {
			tsResult, err := analyzer.Analyze(r.projectRoot, dl.tsconfigPath)
			if err != nil {
				slog.Warn("TypeScript analysis failed", "error", err)
			} else {
				result.Merge(tsResult)
			}
		}
	}

	// Python third (tree-sitter, fast but least coverage)
	if dl.hasPython && r.cfg.Analyzers.Python.Enabled {
		slog.Info("running Python analyzer")
		analyzer := NewPythonAnalyzer(r.projectRoot, nil, nil)
		pyResult, err := analyzer.Analyze()
		if err != nil {
			slog.Warn("Python analysis failed", "error", err)
		} else {
			result.Merge(pyResult)
		}
	}

	slog.Info("graph analysis complete",
		"total_symbols", len(result.Symbols),
		"total_edges", len(result.Edges),
		"total_boundary", len(result.BoundarySymbols),
	)

	return result, nil
}
