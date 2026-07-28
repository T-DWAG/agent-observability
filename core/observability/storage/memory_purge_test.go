package storage

import (
	"context"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/model"
)

func TestMemoryPurgeTrace(t *testing.T) {
	store := NewMemoryStorage()
	ctx := context.Background()
	tr := &model.Trace{
		TraceID:   "t1",
		TenantID:  "acme",
		StartTime: time.Now(),
		Status:    model.SpanStatusSuccess,
	}
	if err := store.SaveTrace(ctx, tr); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSpan(ctx, &model.Span{SpanID: "s1", TraceID: "t1"}); err != nil {
		t.Fatal(err)
	}
	// 别的租户同名 TraceID 不应被误删
	if err := store.SaveTrace(ctx, &model.Trace{
		TraceID: "t1", TenantID: "other", StartTime: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.PurgeTrace(ctx, "acme", "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTrace(ctx, "acme", "t1"); err != ErrorNotFound {
		t.Fatalf("acme t1 should be gone, err=%v", err)
	}
	if _, err := store.GetTrace(ctx, "other", "t1"); err != nil {
		t.Fatalf("other tenant must remain: %v", err)
	}
	if len(store.Spans) != 0 {
		t.Fatalf("spans should be purged, got %d", len(store.Spans))
	}
}

func TestMemoryPurgeBefore(t *testing.T) {
	store := NewMemoryStorage()
	ctx := context.Background()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	neu := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	_ = store.SaveTrace(ctx, &model.Trace{TraceID: "old", TenantID: "default", StartTime: old})
	_ = store.SaveSpan(ctx, &model.Span{SpanID: "so", TraceID: "old"})
	_ = store.SaveTrace(ctx, &model.Trace{TraceID: "new", TenantID: "default", StartTime: neu})
	_ = store.SaveSpan(ctx, &model.Span{SpanID: "sn", TraceID: "new"})

	before := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	n, err := store.PurgeBefore(ctx, "default", before)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged=%d want 1", n)
	}
	if _, err := store.GetTrace(ctx, "default", "old"); err != ErrorNotFound {
		t.Fatal("old should be gone")
	}
	if _, err := store.GetTrace(ctx, "default", "new"); err != nil {
		t.Fatalf("new must remain: %v", err)
	}
	if len(store.Spans) != 1 || store.Spans[0].SpanID != "sn" {
		t.Fatalf("want only new span, got %+v", store.Spans)
	}
}
