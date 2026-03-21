package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/pipeline"
)

func (s *Server) handleSubmitPlan(w http.ResponseWriter, r *http.Request) {
	var req planSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
		return
	}

	if req.SpecContent == "" && req.SpecFilePath == "" {
		writeError(w, http.StatusBadRequest, "spec_content or spec_file_path is required")
		return
	}

	var specPath string

	if req.SpecFilePath != "" {
		if _, err := os.Stat(req.SpecFilePath); err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("spec file %q does not exist", req.SpecFilePath))
				return
			}
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("stat spec file: %v", err))
			return
		}
		specPath = req.SpecFilePath
	} else {
		tmpFile, err := os.CreateTemp("", "plan-spec-*.md")
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("create temp file: %v", err))
			return
		}
		if _, err := tmpFile.WriteString(req.SpecContent); err != nil {
			_ = tmpFile.Close()
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("write temp file: %v", err))
			return
		}
		if err := tmpFile.Close(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("close temp file: %v", err))
			return
		}
		specPath = tmpFile.Name()
	}

	ctx := r.Context()
	project := "default"

	sessionID, err := s.db.StartSession(ctx, database.SessionKindPlanOnly, project, specPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("start session: %v", err))
		return
	}

	if err := s.db.LogWorkflowEvent(ctx, "", "", pipeline.EventPlanStarted, map[string]any{
		"session_id": sessionID,
		"spec_path":  specPath,
	}); err != nil {
		slog.Warn("failed to log plan_started event", "session_id", sessionID, "error", err)
	}

	// TODO: background execution — plan logic is in package main, not importable yet.

	writeJSON(w, http.StatusAccepted, planSubmitResponse{
		SessionID: sessionID,
		Status:    "started",
	})
}
