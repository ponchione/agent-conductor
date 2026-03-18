package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExpandsEnvVars(t *testing.T) {
	t.Setenv("TEST_CONDUCTOR_KEY", "sk-secret-123")

	yaml := `
project:
  name: test-project
  path: /tmp/test
models:
  providers:
    anthropic:
      endpoint: https://api.anthropic.com/v1
      model: claude-sonnet-4-20250514
      api_key: ${TEST_CONDUCTOR_KEY}
`
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	p, ok := cfg.Models.Providers["anthropic"]
	if !ok {
		t.Fatal("expected 'anthropic' provider to exist")
	}

	if p.APIKey != "sk-secret-123" {
		t.Errorf("APIKey = %q, want %q", p.APIKey, "sk-secret-123")
	}
	if p.Endpoint != "https://api.anthropic.com/v1" {
		t.Errorf("Endpoint = %q, want %q", p.Endpoint, "https://api.anthropic.com/v1")
	}
}

func TestLoadGuardrailsDefaults(t *testing.T) {
	yaml := `
project:
  name: test-project
  path: /tmp/test
`
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Guardrails.MaxInvestigationTargets != 6 {
		t.Errorf("MaxInvestigationTargets = %d, want 6", cfg.Guardrails.MaxInvestigationTargets)
	}
	if cfg.Guardrails.MaxSubCallsTotal != 12 {
		t.Errorf("MaxSubCallsTotal = %d, want 12", cfg.Guardrails.MaxSubCallsTotal)
	}
	if cfg.Guardrails.PhaseTimeoutSeconds != 300 {
		t.Errorf("PhaseTimeoutSeconds = %d, want 300", cfg.Guardrails.PhaseTimeoutSeconds)
	}
	if cfg.Guardrails.MaxCostPerPhaseUSD != 0.50 {
		t.Errorf("MaxCostPerPhaseUSD = %f, want 0.50", cfg.Guardrails.MaxCostPerPhaseUSD)
	}
	if cfg.Guardrails.WarnCostPerPhaseUSD != 0.10 {
		t.Errorf("WarnCostPerPhaseUSD = %f, want 0.10", cfg.Guardrails.WarnCostPerPhaseUSD)
	}
}

func TestLoadVerifyCommandConfig(t *testing.T) {
	yaml := `
project:
  name: test-project
  path: /tmp/test
verify:
  commands:
    build:
      argv: ["make", "build"]
      workdir: .
      timeout_seconds: 120
    test:
      argv: ["make", "test"]
  smoke:
    assets:
      command:
        argv: ["go", "test", "./internal/api", "-run", "TestSmokeAssets"]
        timeout_seconds: 180
`
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	build, ok := cfg.Verify.Commands["build"]
	if !ok {
		t.Fatal("expected verify.commands.build to exist")
	}
	if got := len(build.Argv); got != 2 {
		t.Fatalf("len(build.Argv) = %d, want 2", got)
	}
	if build.Argv[0] != "make" || build.Argv[1] != "build" {
		t.Fatalf("build.Argv = %#v, want [\"make\", \"build\"]", build.Argv)
	}
	if build.Workdir != "." {
		t.Fatalf("build.Workdir = %q, want \".\"", build.Workdir)
	}
	if build.TimeoutSeconds != 120 {
		t.Fatalf("build.TimeoutSeconds = %d, want 120", build.TimeoutSeconds)
	}

	smoke, ok := cfg.Verify.Smoke["assets"]
	if !ok {
		t.Fatal("expected verify.smoke.assets to exist")
	}
	if got := len(smoke.Command.Argv); got != 5 {
		t.Fatalf("len(smoke.Command.Argv) = %d, want 5", got)
	}
	if smoke.Command.TimeoutSeconds != 180 {
		t.Fatalf("smoke.Command.TimeoutSeconds = %d, want 180", smoke.Command.TimeoutSeconds)
	}
}

func TestValidateModelRoutingRequiresExplicitProviderTypeAndRoles(t *testing.T) {
	cfg := &ProjectConfig{
		Models: Models{
			Providers: map[string]ProviderConfig{
				"local": {Model: "deepseek-r1"},
			},
			Roles: map[string]string{
				"describe": "local",
			},
		},
	}

	err := ValidateModelRouting(cfg, []string{"describe"})
	if err == nil || err.Error() != "models.providers.local.type is required" {
		t.Fatalf("ValidateModelRouting() error = %v, want explicit provider type error", err)
	}

	cfg.Models.Providers["local"] = ProviderConfig{Type: "openai", Model: "deepseek-r1"}
	delete(cfg.Models.Roles, "describe")
	err = ValidateModelRouting(cfg, []string{"describe"})
	if err == nil || err.Error() != "models.roles.describe is required" {
		t.Fatalf("ValidateModelRouting() error = %v, want missing role error", err)
	}
}
