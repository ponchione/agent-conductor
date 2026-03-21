package api

import (
	"sync"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
)

func TestOverrideStoreGetAll_Empty(t *testing.T) {
	t.Parallel()
	store := NewOverrideStore()
	got := store.GetAll()
	if got == nil || len(got) != 0 {
		t.Fatalf("GetAll() on empty store = %v, want empty map", got)
	}
}

func TestOverrideStoreSetAndGet(t *testing.T) {
	t.Parallel()
	store := NewOverrideStore()
	overrides := map[string]ModelOverride{
		"build": {Provider: "claude-cli", Model: "claude-sonnet-4-20250514"},
	}
	store.Set(overrides)

	got := store.GetAll()
	if len(got) != 1 {
		t.Fatalf("GetAll() len = %d, want 1", len(got))
	}
	if got["build"].Model != "claude-sonnet-4-20250514" {
		t.Fatalf("GetAll()[build].Model = %q, want claude-sonnet-4-20250514", got["build"].Model)
	}
}

func TestOverrideStoreGetSingle(t *testing.T) {
	t.Parallel()
	store := NewOverrideStore()
	store.Set(map[string]ModelOverride{
		"build": {Provider: "claude-cli", Model: "opus"},
	})

	override, ok := store.Get("build")
	if !ok {
		t.Fatal("Get(build) returned false, want true")
	}
	if override.Model != "opus" {
		t.Fatalf("Get(build).Model = %q, want opus", override.Model)
	}

	_, ok = store.Get("nonexistent")
	if ok {
		t.Fatal("Get(nonexistent) returned true, want false")
	}
}

func TestOverrideStoreSetReplacesAll(t *testing.T) {
	t.Parallel()
	store := NewOverrideStore()
	store.Set(map[string]ModelOverride{
		"build":          {Provider: "claude-cli", Model: "opus"},
		"verify_analyze": {Provider: "claude-cli", Model: "sonnet"},
	})
	store.Set(map[string]ModelOverride{
		"build": {Provider: "claude-cli", Model: "haiku"},
	})

	got := store.GetAll()
	if len(got) != 1 {
		t.Fatalf("GetAll() len = %d after replace, want 1", len(got))
	}
	if got["build"].Model != "haiku" {
		t.Fatalf("GetAll()[build].Model = %q, want haiku", got["build"].Model)
	}
}

func TestOverrideStoreClear(t *testing.T) {
	t.Parallel()
	store := NewOverrideStore()
	store.Set(map[string]ModelOverride{
		"build": {Provider: "claude-cli", Model: "opus"},
	})
	store.Clear()
	got := store.GetAll()
	if len(got) != 0 {
		t.Fatalf("GetAll() after Clear() len = %d, want 0", len(got))
	}
}

func TestOverrideStoreGetAllReturnsCopy(t *testing.T) {
	t.Parallel()
	store := NewOverrideStore()
	store.Set(map[string]ModelOverride{
		"build": {Provider: "claude-cli", Model: "opus"},
	})

	got := store.GetAll()
	got["injected"] = ModelOverride{Provider: "evil", Model: "evil"}

	after := store.GetAll()
	if len(after) != 1 {
		t.Fatalf("mutating GetAll() result changed store: len = %d, want 1", len(after))
	}
}

func TestOverrideStoreConcurrentAccess(t *testing.T) {
	t.Parallel()
	store := NewOverrideStore()
	var wg sync.WaitGroup

	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			store.Set(map[string]ModelOverride{
				"build": {Provider: "p", Model: "m"},
			})
		}()
		go func() {
			defer wg.Done()
			_ = store.GetAll()
		}()
	}
	wg.Wait()
}

func TestValidateOverrides_Valid(t *testing.T) {
	t.Parallel()
	knownRoles := map[string]string{"build": "claude-cli", "decompose": "local"}
	providers := map[string]config.ProviderConfig{
		"claude-cli": {Model: "opus"},
		"local":      {Model: "qwen2.5-coder-7b-instruct"},
	}

	overrides := map[string]ModelOverride{
		"build": {Provider: "claude-cli", Model: "opus"},
	}
	if err := ValidateOverrides(overrides, knownRoles, providers); err != nil {
		t.Fatalf("ValidateOverrides() error = %v, want nil", err)
	}
}

