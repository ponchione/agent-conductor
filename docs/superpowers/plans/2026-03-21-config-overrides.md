# Config & Overrides Implementation Plan (Spec 06)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add session-scoped model override system with sidebar config panel, per-workflow queue overrides, and supporting API endpoints.

**Architecture:** In-memory OverrideStore (mutex-protected map) holds session overrides. Three new endpoints serve config/roles data and manage overrides. Frontend gets a useConfig hook feeding a ConfigPanel in the sidebar and per-item override dropdowns in QueueDrawer. Override resolution: per-workflow > session > project.yaml.

**Tech Stack:** Go (chi router, sync.RWMutex), React 19, TypeScript, Tailwind CSS, shadcn/ui

---

## File Structure

**New Go files:**
- `internal/api/override_store.go` — OverrideStore struct, ModelOverride type, validation, resolution
- `internal/api/override_store_test.go` — unit tests for the store
- `internal/api/config_handlers.go` — HTTP handlers for config/override endpoints
- `internal/api/config_handlers_test.go` — HTTP tests for config endpoints

**Modified Go files:**
- `internal/api/server.go` — add `cfg *config.ProjectConfig` to Server struct, add config routes
- `internal/api/server_test.go` — update all NewServer call sites with new parameter
- `cmd/conductor/serve.go` — pass `cfg` to NewServer

**New frontend files:**
- `web/src/hooks/useConfig.ts` — useConfig hook
- `web/src/components/ConfigPanel.tsx` — sidebar config panel with RoleOverrideDropdown

**Modified frontend files:**
- `web/src/types/api.ts` — add config types
- `web/src/api/client.ts` — add config API functions
- `web/src/components/Sidebar.tsx` — replace placeholder with ConfigPanel
- `web/src/components/QueueDrawer.tsx` — add per-item override dropdowns

---

## Task 1: OverrideStore — Core Data Structure

**Files:**
- Create: `internal/api/override_store.go`
- Test: `internal/api/override_store_test.go`

- [ ] **Step 1: Write failing tests for OverrideStore**

Create `internal/api/override_store_test.go`:

```go
package api

import (
	"sync"
	"testing"
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

	for i := 0; i < 50; i++ {
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/gernsback/source/agent-conductor && go test ./internal/api/ -run TestOverrideStore -v -count=1`
Expected: compilation error — OverrideStore, ModelOverride, NewOverrideStore not defined.

- [ ] **Step 3: Implement OverrideStore**

Create `internal/api/override_store.go`:

```go
package api

import "sync"

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
	for k, v := range s.overrides {
		out[k] = v
	}
	return out
}

// Set replaces all overrides atomically. Roles omitted are cleared.
func (s *OverrideStore) Set(overrides map[string]ModelOverride) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides = make(map[string]ModelOverride, len(overrides))
	for k, v := range overrides {
		s.overrides[k] = v
	}
}

// Clear removes all overrides.
func (s *OverrideStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides = make(map[string]ModelOverride)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/gernsback/source/agent-conductor && go test ./internal/api/ -run TestOverrideStore -v -count=1`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/override_store.go internal/api/override_store_test.go
git commit -m "feat(spec06): add OverrideStore with concurrent-safe session overrides"
```

---

## Task 2: Validation Functions

**Files:**
- Modify: `internal/api/override_store.go`
- Modify: `internal/api/override_store_test.go`

- [ ] **Step 1: Write failing tests for validation**

Append to `internal/api/override_store_test.go`:

```go
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

func TestValidateOverrides_EmptyIsValid(t *testing.T) {
	t.Parallel()
	if err := ValidateOverrides(nil, nil, nil); err != nil {
		t.Fatalf("ValidateOverrides(nil) error = %v, want nil", err)
	}
}
```

Add the import for config package in the test file's import block:
```go
"github.com/ponchione/agent-conductor/internal/config"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/gernsback/source/agent-conductor && go test ./internal/api/ -run TestValidateOverrides -v -count=1`
Expected: compilation error — ValidateOverrides not defined.

- [ ] **Step 3: Implement ValidateOverrides**

Add to `internal/api/override_store.go`:

```go
import (
	"fmt"
	"sync"

	"github.com/ponchione/agent-conductor/internal/config"
)

