package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetStatsSummary_Empty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/summary", nil)
	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp statsSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Range != "30d" {
		t.Fatalf("range = %q, want 30d", resp.Range)
	}
	if resp.Pipeline.TotalRuns != 0 {
		t.Fatalf("pipeline.total_runs = %d, want 0", resp.Pipeline.TotalRuns)
	}
	if resp.Plan.TotalRuns != 0 {
		t.Fatalf("plan.total_runs = %d, want 0", resp.Plan.TotalRuns)
	}
}

func TestGetStatsSummary_WithRange(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/summary?range=7d", nil)
	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp statsSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Range != "7d" {
		t.Fatalf("range = %q, want 7d", resp.Range)
	}
}

func TestGetStatsTrends_Empty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/trends", nil)
	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp statsTrendsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.PipelineRuns) != 0 {
		t.Fatalf("pipeline_runs len = %d, want 0", len(resp.PipelineRuns))
	}
	if len(resp.PlanRuns) != 0 {
		t.Fatalf("plan_runs len = %d, want 0", len(resp.PlanRuns))
	}
}

func TestGetStatsModels_Empty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/models", nil)
	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp statsModelsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.Models) != 0 {
		t.Fatalf("models len = %d, want 0", len(resp.Models))
	}
}
