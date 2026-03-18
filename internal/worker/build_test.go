package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/verify"
)

func TestCollectBuildValidationEvidenceRunsConfiguredChecks(t *testing.T) {
	projectDir := t.TempDir()
	subdir := filepath.Join(projectDir, "repo")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	w := &Worker{
		cfg: &config.ProjectConfig{
			Project: config.Project{Path: projectDir},
			Guardrails: config.Guardrails{
				PhaseTimeoutSeconds: 300,
			},
			Verify: config.Verify{
				Commands: map[string]config.VerifyCommand{
					"test": {
						Argv:           []string{"make", "test"},
						Workdir:        "repo",
						TimeoutSeconds: 45,
					},
				},
				Smoke: map[string]config.VerifySmoke{
					"assets": {
						Command: config.VerifyCommand{
							Argv:           []string{"make", "smoke"},
							Workdir:        "repo",
							TimeoutSeconds: 30,
						},
					},
				},
			},
		},
	}

	required := true
	wo := &models.WorkOrder{
		SchemaVersion: models.WorkOrderSchemaVersion,
		TypedAcceptanceCriteria: []models.TypedAcceptanceCriterion{
			{
				ID:             "AC-1",
				Description:    "Configured test precheck passes",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification: models.AcceptanceVerification{
					Kind:  "precheck",
					Check: "test",
				},
			},
			{
				ID:             "AC-2",
				Description:    "Observability assets remain reachable",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification: models.AcceptanceVerification{
					Kind:  "http_smoke",
					Route: "assets",
				},
			},
			{
				ID:             "AC-3",
				Description:    "Configured test precheck passes again",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification: models.AcceptanceVerification{
					Kind:  "precheck",
					Check: "test",
				},
			},
		},
	}

	original := runVerifyCommand
	t.Cleanup(func() { runVerifyCommand = original })

	var calls []string
	runVerifyCommand = func(ctx context.Context, dir string, env []string, argv []string) ([]byte, error) {
		calls = append(calls, dir+"::"+strings.Join(argv, " "))
		return []byte("ok"), nil
	}

	evidence := w.collectBuildValidationEvidence(context.Background(), wo)
	if evidence.Phase != "build" {
		t.Fatalf("Phase = %q, want build", evidence.Phase)
	}
	if len(evidence.Commands) != 1 {
		t.Fatalf("len(Commands) = %d, want 1", len(evidence.Commands))
	}
	if len(evidence.SmokeChecks) != 1 {
		t.Fatalf("len(SmokeChecks) = %d, want 1", len(evidence.SmokeChecks))
	}
	if len(calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(calls))
	}
	if evidence.Commands[0].Name != "test" {
		t.Fatalf("command Name = %q, want test", evidence.Commands[0].Name)
	}
	if strings.Join(evidence.Commands[0].Argv, " ") != "make test" {
		t.Fatalf("command Argv = %#v, want [\"make\", \"test\"]", evidence.Commands[0].Argv)
	}
	if evidence.Commands[0].Workdir != subdir {
		t.Fatalf("command Workdir = %q, want %q", evidence.Commands[0].Workdir, subdir)
	}
	if evidence.Commands[0].Result != models.CriterionResultMet {
		t.Fatalf("command Result = %q, want met", evidence.Commands[0].Result)
	}
	if !strings.Contains(evidence.Commands[0].Notes, `precheck "test" passed`) {
		t.Fatalf("command Notes = %q, want precheck success note", evidence.Commands[0].Notes)
	}
	if strings.Join(evidence.SmokeChecks[0].Argv, " ") != "make smoke" {
		t.Fatalf("smoke Argv = %#v, want [\"make\", \"smoke\"]", evidence.SmokeChecks[0].Argv)
	}
	if evidence.SmokeChecks[0].Workdir != subdir {
		t.Fatalf("smoke Workdir = %q, want %q", evidence.SmokeChecks[0].Workdir, subdir)
	}
	if evidence.SmokeChecks[0].Result != models.CriterionResultMet {
		t.Fatalf("smoke Result = %q, want met", evidence.SmokeChecks[0].Result)
	}
	if !strings.Contains(evidence.SmokeChecks[0].Notes, `http_smoke "assets" passed`) {
		t.Fatalf("smoke Notes = %q, want smoke success note", evidence.SmokeChecks[0].Notes)
	}
}

func TestWriteBuildValidationEvidencePersistsEntries(t *testing.T) {
	dataDir := t.TempDir()
	w := &Worker{
		cfg: &config.ProjectConfig{
			Project: config.Project{DataDir: dataDir},
		},
	}

	path, err := w.writeBuildValidationEvidence("wf-123", &verify.ValidationEvidence{
		Phase: "build",
		Commands: []verify.ValidationEvidenceEntry{
			{Name: "test", Argv: []string{"make", "test"}, Workdir: "/tmp/repo", Result: models.CriterionResultMet, Notes: "ok"},
		},
	})
	if err != nil {
		t.Fatalf("writeBuildValidationEvidence() error = %v", err)
	}

	evidence, err := verify.LoadValidationEvidence(path)
	if err != nil {
		t.Fatalf("LoadValidationEvidence() error = %v", err)
	}
	if len(evidence.Commands) != 1 {
		t.Fatalf("len(Commands) = %d, want 1", len(evidence.Commands))
	}
	if evidence.Commands[0].Workdir != "/tmp/repo" {
		t.Fatalf("Workdir = %q, want /tmp/repo", evidence.Commands[0].Workdir)
	}
}