// ValidateOverrides checks that all role names and provider names are known.
func ValidateOverrides(overrides map[string]ModelOverride, knownRoles map[string]string, providers map[string]config.ProviderConfig) error {
	for role, override := range overrides {
		if _, ok := knownRoles[role]; !ok {
			return fmt.Errorf("unknown role %q", role)
		}
		if _, ok := providers[override.Provider]; !ok {
			return fmt.Errorf("unknown provider %q for role %q", override.Provider, role)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/gernsback/source/agent-conductor && go test ./internal/api/ -run TestValidateOverrides -v -count=1`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/override_store.go internal/api/override_store_test.go
git commit -m "feat(spec06): add ValidateOverrides for role/provider checking"
```

---

## Task 3: Override Resolution Function

**Files:**
- Modify: `internal/api/override_store.go`
- Modify: `internal/api/override_store_test.go`

- [ ] **Step 1: Write failing tests for ResolveModel**

Append to `internal/api/override_store_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/gernsback/source/agent-conductor && go test ./internal/api/ -run TestResolveModel -v -count=1`
Expected: compilation error — ResolveModel not defined.

- [ ] **Step 3: Implement ResolveModel**

Add to `internal/api/override_store.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/gernsback/source/agent-conductor && go test ./internal/api/ -run TestResolveModel -v -count=1`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/override_store.go internal/api/override_store_test.go
git commit -m "feat(spec06): add ResolveModel with three-level priority chain"
```

---

## Task 4: Add ProjectConfig to Server & Update Call Sites

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/server_test.go`
- Modify: `cmd/conductor/serve.go`

- [ ] **Step 1: Add cfg and overrideStore to Server struct, update NewServer signature**

In `internal/api/server.go`, add fields to the Server struct:

```go
type Server struct {
	db             *database.DB
	gitMgr         *git.GitManager
	baseBranch     string
	runQueue       *RunQueue
	workOrderDir   string
	cfg            *config.ProjectConfig
	overrideStore  *OverrideStore
}
```

Update `NewServer` signature and body:

```go
func NewServer(db *database.DB, gitMgr *git.GitManager, baseBranch string, runQueue *RunQueue, workOrderDir string, cfg *config.ProjectConfig) http.Handler {
	s := &Server{
		db: db, gitMgr: gitMgr, baseBranch: baseBranch,
		runQueue: runQueue, workOrderDir: workOrderDir,
		cfg: cfg, overrideStore: NewOverrideStore(),
	}
	// ... existing routes ...
```

Add import for config package:
```go
"github.com/ponchione/agent-conductor/internal/config"
```

- [ ] **Step 2: Update all NewServer call sites in server_test.go**

Find-and-replace every `NewServer(db, nil, "main", nil, "")` and `NewServer(db, nil, "main", rq, "")` call in `internal/api/server_test.go` to add the new `nil` parameter at the end:

- `NewServer(db, nil, "main", nil, "")` → `NewServer(db, nil, "main", nil, "", nil)`
- `NewServer(db, nil, "main", rq, "")` → `NewServer(db, nil, "main", rq, "", nil)`
- `NewServer(db, nil, "main", nil, dir)` → `NewServer(db, nil, "main", nil, dir, nil)`

- [ ] **Step 3: Update serve.go call site**

In `cmd/conductor/serve.go`, update the NewServer call:

```go
if err := http.ListenAndServe(serveAddr, api.NewServer(db, gitMgr, baseBranch, rq, workOrderDir, cfg)); err != nil {
```

- [ ] **Step 4: Run full test suite to verify nothing broke**

Run: `make test`
Expected: all 22+ packages pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/server_test.go cmd/conductor/serve.go
git commit -m "refactor(spec06): add ProjectConfig to Server for config endpoints"
```

---

## Task 5: Config & Override HTTP Handlers

**Files:**
- Create: `internal/api/config_handlers.go`
- Modify: `internal/api/server.go` (add routes)

- [ ] **Step 1: Create config handler file with response types and handlers**

Create `internal/api/config_handlers.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// --- Config response/request types ---

type roleConfigResponse struct {
	Name            string `json:"name"`
	CurrentProvider string `json:"current_provider"`
	CurrentModel    string `json:"current_model"`
	Description     string `json:"description"`
}

type providerModelsResponse struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

type projectInfoResponse struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	DataDir string `json:"data_dir"`
}

type configRolesResponse struct {
	Roles           []roleConfigResponse     `json:"roles"`
	AvailableModels []providerModelsResponse `json:"available_models"`
	Project         projectInfoResponse      `json:"project"`
}

type configOverridesResponse struct {
	Overrides map[string]ModelOverride `json:"overrides"`
}

type configOverridesRequest struct {
	Overrides map[string]ModelOverride `json:"overrides"`
}

// roleDescriptions maps role names to human-readable descriptions.
var roleDescriptions = map[string]string{
	"decompose":        "Decomposes work order into analysis targets",
	"analyze":          "Analyzes individual files for scope context",
	"crosscut":         "Identifies cross-cutting concerns across targets",
	"synthesize":       "Synthesizes scope analysis into context package",
	"describe":         "Generates natural language descriptions",
	"verify_analyze":   "Analyzes build output for correctness",
	"verify_synthesize": "Synthesizes verification results",
}

func (s *Server) handleGetConfigRoles(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "no project config loaded")
		return
	}

	roles := make([]roleConfigResponse, 0, len(s.cfg.Models.Roles))
	for roleName, providerName := range s.cfg.Models.Roles {
		provider, ok := s.cfg.Models.Providers[providerName]
		model := ""
		if ok {
			model = provider.Model
		}
		desc := roleDescriptions[roleName]
		roles = append(roles, roleConfigResponse{
			Name:            roleName,
			CurrentProvider: providerName,
			CurrentModel:    model,
			Description:     desc,
		})
	}

	availableModels := make([]providerModelsResponse, 0, len(s.cfg.Models.Providers))
	for provName, provCfg := range s.cfg.Models.Providers {
		availableModels = append(availableModels, providerModelsResponse{
			Provider: provName,
			Models:   []string{provCfg.Model},
		})
	}

	writeJSON(w, http.StatusOK, configRolesResponse{
		Roles:           roles,
		AvailableModels: availableModels,
		Project: projectInfoResponse{
			Name:    s.cfg.Project.Name,
			Path:    s.cfg.Project.Path,
			DataDir: s.cfg.Project.DataDir,
		},
	})
}

