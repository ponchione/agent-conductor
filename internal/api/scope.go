package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/ponchione/agent-conductor/internal/database"
)

func (s *Server) handleGetWorkflowScope(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Use GetWorkflow (sqlc-generated) to get full workflow including context_package_path.
	wf, err := s.db.GetWorkflow(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get workflow: %v", err))
		return
	}

	// Try reading from the context_package_path file first.
	if wf.ContextPackagePath.Valid && wf.ContextPackagePath.String != "" {
		data, err := os.ReadFile(wf.ContextPackagePath.String)
		if err == nil {
			// Validate it's valid JSON before returning.
			if json.Valid(data) {
				writeJSON(w, http.StatusOK, workflowScopeResponse{
					ContextPackage: json.RawMessage(data),
					Source:         "file",
				})
				return
			}
		}
	}

	// Fallback: look for a context_package artifact.
	artifact, err := s.db.GetLatestArtifactByType(r.Context(), database.ArtifactTypeContextPackage, "", id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no context package found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get artifact: %v", err))
		return
	}

	data, err := os.ReadFile(artifact.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, "context package file not readable")
		return
	}

	if !json.Valid(data) {
		writeError(w, http.StatusInternalServerError, "context package file is not valid JSON")
		return
	}

	writeJSON(w, http.StatusOK, workflowScopeResponse{
		ContextPackage: json.RawMessage(data),
		Source:         "artifact",
	})
}
