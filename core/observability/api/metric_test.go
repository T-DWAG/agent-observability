package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/metrics"
	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func TestGetMetrics(t *testing.T) {
	store := storage.NewMemoryStorage()
	now := time.Now().UTC()
	_ = store.SaveTrace(context.Background(), &model.Trace{
		TraceID: "tm", TenantID: "default", StartTime: now.Add(-time.Hour),
		Status: model.SpanStatusSuccess, TotalTokens: 10, DurationMs: 100,
	})

	srv := NewServer(store).WithAggregator(metrics.NewAggregator(store, time.Minute))
	req := withTestAuth(httptest.NewRequest(http.MethodGet, "/api/v1/metrics?scope=last_24h", nil))
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body metrics.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TotalTraces != 1 {
		t.Fatalf("traces=%d", body.TotalTraces)
	}
}

func TestGetMetrics_BadScope(t *testing.T) {
	store := storage.NewMemoryStorage()
	srv := NewServer(store).WithAggregator(metrics.NewAggregator(store, 0))
	req := withTestAuth(httptest.NewRequest(http.MethodGet, "/api/v1/metrics?scope=nope", nil))
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}
