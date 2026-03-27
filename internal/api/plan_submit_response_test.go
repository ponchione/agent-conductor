package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/agent-conductor/internal/planner"
)

func TestHandleSubmitPlan_ReturnsRealSessionID(t *testing.T) {
	db := newTestDB(t)

	claudeDir := t.TempDir()
	claudePath := filepath.Join(claudeDir, "claude")
	if err := os.WriteFile(claudePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", claudePath, err)
	}
	t.Setenv("PATH", claudeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	done := make(chan struct{})
	s := &Server{
		db:            db,
		workOrderDir:  t.TempDir(),
		cfg:           testConfig(),
		overrideStore: NewOverrideStore(),
		generateFunc: func(ctx context.Context, input planner.GenerateInput) (*planner.GenerateResult, error) {
			defer close(done)
			return &planner.GenerateResult{}, nil
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/plan", strings.NewReader(`{"spec_content":"# Sample spec\n\nBody"}`))

	s.handleSubmitPlan(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var payload planSubmitResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.PlanRunID == "" {
		t.Fatal("PlanRunID should not be empty")
	}
	if payload.SessionID == "" {
		t.Fatal("SessionID should not be empty")
	}
	if payload.SessionID == payload.PlanRunID {
		t.Fatalf("SessionID = %q, should differ from PlanRunID = %q", payload.SessionID, payload.PlanRunID)
	}

	session, err := db.GetSession(context.Background(), payload.SessionID)
	if err != nil {
		t.Fatalf("GetSession(%q) error: %v", payload.SessionID, err)
	}
	if session.ID != payload.SessionID {
		t.Fatalf("session.ID = %q, want %q", session.ID, payload.SessionID)
	}

	planRun, err := db.GetPlanRun(context.Background(), payload.PlanRunID)
	if err != nil {
		t.Fatalf("GetPlanRun(%q) error: %v", payload.PlanRunID, err)
	}
	if !planRun.SessionID.Valid {
		t.Fatal("plan run session_id should be set")
	}
	if planRun.SessionID.String != payload.SessionID {
		t.Fatalf("planRun.SessionID = %q, want %q", planRun.SessionID.String, payload.SessionID)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for plan generation")
	}

	time.Sleep(50 * time.Millisecond)
}
