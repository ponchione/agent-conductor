package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/models"
)

func TestWriteInitProjectYAMLUsesCanonicalProjectShape(t *testing.T) {
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

	if strings.Contains(content, "\nprompts:\n") {
		t.Fatalf("project.yaml unexpectedly includes prompts section:\n%s", content)
	}
	if strings.Contains(content, "\nexecutor:\n") {
		t.Fatalf("project.yaml unexpectedly includes executor section:\n%s", content)
	}
	if strings.Contains(content, "\nmodels:\n") {
		t.Fatalf("project.yaml unexpectedly includes models section:\n%s", content)
	}
	if !strings.Contains(content, "\nverify:\n") {
		t.Fatalf("project.yaml missing verify section:\n%s", content)
	}
	if !strings.Contains(content, "    build:\n") || !strings.Contains(content, "        - \"go\"\n") {
		t.Fatalf("project.yaml missing go build verify command:\n%s", content)
	}
	if !strings.Contains(content, "    test:\n") || !strings.Contains(content, "        - \"test\"\n") {
		t.Fatalf("project.yaml missing go test verify command:\n%s", content)
	}
}

func TestWriteBootstrapYAMLUsesSchemaVersionTwoShape(t *testing.T) {
	outputDir := t.TempDir()
	required := true
	bwo := &initBootstrapWO{
		Title:           "Bootstrap project skeleton",
		Type:            "bootstrap",
		TargetModule:    ".",
		ReferenceModule: "",
		KnownFiles:      []string{"go.mod", "cmd/server/main.go", ".gitignore"},
		Requirements: []models.WorkOrderRequirement{
			{ID: "REQ-1", Text: "Project skeleton exists"},
		},
		AcceptanceCriteria: []models.TypedAcceptanceCriterion{
			{
				ID:             "AC-1",
				Description:    "Bootstrap files are created",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification: models.AcceptanceVerification{
					Kind:  "diff_review",
					Focus: []string{"go.mod", "cmd/server/main.go"},
				},
			},
			{
				ID:             "AC-2",
				Description:    "The project builds",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification: models.AcceptanceVerification{
					Kind:  "precheck",
					Check: "build",
				},
			},
		},
		Constraints: []string{"Do not add unused dependencies"},
	}

	if err := writeBootstrapYAML(bwo, outputDir); err != nil {
		t.Fatalf("writeBootstrapYAML() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "bootstrap.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)

	for _, snippet := range []string{
		"schema_version: 2",
		"requirements:",
		"acceptance_criteria:",
		"id: REQ-1",
		"id: AC-1",
		"kind: diff_review",
		"check: build",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("bootstrap.yaml missing %q:\n%s", snippet, content)
		}
	}
}

func TestParseInitResponseAcceptsTypedBootstrapWorkOrder(t *testing.T) {
	raw := `{
	  "project_config": {
	    "project_name": "demo",
	    "project_language": "go",
	    "project_framework": "chi",
	    "index_include": ["**/*.go"],
	    "index_exclude": ["**/.git/**"],
	    "module_path": "internal",
	    "module_structure": ["cmd/server", "internal"],
	    "shared_path": "internal/shared",
	    "sql_path": "sql"
	  },
	  "bootstrap": {
	    "title": "Bootstrap project skeleton",
	    "type": "bootstrap",
	    "target_module": ".",
	    "reference_module": "",
	    "known_files": ["go.mod", "cmd/server/main.go", ".gitignore"],
	    "requirements": [
	      {"id": "REQ-1", "text": "Project skeleton exists"}
	    ],
	    "acceptance_criteria": [
	      {
	        "id": "AC-1",
	        "description": "Bootstrap files are created",
	        "requirement_ids": ["REQ-1"],
	        "required": true,
	        "verification": {"kind": "diff_review", "focus": ["go.mod"]}
	      }
	    ],
	    "constraints": ["Do not add unused dependencies"]
	  }
	}`

	resp, err := parseInitResponse(raw)
	if err != nil {
		t.Fatalf("parseInitResponse() error = %v", err)
	}
	if got := len(resp.Bootstrap.Requirements); got != 1 {
		t.Fatalf("len(Requirements) = %d, want 1", got)
	}
	if got := len(resp.Bootstrap.AcceptanceCriteria); got != 1 {
		t.Fatalf("len(AcceptanceCriteria) = %d, want 1", got)
	}
	if resp.Bootstrap.AcceptanceCriteria[0].Verification.Kind != "diff_review" {
		t.Fatalf("Verification.Kind = %q, want diff_review", resp.Bootstrap.AcceptanceCriteria[0].Verification.Kind)
	}
	if resp.Bootstrap.AcceptanceCriteria[0].Required == nil || !*resp.Bootstrap.AcceptanceCriteria[0].Required {
		t.Fatal("expected required flag to be parsed")
	}
}
