package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
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
	"decompose":         "Decomposes work order into analysis targets",
	"analyze":           "Analyzes individual files for scope context",
	"crosscut":          "Identifies cross-cutting concerns across targets",
	"synthesize":        "Synthesizes scope analysis into context package",
	"describe":          "Generates natural language descriptions",
	"build":             "Executes code generation",
	"verify_analyze":    "Analyzes build output for correctness",
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

	slices.SortFunc(roles, func(a, b roleConfigResponse) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	slices.SortFunc(availableModels, func(a, b providerModelsResponse) int {
		if a.Provider < b.Provider {
			return -1
		}
		if a.Provider > b.Provider {
			return 1
		}
		return 0
	})

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