func TestValidateOverrides_UnknownRole(t *testing.T) {
	t.Parallel()
	knownRoles := map[string]string{"build": "claude-cli"}
	providers := map[string]config.ProviderConfig{
		"claude-cli": {Model: "opus"},
	}

	overrides := map[string]ModelOverride{
		"nonexistent": {Provider: "claude-cli", Model: "opus"},
	}
	if err := ValidateOverrides(overrides, knownRoles, providers); err == nil {
		t.Fatal("ValidateOverrides() error = nil, want error for unknown role")
	}
}

func TestValidateOverrides_UnknownProvider(t *testing.T) {
	t.Parallel()
	knownRoles := map[string]string{"build": "claude-cli"}
	providers := map[string]config.ProviderConfig{
		"claude-cli": {Model: "opus"},
	}

	overrides := map[string]ModelOverride{
		"build": {Provider: "unknown-provider", Model: "opus"},
	}
	if err := ValidateOverrides(overrides, knownRoles, providers); err == nil {
		t.Fatal("ValidateOverrides() error = nil, want error for unknown provider")
	}
}

func TestValidateOverrides_UnknownModel(t *testing.T) {
	t.Parallel()
	knownRoles := map[string]string{"build": "claude-cli"}
	providers := map[string]config.ProviderConfig{
		"claude-cli": {Model: "opus"},
	}

	overrides := map[string]ModelOverride{
		"build": {Provider: "claude-cli", Model: "bogus-model"},
	}
	if err := ValidateOverrides(overrides, knownRoles, providers); err == nil {
		t.Fatal("ValidateOverrides() error = nil, want error for unknown model")
	}
}

func TestValidateOverrides_EmptyIsValid(t *testing.T) {
	t.Parallel()
	if err := ValidateOverrides(nil, nil, nil); err != nil {
		t.Fatalf("ValidateOverrides(nil) error = %v, want nil", err)
	}
}

func TestResolveModel_PerWorkflowWins(t *testing.T) {
	t.Parallel()
	store := NewOverrideStore()
	store.Set(map[string]ModelOverride{
		"build": {Provider: "claude-cli", Model: "session-model"},
	})
	perWorkflow := map[string]ModelOverride{
		"build": {Provider: "claude-cli", Model: "workflow-model"},
	}
	projectDefault := ModelOverride{Provider: "claude-cli", Model: "default-model"}

	got := ResolveModel("build", perWorkflow, store, projectDefault)
	if got.Model != "workflow-model" {
		t.Fatalf("ResolveModel() = %q, want workflow-model", got.Model)
	}
}

func TestResolveModel_SessionWinsOverDefault(t *testing.T) {
	t.Parallel()
	store := NewOverrideStore()
	store.Set(map[string]ModelOverride{
		"build": {Provider: "claude-cli", Model: "session-model"},
	})
	projectDefault := ModelOverride{Provider: "claude-cli", Model: "default-model"}

	got := ResolveModel("build", nil, store, projectDefault)
	if got.Model != "session-model" {
		t.Fatalf("ResolveModel() = %q, want session-model", got.Model)
	}
}

func TestResolveModel_FallsBackToDefault(t *testing.T) {
	t.Parallel()
	store := NewOverrideStore()
	projectDefault := ModelOverride{Provider: "claude-cli", Model: "default-model"}

	got := ResolveModel("build", nil, store, projectDefault)
	if got.Model != "default-model" {
		t.Fatalf("ResolveModel() = %q, want default-model", got.Model)
	}
}

func TestResolveModel_NilStore(t *testing.T) {
	t.Parallel()
	projectDefault := ModelOverride{Provider: "claude-cli", Model: "default-model"}

	got := ResolveModel("build", nil, nil, projectDefault)
	if got.Model != "default-model" {
		t.Fatalf("ResolveModel() = %q, want default-model", got.Model)
	}
}
