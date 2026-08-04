package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	otelsink "github.com/T-Dwag/agent-observability/exporter/otel"
	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

type stubOTLPClient struct {
	err error
}

func (c *stubOTLPClient) Start(context.Context) error { return nil }
func (c *stubOTLPClient) Stop(context.Context) error  { return nil }
func (c *stubOTLPClient) UploadTraces(context.Context, []*tracepb.ResourceSpans) error {
	return c.err
}

var _ otlptrace.Client = (*stubOTLPClient)(nil)

func TestExportTrace_NotConfigured(t *testing.T) {
	store := storage.NewMemoryStorage()
	srv := NewServer(store)
	req := withTestAuth(httptest.NewRequest(http.MethodPost, "/api/v1/traces/t1/export", nil))
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503", rec.Code)
	}
}

func TestExportTrace_NotFound(t *testing.T) {
	store := storage.NewMemoryStorage()
	exporter := otelsink.New(store, &stubOTLPClient{}, otelsink.Config{Timeout: time.Second})
	srv := NewServer(store).WithOTelExporter(exporter)

	req := withTestAuth(httptest.NewRequest(http.MethodPost, "/api/v1/traces/missing/export", nil))
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404 body=%s", rec.Code, rec.Body.String())
	}
}

func TestExportTrace_BadGateway(t *testing.T) {
	store := storage.NewMemoryStorage()
	now := time.Now().UTC()
	_ = store.SaveTrace(context.Background(), &model.Trace{
		TraceID: "t-export", TenantID: "default", StartTime: now,
		Status: model.SpanStatusSuccess,
	})
	_ = store.SaveSpan(context.Background(), &model.Span{
		SpanID: "s1", TraceID: "t-export", SpanType: model.SpanTypeAgent,
		StartTime: now, EndTime: now, Status: model.SpanStatusSuccess,
	})

	exporter := otelsink.New(store, &stubOTLPClient{err: errors.New("collector down")}, otelsink.Config{Timeout: time.Second})
	srv := NewServer(store).WithOTelExporter(exporter)
	req := withTestAuth(httptest.NewRequest(http.MethodPost, "/api/v1/traces/t-export/export", nil))
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d want 502 body=%s", rec.Code, rec.Body.String())
	}
}

func TestExportTrace_OK(t *testing.T) {
	store := storage.NewMemoryStorage()
	now := time.Now().UTC()
	_ = store.SaveTrace(context.Background(), &model.Trace{
		TraceID: "t-ok", TenantID: "default", StartTime: now,
		Status: model.SpanStatusSuccess,
	})
	_ = store.SaveSpan(context.Background(), &model.Span{
		SpanID: "s1", TraceID: "t-ok", SpanType: model.SpanTypeAgent,
		StartTime: now, EndTime: now, Status: model.SpanStatusSuccess,
	})

	exporter := otelsink.New(store, &stubOTLPClient{}, otelsink.Config{Timeout: time.Second})
	srv := NewServer(store).WithOTelExporter(exporter)
	req := withTestAuth(httptest.NewRequest(http.MethodPost, "/api/v1/traces/t-ok/export", nil))
	rec := httptest.NewRecorder()
	srv.Handler(testAPIKeys()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "exported" || body["trace_id"] != "t-ok" {
		t.Fatalf("body=%v", body)
	}
}
