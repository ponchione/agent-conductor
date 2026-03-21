package api

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/git"
)

const (
	defaultLimit            = 20
	eventStreamBatchLimit   = 100
	eventStreamPollInterval = 1 * time.Second
)

//go:embed static/*
var staticFiles embed.FS

// Server exposes read-only observability endpoints backed by the conductor DB.
type Server struct {
	db            *database.DB
	gitMgr        *git.GitManager
	baseBranch    string
	runQueue      *RunQueue
	workOrderDir  string
	cfg           *config.ProjectConfig
	overrideStore *OverrideStore
}

// NewServer builds the HTTP handler tree for observability reads.
func NewServer(db *database.DB, gitMgr *git.GitManager, baseBranch string, runQueue *RunQueue, workOrderDir string, cfg *config.ProjectConfig) http.Handler {
	s := &Server{
		db: db, gitMgr: gitMgr, baseBranch: baseBranch,
		runQueue: runQueue, workOrderDir: workOrderDir,
		cfg: cfg, overrideStore: NewOverrideStore(),
	}
	r := chi.NewRouter()
	r.Mount("/assets/", s.staticAssetsHandler())
	r.Get("/api/sessions", s.handleListSessions)
	r.Get("/api/sessions/{id}", s.handleGetSession)
	r.Get("/api/stats/plan-audit", s.handleGetPlanAuditStats)
	r.Get("/api/events/stream", s.handleEventStream)
	r.Get("/api/workflows", s.handleListWorkflows)
	r.Get("/api/workflows/{id}", s.handleGetWorkflow)
	r.Get("/api/workflows/{id}/diff", s.handleGetWorkflowDiff)
	r.Get("/api/workflows/{id}/scope", s.handleGetWorkflowScope)
	r.Post("/api/workflows/{id}/approve", s.handleApproveWorkflow)
	r.Post("/api/workflows/{id}/reject", s.handleRejectWorkflow)
	r.Get("/api/queue", s.handleGetQueue)
	r.Post("/api/queue", s.handleAddQueueItems)
	r.Delete("/api/queue/{id}", s.handleRemoveQueueItem)
	r.Post("/api/queue/reorder", s.handleReorderQueue)
	r.Post("/api/queue/start", s.handleStartQueue)
	r.Post("/api/queue/pause", s.handlePauseQueue)
	r.Post("/api/queue/continue", s.handleContinueQueue)
	r.Get("/api/queue/events", s.handleQueueEvents)
	r.Get("/api/work-orders", s.handleListWorkOrders)
	r.Get("/api/work-orders/{filename}", s.handleGetWorkOrder)
	r.Put("/api/work-orders/{filename}", s.handleUpdateWorkOrder)
	r.Post("/api/plan", s.handleSubmitPlan)
	r.Get("/api/config/roles", s.handleGetConfigRoles)
	r.Get("/api/config/overrides", s.handleGetConfigOverrides)
	r.Put("/api/config/overrides", s.handlePutConfigOverrides)
	r.Get("/*", s.handleSPAFallback)
	return r
}

func (s *Server) handleSPAFallback(w http.ResponseWriter, r *http.Request) {
	content, err := fs.ReadFile(staticFiles, "static/app/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("load observability UI: %v (run make web-build to refresh embedded assets)", err))
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

func (s *Server) handleEventStream(w http.ResponseWriter, r *http.Request) {
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id"))
	if workflowID == "" {
		writeError(w, http.StatusBadRequest, "workflow_id is required")
		return
	}

	cursor, err := parseEventCursor(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	events, err := s.loadEventBatch(r.Context(), workflowID, cursor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list events: %v", err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	lastID := cursor
	if err := writeEventBatch(w, flusher, events, &lastID); err != nil {
		return
	}

	ticker := time.NewTicker(eventStreamPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			events, err := s.loadEventBatch(r.Context(), workflowID, lastID)
			if err != nil {
				if r.Context().Err() == nil {
					slog.Warn("event stream poll failed", "workflow_id", workflowID, "error", err)
				}
				return
			}
			if err := writeEventBatch(w, flusher, events, &lastID); err != nil {
				return
			}
		}
	}
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

func parseEventCursor(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if raw == "" {
		raw = strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	}
	if raw == "" {
		return 0, nil
	}

	cursor, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || cursor < 0 {
		return 0, fmt.Errorf("invalid event cursor %q", raw)
	}
	return cursor, nil
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

func (s *Server) loadEventBatch(ctx context.Context, workflowID string, afterID int64) ([]database.Event, error) {
	return s.db.ListEventsSince(ctx, database.ListEventsSinceParams{
		WorkflowID: sql.NullString{String: workflowID, Valid: true},
		ID:         afterID,
		Limit:      eventStreamBatchLimit,
	})
}

func writeEventBatch(w http.ResponseWriter, flusher http.Flusher, rows []database.Event, lastID *int64) error {
	for _, row := range rows {
		payload, err := json.Marshal(mapEventStreamRow(row))
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "id: %d\n", row.ID); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return err
		}
		flusher.Flush()
		*lastID = row.ID
	}
	return nil
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r, 50)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	offset, err := parseOffset(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	status := r.URL.Query().Get("status")
	project := r.URL.Query().Get("project")
	sessionID := r.URL.Query().Get("session_id")

	rows, total, err := s.db.ListWorkflowsForUI(r.Context(), status, project, sessionID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list workflows: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, workflowListResponse{
		Workflows: mapWorkflowSummaries(rows),
		Total:     total,
	})
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	wf, pr, subCalls, err := s.db.GetWorkflowDetailForUI(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get workflow: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, mapWorkflowDetail(wf, pr, subCalls))
}

func parseOffset(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("offset")
	if raw == "" {
		return 0, nil
	}

	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid offset %q", raw)
	}
	return offset, nil
}

func (s *Server) staticAssetsHandler() http.Handler {
	assetsFS, err := fs.Sub(staticFiles, "static/app/assets")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("load assets: %v", err))
		})
	}
	return http.StripPrefix("/assets/", http.FileServerFS(assetsFS))
}