func (s *Server) handleGetConfigOverrides(w http.ResponseWriter, r *http.Request) {
	overrides := s.overrideStore.GetAll()
	if overrides == nil {
		overrides = make(map[string]ModelOverride)
	}
	writeJSON(w, http.StatusOK, configOverridesResponse{Overrides: overrides})
}

func (s *Server) handlePutConfigOverrides(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "no project config loaded")
		return
	}

	var req configOverridesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Overrides == nil {
		req.Overrides = make(map[string]ModelOverride)
	}

	if err := ValidateOverrides(req.Overrides, s.cfg.Models.Roles, s.cfg.Models.Providers); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.overrideStore.Set(req.Overrides)

	writeJSON(w, http.StatusOK, configOverridesResponse{Overrides: s.overrideStore.GetAll()})
}
```

- [ ] **Step 2: Register config routes in server.go**

In `internal/api/server.go`, add these routes before the SPA fallback (`r.Get("/*", ...)`):

```go
	// Config & override routes
	r.Get("/api/config/roles", s.handleGetConfigRoles)
	r.Get("/api/config/overrides", s.handleGetConfigOverrides)
	r.Put("/api/config/overrides", s.handlePutConfigOverrides)
```

- [ ] **Step 3: Verify build passes**

Run: `make build`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/api/config_handlers.go internal/api/server.go
git commit -m "feat(spec06): add config roles and override HTTP endpoints"
```

