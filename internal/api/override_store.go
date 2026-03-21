package api

import (
	"fmt"
	"maps"
	"sync"

	"github.com/ponchione/agent-conductor/internal/config"
)

// ModelOverride represents a provider/model override for a single role.
type ModelOverride struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// OverrideStore holds session-scoped role-to-model overrides in memory.
// Safe for concurrent access.
type OverrideStore struct {
	mu        sync.RWMutex
	overrides map[string]ModelOverride
}

// NewOverrideStore creates an empty OverrideStore.
func NewOverrideStore() *OverrideStore {
	return &OverrideStore{
		overrides: make(map[string]ModelOverride),
	}
}

// Get returns the override for a single role.
func (s *OverrideStore) Get(role string) (ModelOverride, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.overrides[role]
	return o, ok
}

// GetAll returns a copy of all overrides.
func (s *OverrideStore) GetAll() map[string]ModelOverride {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]ModelOverride, len(s.overrides))
	maps.Copy(out, s.overrides)
	return out
}

// Set replaces all overrides atomically. Roles omitted are cleared.
func (s *OverrideStore) Set(overrides map[string]ModelOverride) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides = make(map[string]ModelOverride, len(overrides))
	maps.Copy(s.overrides, overrides)
}

// Clear removes all overrides.
func (s *OverrideStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides = make(map[string]ModelOverride)
}

// ValidateOverrides checks that all role names, provider names, and model
// names are known. Each override's model must match one of the provider's
// configured models.
func ValidateOverrides(overrides map[string]ModelOverride, knownRoles map[string]string, providers map[string]config.ProviderConfig) error {
	for role, override := range overrides {
		if _, ok := knownRoles[role]; !ok {
			return fmt.Errorf("unknown role %q", role)
		}
		prov, ok := providers[override.Provider]
		if !ok {
			return fmt.Errorf("unknown provider %q for role %q", override.Provider, role)
		}
		if override.Model != prov.Model {
			return fmt.Errorf("unknown model %q for provider %q (role %q)", override.Model, override.Provider, role)
		}
	}
	return nil
}

// ResolveModel determines the effective model for a role using the three-level
// priority chain: per-workflow > session > project default.
// sessionStore may be nil (CLI mode — falls through to projectDefault).
func ResolveModel(role string, perWorkflow map[string]ModelOverride, sessionStore *OverrideStore, projectDefault ModelOverride) ModelOverride {
	if override, ok := perWorkflow[role]; ok {
		return override
	}
	if sessionStore != nil {
		if override, ok := sessionStore.Get(role); ok {
			return override
		}
	}
	return projectDefault
}
