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