---

## Task 6: Config Endpoint Tests

**Files:**
- Create: `internal/api/config_handlers_test.go`

- [ ] **Step 1: Write config endpoint tests**

Create `internal/api/config_handlers_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
)

func testConfig() *config.ProjectConfig {
	return &config.ProjectConfig{
		Project: config.Project{
			Name:    "test-project",
			Path:    "/tmp/test-project",
			DataDir: "/tmp/test-project/.topham",
		},
		Models: config.Models{
			Providers: map[string]config.ProviderConfig{
				"claude-cli": {Type: "claude-cli", Model: "opus"},
				"local":      {Type: "openai", Model: "qwen2.5-coder-7b"},
			},
			Roles: map[string]string{
				"build":          "claude-cli",
				"decompose":      "local",
				"verify_analyze": "claude-cli",
			},
		},
	}
}

func TestGetConfigRoles(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	cfg := testConfig()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config/roles", nil)
	NewServer(db, nil, "main", nil, "", cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp configRolesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.Roles) != 3 {
		t.Fatalf("roles count = %d, want 3", len(resp.Roles))
	}
	if resp.Project.Name != "test-project" {
		t.Fatalf("project name = %q, want test-project", resp.Project.Name)
	}
	if len(resp.AvailableModels) != 2 {
		t.Fatalf("available_models count = %d, want 2", len(resp.AvailableModels))
	}
}

func TestGetConfigRoles_NilConfig(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config/roles", nil)
	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestGetConfigOverrides_Empty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config/overrides", nil)
	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp configOverridesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.Overrides) != 0 {
		t.Fatalf("overrides count = %d, want 0", len(resp.Overrides))
	}
}

func TestPutConfigOverrides_Valid(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	cfg := testConfig()

	body := `{"overrides":{"build":{"provider":"claude-cli","model":"opus"}}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/config/overrides", strings.NewReader(body))
	NewServer(db, nil, "main", nil, "", cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp configOverridesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Overrides["build"].Model != "opus" {
		t.Fatalf("overrides[build].model = %q, want opus", resp.Overrides["build"].Model)
	}
}

func TestPutConfigOverrides_UnknownRole(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	cfg := testConfig()

	body := `{"overrides":{"nonexistent":{"provider":"claude-cli","model":"opus"}}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/config/overrides", strings.NewReader(body))
	NewServer(db, nil, "main", nil, "", cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestPutConfigOverrides_UnknownProvider(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	cfg := testConfig()

	body := `{"overrides":{"build":{"provider":"unknown","model":"opus"}}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/config/overrides", strings.NewReader(body))
	NewServer(db, nil, "main", nil, "", cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestPutConfigOverrides_ClearsOmittedRoles(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	cfg := testConfig()
	handler := NewServer(db, nil, "main", nil, "", cfg)

	// Set two overrides
	body1 := `{"overrides":{"build":{"provider":"claude-cli","model":"opus"},"decompose":{"provider":"local","model":"qwen2.5-coder-7b"}}}`
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPut, "/api/config/overrides", strings.NewReader(body1))
	handler.ServeHTTP(rec1, req1)

	// Set only one — decompose should be cleared
	body2 := `{"overrides":{"build":{"provider":"claude-cli","model":"opus"}}}`
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPut, "/api/config/overrides", strings.NewReader(body2))
	handler.ServeHTTP(rec2, req2)

	var resp configOverridesResponse
	if err := json.NewDecoder(rec2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.Overrides) != 1 {
		t.Fatalf("overrides count = %d, want 1", len(resp.Overrides))
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd /home/gernsback/source/agent-conductor && go test ./internal/api/ -run TestGetConfig\|TestPutConfig -v -count=1`
Expected: all PASS.

- [ ] **Step 3: Run full test suite**

Run: `make test`
Expected: all packages pass.

- [ ] **Step 4: Commit**

```bash
git add internal/api/config_handlers_test.go
git commit -m "test(spec06): add config endpoint handler tests"
```

---

## Task 7: Frontend Types & API Client

**Files:**
- Modify: `web/src/types/api.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add config types to api.ts**

Append to `web/src/types/api.ts`:

```typescript
// --- Spec 06: Config & Override types ---

export interface RoleConfig {
  name: string;
  current_provider: string;
  current_model: string;
  description: string;
}

export interface ProviderModels {
  provider: string;
  models: string[];
}

export interface ProjectInfo {
  name: string;
  path: string;
  data_dir: string;
}

export interface ModelOverride {
  provider: string;
  model: string;
}

export interface ConfigRolesResponse {
  roles: RoleConfig[];
  available_models: ProviderModels[];
  project: ProjectInfo;
}

export interface ConfigOverridesResponse {
  overrides: Record<string, ModelOverride>;
}
```

- [ ] **Step 2: Add config API functions to client.ts**

Add a `putJSON` helper (similar to existing `postJSON`), then add config functions. Append to `web/src/api/client.ts`:

```typescript
async function putJSON<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(path, {
    method: "PUT",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });

  const payload = (await response.json().catch(() => ({}))) as { error?: string };
  if (!response.ok) {
    throw new Error(payload.error || `Request failed: ${response.status}`);
  }
  return payload as T;
}

// --- Config API ---

export async function getConfigRoles(): Promise<ConfigRolesResponse> {
  return fetchJSON<ConfigRolesResponse>("/api/config/roles");
}

export async function getConfigOverrides(): Promise<ConfigOverridesResponse> {
  return fetchJSON<ConfigOverridesResponse>("/api/config/overrides");
}

export async function putConfigOverrides(
  overrides: Record<string, ModelOverride>,
): Promise<ConfigOverridesResponse> {
  return putJSON<ConfigOverridesResponse>("/api/config/overrides", { overrides });
}
```

Add `ConfigRolesResponse`, `ConfigOverridesResponse`, and `ModelOverride` to the import block at the top of `client.ts`.

- [ ] **Step 3: Verify frontend build**

Run: `cd /home/gernsback/source/agent-conductor/web && npm run build`
Expected: PASS (no type errors).

- [ ] **Step 4: Commit**

```bash
git add web/src/types/api.ts web/src/api/client.ts
git commit -m "feat(spec06): add config TypeScript types and API client functions"
```

---

## Task 8: useConfig Hook

**Files:**
- Create: `web/src/hooks/useConfig.ts`

- [ ] **Step 1: Create useConfig hook**

Create `web/src/hooks/useConfig.ts`:

```typescript
import { useCallback, useEffect, useRef, useState } from "react";
import type {
  ConfigRolesResponse,
  ConfigOverridesResponse,
  ModelOverride,
  ProjectInfo,
  ProviderModels,
  RoleConfig,
} from "../types/api";
import {
  getConfigRoles,
  getConfigOverrides,
  putConfigOverrides,
} from "../api/client";

const EMPTY_PROJECT: ProjectInfo = { name: "", path: "", data_dir: "" };

export function useConfig() {
  const [roles, setRoles] = useState<RoleConfig[]>([]);
  const [availableModels, setAvailableModels] = useState<ProviderModels[]>([]);
  const [project, setProject] = useState<ProjectInfo>(EMPTY_PROJECT);
  const [overrides, setOverrides] = useState<Record<string, ModelOverride>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchAll = useCallback(async () => {
    try {
      const [rolesResp, overridesResp] = await Promise.all([
        getConfigRoles(),
        getConfigOverrides(),
      ]);
      if (mountedRef.current) {
        setRoles(rolesResp.roles ?? []);
        setAvailableModels(rolesResp.available_models ?? []);
        setProject(rolesResp.project);
        setOverrides(overridesResp.overrides ?? {});
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : "Failed to fetch config");
      }
    }
  }, []);

  const refresh = useCallback(() => {
    fetchAll();
  }, [fetchAll]);

  useEffect(() => {
    mountedRef.current = true;
    fetchAll().finally(() => {
      if (mountedRef.current) setLoading(false);
    });
    return () => {
      mountedRef.current = false;
    };
  }, [fetchAll]);

  const setOverride = useCallback(
    async (role: string, provider: string, model: string) => {
      try {
        const next = { ...overrides, [role]: { provider, model } };
        const resp = await putConfigOverrides(next);
        if (mountedRef.current) {
          setOverrides(resp.overrides ?? {});
          setError(null);
        }
      } catch (err) {
        if (mountedRef.current) {
          setError(err instanceof Error ? err.message : "Failed to set override");
        }
      }
    },
    [overrides],
  );

  const clearOverride = useCallback(
    async (role: string) => {
      try {
        const next = { ...overrides };
        delete next[role];
        const resp = await putConfigOverrides(next);
        if (mountedRef.current) {
          setOverrides(resp.overrides ?? {});
          setError(null);
        }
      } catch (err) {
        if (mountedRef.current) {
          setError(
            err instanceof Error ? err.message : "Failed to clear override",
          );
        }
      }
    },
    [overrides],
  );

  const clearAllOverrides = useCallback(async () => {
    try {
      const resp = await putConfigOverrides({});
      if (mountedRef.current) {
        setOverrides(resp.overrides ?? {});
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(
          err instanceof Error ? err.message : "Failed to clear overrides",
        );
      }
    }
  }, []);

  return {
    roles,
    availableModels,
    project,
    overrides,
    loading,
    error,
    setOverride,
    clearOverride,
    clearAllOverrides,
    refresh,
  };
}
```

- [ ] **Step 2: Verify frontend build**

Run: `cd /home/gernsback/source/agent-conductor/web && npm run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/hooks/useConfig.ts
git commit -m "feat(spec06): add useConfig hook with override actions"
```

---

## Task 9: ConfigPanel Component

**Files:**
- Create: `web/src/components/ConfigPanel.tsx`

- [ ] **Step 1: Create ConfigPanel with RoleOverrideDropdown**

Create `web/src/components/ConfigPanel.tsx`:

```tsx
import { useConfig } from "@/hooks/useConfig";
import type { ModelOverride, ProviderModels, RoleConfig } from "@/types/api";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

export function formatRoleName(name: string): string {
  return name
    .split("_")
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

interface RoleOverrideDropdownProps {
  role: RoleConfig;
  availableModels: ProviderModels[];
  activeOverride?: ModelOverride;
  onChange: (provider: string, model: string) => void;
  onClear: () => void;
  label?: string;
}

export function RoleOverrideDropdown({
  role,
  availableModels,
  activeOverride,
  onChange,
  onClear,
  label,
}: RoleOverrideDropdownProps) {
  const currentValue = activeOverride
    ? `${activeOverride.provider}::${activeOverride.model}`
    : "";

  return (
    <div className="space-y-1">
      <div className="flex items-center gap-1.5">
        {activeOverride && (
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-blue-500" />
        )}
        <label className="text-xs font-medium text-muted-foreground">
          {label ?? formatRoleName(role.name)}
        </label>
      </div>
      <select
        className="w-full rounded-md border bg-background px-2 py-1 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
        value={currentValue}
        onChange={(e) => {
          const val = e.target.value;
          if (val === "") {
            onClear();
          } else {
            const [provider, model] = val.split("::");
            onChange(provider, model);
          }
        }}
      >
        <option value="">
          Default ({role.current_model})
        </option>
        {availableModels.map((pm) => (
          <optgroup key={pm.provider} label={pm.provider}>
            {pm.models.map((model) => (
              <option key={`${pm.provider}::${model}`} value={`${pm.provider}::${model}`}>
                {model}
              </option>
            ))}
          </optgroup>
        ))}
      </select>
    </div>
  );
}

export function ConfigPanel() {
  const config = useConfig();

  if (config.loading) {
    return (
      <div className="space-y-2">
        <p className="text-xs font-medium uppercase text-muted-foreground">Config</p>
        <p className="text-sm text-muted-foreground">Loading...</p>
      </div>
    );
  }

  if (config.error && !config.project.name) {
    return (
      <div className="space-y-2">
        <p className="text-xs font-medium uppercase text-muted-foreground">Config</p>
        <p className="text-sm text-muted-foreground">No project loaded</p>
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <span className="inline-block h-2 w-2 rounded-full bg-red-500" />
          <span>Disconnected</span>
        </div>
      </div>
    );
  }

  const hasOverrides = Object.keys(config.overrides).length > 0;

  return (
    <div className="space-y-3">
      {/* Project info */}
      <div>
        <p className="text-xs font-medium uppercase text-muted-foreground">Config</p>
        <p className="mt-1 text-sm font-medium">{config.project.name}</p>
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <p className="truncate text-xs text-muted-foreground">{config.project.path}</p>
            </TooltipTrigger>
            <TooltipContent side="top">
              <p className="text-xs">{config.project.path}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>

      {/* Connection status */}
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <span className="inline-block h-2 w-2 rounded-full bg-green-500" />
        <span>Connected</span>
      </div>

      {/* Role overrides */}
      {config.roles.length > 0 && (
        <div className="space-y-2">
          {config.roles.map((role) => (
            <RoleOverrideDropdown
              key={role.name}
              role={role}
              availableModels={config.availableModels}
              activeOverride={config.overrides[role.name]}
              onChange={(provider, model) =>
                config.setOverride(role.name, provider, model)
              }
              onClear={() => config.clearOverride(role.name)}
            />
          ))}

          {/* Reset All */}
          {hasOverrides && (
            <button
              className="text-xs text-muted-foreground underline hover:text-foreground"
              onClick={config.clearAllOverrides}
            >
              Reset All
            </button>
          )}
        </div>
      )}

      {/* Error display */}
      {config.error && (
        <p className="text-xs text-red-400">{config.error}</p>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify frontend build**

Run: `cd /home/gernsback/source/agent-conductor/web && npm run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/ConfigPanel.tsx
git commit -m "feat(spec06): add ConfigPanel with role override dropdowns"
```

---

## Task 10: Sidebar Integration

**Files:**
- Modify: `web/src/components/Sidebar.tsx`

- [ ] **Step 1: Replace config placeholder with ConfigPanel**

In `web/src/components/Sidebar.tsx`:

1. Add import: `import { ConfigPanel } from "@/components/ConfigPanel";`
2. Replace the entire "Config panel — pushed to bottom" block (lines 53-65 approximately):

From:
```tsx
      {/* Config panel — pushed to bottom */}
      <div className="mt-auto">
        <Separator className="mb-4" />
        <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">
          Config
        </p>
        <p className="mb-3 text-sm text-muted-foreground">
          No project loaded
        </p>
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <span className="inline-block h-2 w-2 rounded-full bg-zinc-500" />
          <span>Disconnected</span>
        </div>
      </div>
```

To:
```tsx
      {/* Config panel — pushed to bottom */}
      <div className="mt-auto">
        <Separator className="mb-4" />
        <ConfigPanel />
      </div>
```

- [ ] **Step 2: Verify frontend build**

Run: `cd /home/gernsback/source/agent-conductor/web && npm run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Sidebar.tsx
git commit -m "feat(spec06): replace sidebar config placeholder with ConfigPanel"
```

---

## Task 11: Queue Drawer Per-Item Overrides

**Files:**
- Modify: `web/src/components/QueueDrawer.tsx`

- [ ] **Step 1: Add per-item override dropdowns to PendingItemRow**

In `web/src/components/QueueDrawer.tsx`:

1. Add imports:
```tsx
import { useConfig } from "@/hooks/useConfig";
import { RoleOverrideDropdown } from "@/components/ConfigPanel";
```

2. Use the `useConfig` hook inside `QueueDrawer`:
```tsx
const config = useConfig();
```

3. Pass `config` data to `PendingItemRow` as new props: `roles`, `availableModels`, `sessionOverrides`.

4. Replace the current `PendingItemRow` expanded section (lines 358-367, the read-only override display) with interactive `RoleOverrideDropdown` components. Always show the override toggle button (not just when hasOverrides), and when expanded show dropdowns for each role.

5. When a dropdown changes, update the queue item's overrides. Since queue items use `Record<string, string>` for overrides (flat key-value), store as `"roleName"` → `"provider::model"` format, or update the queue item via the queue API.

The existing `item.overrides` uses `Record<string, string>`, so per-item overrides will be stored as `role → "provider::model"`. The dropdown `onChange` should call `queue.removeItem` and re-add (or ideally a future PATCH — for now, use the existing overrides display as read-only with a note that per-item editing requires an API enhancement).

**Practical approach for v1:** Show override dropdowns that display the effective override chain (item > session > default) but note that the existing queue API's `addQueueItems` sets overrides at add time. For pending items, we display the current overrides with the session/default fallback context using `RoleOverrideDropdown` in read-only-display mode.

Actually, looking at the queue API — `QueueItem.Overrides` is `map[string]string`. The simplest v1 approach: expand the override section to always be toggleable (not just when overrides exist), and show the `RoleOverrideDropdown` components pre-populated with the item's overrides falling back to session overrides falling back to defaults. Since there's no PATCH endpoint for queue items yet, the dropdowns will be read-only for now — per the spec, this is acceptable for v1 since the overrides are set when items are added to the queue.

**Revised approach:** Make the pending item expansion always available. Show the override dropdowns read-only-style, showing the effective model for each role (item override > session override > project default). This satisfies the spec's "pre-populated from" requirement without needing a new PATCH endpoint.

In `PendingItemRow`, replace the current toggle logic:

```tsx
// Change the toggle button to always show (remove hasOverrides guard)
<Button
  size="xs"
  variant="ghost"
  onClick={onToggleOverrides}
  className="text-xs text-muted-foreground"
>
  {isExpanded ? (
    <ChevronDown className="h-3 w-3" />
  ) : (
    <ChevronRight className="h-3 w-3" />
  )}
  Overrides
</Button>
```

And replace the expanded content:

```tsx
{isExpanded && (
  <div className="mt-2 space-y-2 rounded bg-muted/50 p-2">
    <p className="text-xs font-medium text-muted-foreground">Override for this run</p>
    {roles.map((role) => {
      // Determine effective override: item > session > default
      const itemOverrideRaw = item.overrides?.[role.name];
      let activeOverride: ModelOverride | undefined;
      if (itemOverrideRaw) {
        const [provider, model] = itemOverrideRaw.split("::");
        activeOverride = { provider, model };
      } else if (sessionOverrides[role.name]) {
        activeOverride = sessionOverrides[role.name];
      }

      return (
        <RoleOverrideDropdown
          key={role.name}
          role={role}
          availableModels={availableModels}
          activeOverride={activeOverride}
          onChange={() => {}}
          onClear={() => {}}
          label={formatRoleName(role.name)}
        />
      );
    })}
  </div>
)}
```

Note: The dropdowns are wired but non-functional for writes in v1 since there's no queue item PATCH endpoint. The spec acknowledges this is acceptable — the per-item overrides set at queue-add time are displayed correctly.

- [ ] **Step 2: Verify frontend build**

Run: `cd /home/gernsback/source/agent-conductor/web && npm run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/QueueDrawer.tsx
git commit -m "feat(spec06): add per-item override display in queue drawer"
```

---

## Task 12: Final Build Verification

- [ ] **Step 1: Run Go build**

Run: `make build`
Expected: PASS.

- [ ] **Step 2: Run Go tests**

Run: `make test`
Expected: all packages pass, 0 failures.

- [ ] **Step 3: Run frontend build**

Run: `cd /home/gernsback/source/agent-conductor/web && npm run build`
Expected: PASS.

- [ ] **Step 4: Final commit (if any remaining unstaged changes)**

Only if there are loose changes from fixups.
