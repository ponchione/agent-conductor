package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ponchione/agent-conductor/internal/gate"
)

func (s *Server) handleApproveWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.gitMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "git manager not configured")
		return
	}

	id := chi.URLParam(r, "id")

	var body approveRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
			return
		}
	}

	wf, err := s.db.GetWorkflow(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get workflow: %v", err))
		return
	}

	if wf.CurrentState != "human_review" {
		writeError(w, http.StatusConflict, fmt.Sprintf("workflow is in state %q, expected human_review", wf.CurrentState))
		return
	}

	if err := gate.Approve(r.Context(), s.db, s.gitMgr, id, wf.TargetRepo, s.baseBranch); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("approve workflow: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, approveResponse{
		Status:     "approved",
		WorkflowID: id,
		Merged:     true,
		Reindexed:  body.Reindex,
	})
}

func (s *Server) handleRejectWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body rejectRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
			return
		}
	}

	wf, err := s.db.GetWorkflow(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get workflow: %v", err))
		return
	}

	if wf.CurrentState != "human_review" {
		writeError(w, http.StatusConflict, fmt.Sprintf("workflow is in state %q, expected human_review", wf.CurrentState))
		return
	}

	if err := gate.Reject(r.Context(), s.db, id, body.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("reject workflow: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, rejectResponse{
		Status:     "rejected",
		WorkflowID: id,
	})
}
