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
