package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func seedStore(t *testing.T) storage.Storage {
	t.Helper()
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()

	tr := &model.Trace{
		TraceID:     "tr-001",
		TenantID:    "default",
		SessionID:   "sess-a",
		UserInput:   "北京天气？",
		AgentOutput: "25°C",
		StartTime:   now,
		EndTime:     now.Add(time.Second),
		Status:      model.SpanStatusSuccess,
		SpanCount:   2,
		TotalTokens: 100,
	}
	sp1 := &model.Span{
		SpanID: "sp-1", TraceID: "tr-001", SpanType: model.SpanTypeAgent,
		SpanName: "agent", StartTime: now, Status: model.SpanStatusSuccess,
	}
	sp2 := &model.Span{
		SpanID: "sp-2", TraceID: "tr-001", ParentSpanID: "sp-1",
		SpanType: model.SpanTypeLLM, SpanName: "llm", StartTime: now.Add(10 * time.Millisecond),
		Status: model.SpanStatusSuccess, TotalTokens: 100,
	}
	if err := store.SaveSpan(ctx, sp1); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSpan(ctx, sp2); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTrace(ctx, tr); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestHealthz(t *testing.T) {
	srv := NewServer(storage.NewMemoryStorage())
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestGetTrace_OK(t *testing.T) {
	srv := NewServer(seedStore(t))
	req := withTestAuth(httptest.NewRequest(http.MethodGet, "/api/v1/traces/tr-001", nil))
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body traceDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TraceID != "tr-001" || body.UserInput != "北京天气？" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestGetTrace_NotFound(t *testing.T) {
	srv := NewServer(seedStore(t))
	req := withTestAuth(httptest.NewRequest(http.MethodGet, "/api/v1/traces/no-such", nil))
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestListTraces_FilterSession(t *testing.T) {
	srv := NewServer(seedStore(t))
	req := withTestAuth(httptest.NewRequest(http.MethodGet, "/api/v1/traces?session_id=sess-a&page=1&size=10", nil))
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body listTracesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 1 || len(body.Items) != 1 {
		t.Fatalf("total=%d len=%d", body.Total, len(body.Items))
	}
}

func TestGetTraceSpans(t *testing.T) {
	srv := NewServer(seedStore(t))
	req := withTestAuth(httptest.NewRequest(http.MethodGet, "/api/v1/traces/tr-001/spans", nil))
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	total, _ := body["total"].(float64)
	if int(total) != 2 {
		t.Fatalf("total spans = %v", body["total"])
	}
}
