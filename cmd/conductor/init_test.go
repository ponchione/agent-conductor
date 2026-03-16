package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteInitProjectYAMLIncludesPlannerPromptEntries(t *testing.T) {
	outputDir := t.TempDir()
	pc := &initProjectConfig{
		ProjectName:      "demo",
		ProjectLanguage:  "go",
		ProjectFramework: "chi",
		IndexInclude:     []string{"**/*.go"},
		IndexExclude:     []string{"**/.git/**"},
		ModulePath:       "internal",
		ModuleStructure:  []string{"cmd/conductor", "internal"},
		SharedPath:       "internal/shared",
		SQLPath:          "sql",
	}

	if err := writeInitProjectYAML(pc, outputDir); err != nil {
		t.Fatalf("writeInitProjectYAML() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "project.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "  plan: templates/plan-prompt.md\n") {
		t.Fatalf("project.yaml missing plan prompt entry:\n%s", content)
	}
	if !strings.Contains(content, "  plan_audit: templates/plan-audit.md\n") {
		t.Fatalf("project.yaml missing plan_audit prompt entry:\n%s", content)
	}
}
