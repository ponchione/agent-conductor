package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
)

func TestResolver_DetectLanguages_Go(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)

	cfg := &config.GraphConfig{Enabled: true}
	r := NewResolver(tmpDir, cfg)

	langs := r.detectLanguages()

	if !langs.hasGo {
		t.Error("expected Go to be detected")
	}
	if langs.hasTypeScript {
		t.Error("did not expect TypeScript to be detected")
	}
	if langs.hasPython {
		t.Error("did not expect Python to be detected")
	}
}

func TestResolver_DetectLanguages_TypeScript(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte("{}"), 0644)

	cfg := &config.GraphConfig{Enabled: true}
	r := NewResolver(tmpDir, cfg)

	langs := r.detectLanguages()

	if langs.hasGo {
		t.Error("did not expect Go to be detected")
	}
	if !langs.hasTypeScript {
		t.Error("expected TypeScript to be detected")
	}
	if langs.tsconfigPath != filepath.Join(tmpDir, "tsconfig.json") {
		t.Errorf("tsconfigPath = %q, want %q", langs.tsconfigPath, filepath.Join(tmpDir, "tsconfig.json"))
	}
}

func TestResolver_DetectLanguages_Python(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "src", "app.py"), []byte("print('hello')"), 0644)

	cfg := &config.GraphConfig{Enabled: true}
	r := NewResolver(tmpDir, cfg)

	langs := r.detectLanguages()

	if !langs.hasPython {
		t.Error("expected Python to be detected")
	}
}

func TestResolver_DetectLanguages_PythonSkipsVenv(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "venv", "lib"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "venv", "lib", "site.py"), []byte("# venv file"), 0644)

	cfg := &config.GraphConfig{Enabled: true}
	r := NewResolver(tmpDir, cfg)

	langs := r.detectLanguages()

	if langs.hasPython {
		t.Error("expected Python NOT to be detected (only in venv)")
	}
}

func TestResolver_DetectLanguages_CustomTsconfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "web"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "web", "tsconfig.json"), []byte("{}"), 0644)

	cfg := &config.GraphConfig{
		Enabled: true,
		Analyzers: config.AnalyzerConfigs{
			TypeScript: config.TSAnalyzerConfig{
				TsconfigPath: "web/tsconfig.json",
			},
		},
	}
	r := NewResolver(tmpDir, cfg)

	langs := r.detectLanguages()

	if !langs.hasTypeScript {
		t.Error("expected TypeScript to be detected with custom tsconfig path")
	}
}

func TestResolver_DetectLanguages_AllLanguages(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.py"), []byte("print('hello')"), 0644)

	cfg := &config.GraphConfig{Enabled: true}
	r := NewResolver(tmpDir, cfg)

	langs := r.detectLanguages()

	if !langs.hasGo {
		t.Error("expected Go to be detected")
	}
	if !langs.hasTypeScript {
		t.Error("expected TypeScript to be detected")
	}
	if !langs.hasPython {
		t.Error("expected Python to be detected")
	}
}

func TestResolver_DetectLanguages_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.GraphConfig{Enabled: true}
	r := NewResolver(tmpDir, cfg)

	langs := r.detectLanguages()

	if langs.hasGo || langs.hasTypeScript || langs.hasPython {
		t.Error("expected no languages to be detected in empty directory")
	}
}

func TestResolver_Analyze_NoLanguagesEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)

	cfg := &config.GraphConfig{
		Enabled: true,
		// All analyzers disabled (default zero values)
	}
	r := NewResolver(tmpDir, cfg)

	result, err := r.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(result.Symbols))
	}
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(result.Edges))
	}
}
