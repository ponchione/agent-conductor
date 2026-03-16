package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/models"
)

func TestRunPreChecksUsesConfiguredCommandForVersionTwoCriteria(t *testing.T) {
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
			},
		},
	}

	required := true
	wo := &models.WorkOrder{
		SchemaVersion: 2,
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
		},
	}

	original := runVerifyCommand
	t.Cleanup(func() { runVerifyCommand = original })

	var gotDir string
	var gotArgv []string
	var gotDeadline bool
	runVerifyCommand = func(ctx context.Context, dir string, env []string, argv []string) ([]byte, error) {
		gotDir = dir
		gotArgv = append([]string(nil), argv...)
		_, gotDeadline = ctx.Deadline()
		return []byte("ok"), nil
	}

	results := w.runPreChecks(context.Background(), wo)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].NormalizedResult() != models.CriterionResultMet {
		t.Fatalf("result = %q, want met (%s)", results[0].NormalizedResult(), results[0].Notes)
	}
	if gotDir != subdir {
		t.Fatalf("command dir = %q, want %q", gotDir, subdir)
	}
	if strings.Join(gotArgv, " ") != "make test" {
		t.Fatalf("command argv = %#v, want [\"make\", \"test\"]", gotArgv)
	}
	if !gotDeadline {
		t.Fatal("expected command context to have a deadline")
	}
	if !strings.Contains(results[0].Notes, `precheck "test" passed`) {
		t.Fatalf("notes = %q, want configured precheck success note", results[0].Notes)
	}
}

func TestRunPreChecksReportsMissingConfiguredCommand(t *testing.T) {
	w := &Worker{
		cfg: &config.ProjectConfig{
			Project: config.Project{Path: t.TempDir()},
		},
	}

	required := true
	wo := &models.WorkOrder{
		SchemaVersion: 2,
		TypedAcceptanceCriteria: []models.TypedAcceptanceCriterion{
			{
				ID:             "AC-1",
				Description:    "Configured lint precheck passes",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification: models.AcceptanceVerification{
					Kind:  "precheck",
					Check: "lint",
				},
			},
		},
	}

	results := w.runPreChecks(context.Background(), wo)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].NormalizedResult() != models.CriterionResultUnassessable {
		t.Fatalf("result = %q, want unassessable", results[0].NormalizedResult())
	}
	if !strings.Contains(results[0].Notes, `precheck "lint" is not configured under verify.commands`) {
		t.Fatalf("notes = %q, want missing configured command note", results[0].Notes)
	}
}

func TestRunPreChecksKeepsLegacyStringMatchingCompatibility(t *testing.T) {
	w := &Worker{
		cfg: &config.ProjectConfig{
			Project: config.Project{Path: t.TempDir()},
			Guardrails: config.Guardrails{
				PhaseTimeoutSeconds: 300,
			},
		},
	}

	wo := &models.WorkOrder{
		AcceptanceCriteria: []string{"go test ./... passes"},
	}

	original := runVerifyCommand
	t.Cleanup(func() { runVerifyCommand = original })

	runVerifyCommand = func(ctx context.Context, dir string, env []string, argv []string) ([]byte, error) {
		if dir != w.cfg.Project.Path {
			return nil, fmt.Errorf("unexpected dir %q", dir)
		}
		if strings.Join(argv, " ") != "go test ./..." {
			return nil, fmt.Errorf("unexpected argv %q", strings.Join(argv, " "))
		}
		return []byte("ok"), nil
	}

	results := w.runPreChecks(context.Background(), wo)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].NormalizedResult() != models.CriterionResultMet {
		t.Fatalf("result = %q, want met (%s)", results[0].NormalizedResult(), results[0].Notes)
	}
	if !strings.Contains(results[0].Notes, "legacy precheck passed: go test ./...") {
		t.Fatalf("notes = %q, want legacy precheck note", results[0].Notes)
	}
}

func TestRunPreChecksExecutesConfiguredSmokeRoutine(t *testing.T) {
	w := &Worker{
		cfg: &config.ProjectConfig{
			Project: config.Project{Path: t.TempDir()},
			Guardrails: config.Guardrails{
				PhaseTimeoutSeconds: 300,
			},
			Verify: config.Verify{
				Smoke: map[string]config.VerifySmoke{
					"assets": {
						Command: config.VerifyCommand{
							Argv: []string{"make", "smoke"},
						},
					},
				},
			},
		},
	}

	required := true
	wo := &models.WorkOrder{
		SchemaVersion: 2,
		TypedAcceptanceCriteria: []models.TypedAcceptanceCriterion{
			{
				ID:             "AC-1",
				Description:    "Observability assets remain reachable",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification: models.AcceptanceVerification{
					Kind:  "http_smoke",
					Route: "assets",
				},
			},
		},
	}

	original := runVerifyCommand
	t.Cleanup(func() { runVerifyCommand = original })

	runVerifyCommand = func(ctx context.Context, dir string, env []string, argv []string) ([]byte, error) {
		if strings.Join(argv, " ") != "make smoke" {
			return nil, fmt.Errorf("unexpected argv %q", strings.Join(argv, " "))
		}
		return []byte("ok"), nil
	}

	results := w.runPreChecks(context.Background(), wo)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].NormalizedResult() != models.CriterionResultMet {
		t.Fatalf("result = %q, want met", results[0].NormalizedResult())
	}
	if !strings.Contains(results[0].Notes, `http_smoke "assets" passed`) {
		t.Fatalf("notes = %q, want smoke success note", results[0].Notes)
	}
}

func TestPrecheckTimeoutFallsBackToGuardrailDefault(t *testing.T) {
	w := &Worker{
		cfg: &config.ProjectConfig{
			Guardrails: config.Guardrails{
				PhaseTimeoutSeconds: 42,
			},
		},
	}

	if got := w.precheckTimeout(0); got != 42*time.Second {
		t.Fatalf("precheckTimeout(0) = %s, want 42s", got)
	}
}
