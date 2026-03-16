package api

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/ponchione/agent-conductor/internal/database"
)

const defaultLimit = 20

//go:embed static/*
var staticFiles embed.FS

// Server exposes read-only observability endpoints backed by the conductor DB.
type Server struct {
	db *database.DB
}

// NewServer builds the HTTP handler tree for observability reads.
func NewServer(db *database.DB) http.Handler {
	s := &Server{db: db}
	r := chi.NewRouter()
	r.Get("/", s.handleIndex)
	r.Get("/observability", s.handleObservabilityPage)
	r.Mount("/assets/", s.staticAssetsHandler())
	r.Get("/api/sessions", s.handleListSessions)
	r.Get("/api/sessions/{id}", s.handleGetSession)
	r.Get("/api/stats/plan-audit", s.handleGetPlanAuditStats)
	return r
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/observability", http.StatusTemporaryRedirect)
}

func (s *Server) handleObservabilityPage(w http.ResponseWriter, r *http.Request) {
	content, err := fs.ReadFile(staticFiles, "static/observability.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("load observability UI: %v", err))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(content); err != nil {
		http.Error(w, fmt.Sprintf("write observability UI: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r, defaultLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := s.db.ListSessions(r.Context(), r.URL.Query().Get("state"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list sessions: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, sessionListResponse{Sessions: mapSessionSummaries(rows)})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	detail, err := s.db.GetSessionDetail(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get session detail: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, mapSessionDetail(detail))
}

func (s *Server) handleGetPlanAuditStats(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r, defaultLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	stats, err := s.db.GetPlanAuditChangeStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get plan audit stats: %v", err))
		return
	}

	recentRuns, err := s.db.ListPlanRunUsefulness(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list plan audit runs: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, planAuditStatsResponse{
		Summary:    mapPlanAuditSummary(stats),
		RecentRuns: mapPlanRunUsefulnessRows(recentRuns),
	})
}

func parseLimit(r *http.Request, defaultValue int) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultValue, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0, fmt.Errorf("invalid limit %q", raw)
	}
	return limit, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) staticAssetsHandler() http.Handler {
	assetsFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("load assets: %v", err))
		})
	}
	return http.StripPrefix("/assets/", http.FileServerFS(assetsFS))
}
