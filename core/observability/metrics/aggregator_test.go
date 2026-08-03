package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func TestAggregate_Last24h(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t1", TenantID: "default", StartTime: now.Add(-2 * time.Hour),
		EndTime: now.Add(-2*time.Hour + time.Second),
		Status:  model.SpanStatusSuccess, TotalTokens: 100, TotalCost: 0.01, DurationMs: 1000,
	})
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t2", TenantID: "default", StartTime: now.Add(-1 * time.Hour),
		EndTime: now.Add(-1*time.Hour + time.Second),
		Status:  model.SpanStatusError, TotalTokens: 50, TotalCost: 0.02, DurationMs: 2000,
	})
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t-old", TenantID: "default", StartTime: now.Add(-48 * time.Hour),
		Status: model.SpanStatusSuccess, TotalTokens: 999, DurationMs: 1,
	})

	_ = store.SaveSpan(ctx, &model.Span{
		SpanID: "s1", TraceID: "t1", SpanType: model.SpanTypeTool, ToolName: "get_weather",
		StartTime: now.Add(-2 * time.Hour), Status: model.SpanStatusSuccess,
	})
	_ = store.SaveSpan(ctx, &model.Span{
		SpanID: "s2", TraceID: "t1", SpanType: model.SpanTypeTool, ToolName: "get_weather",
		StartTime: now.Add(-2 * time.Hour), Status: model.SpanStatusSuccess,
	})
	_ = store.SaveSpan(ctx, &model.Span{
		SpanID: "s3", TraceID: "t2", SpanType: model.SpanTypeTool, ToolName: "search",
		StartTime: now.Add(-1 * time.Hour), Status: model.SpanStatusSuccess,
	})

	agg := NewAggregator(store)
	snap, err := agg.Aggregate(ctx, "default", ScopeLast24h, now)
	if err != nil {
		t.Fatal(err)
	}
	if snap.TotalTraces != 2 {
		t.Fatalf("traces=%d", snap.TotalTraces)
	}
	if snap.TotalTokens != 150 {
		t.Fatalf("tokens=%d", snap.TotalTokens)
	}
	if snap.SuccessRate != 0.5 {
		t.Fatalf("success_rate=%v", snap.SuccessRate)
	}
	if snap.AvgDurationMs != 1500 {
		t.Fatalf("avg=%v", snap.AvgDurationMs)
	}
	if len(snap.TopTools) < 1 || snap.TopTools[0].Name != "get_weather" || snap.TopTools[0].Count != 2 {
		t.Fatalf("top_tools=%+v", snap.TopTools)
	}
	if snap.RefreshedAt.IsZero() {
		t.Fatal("refreshed_at should be set")
	}
}

func TestAggregate_InvalidScope(t *testing.T) {
	agg := NewAggregator(storage.NewMemoryStorage())
	_, err := agg.Aggregate(context.Background(), "default", "yesterday", time.Now())
	if err == nil {
		t.Fatal("want error")
	}
}

func TestRefresh_PersistsAllScopes(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t1", TenantID: "default", StartTime: now.Add(-time.Hour),
		Status: model.SpanStatusSuccess, DurationMs: 10,
	})

	agg := NewAggregator(store)
	if err := agg.Refresh(ctx, "default", now); err != nil {
		t.Fatal(err)
	}
	for _, scope := range SupportedScopes {
		got, err := store.GetMetricSnapshot(ctx, "default", scope)
		if err != nil {
			t.Fatalf("scope %s: %v", scope, err)
		}
		if got.Scope != scope {
			t.Fatalf("scope=%q want %q", got.Scope, scope)
		}
	}
	if len(store.MetricSnapshots) != 3 {
		t.Fatalf("snapshots=%d want 3", len(store.MetricSnapshots))
	}
}

func TestAggregate_ReadsPersistedSnapshot(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()

	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t1", TenantID: "default", StartTime: now.Add(-time.Hour),
		Status: model.SpanStatusSuccess, DurationMs: 10,
	})

	agg := NewAggregator(store)
	if err := agg.Refresh(ctx, "default", now); err != nil {
		t.Fatal(err)
	}

	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t2", TenantID: "default", StartTime: now.Add(-30 * time.Minute),
		Status: model.SpanStatusSuccess, DurationMs: 10,
	})

	s1, err := agg.Aggregate(ctx, "default", ScopeLast24h, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if s1.TotalTraces != 1 {
		t.Fatalf("stale snapshot want 1, got %d", s1.TotalTraces)
	}

	if err := agg.Refresh(ctx, "default", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	s2, err := agg.Aggregate(ctx, "default", ScopeLast24h, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if s2.TotalTraces != 2 {
		t.Fatalf("after refresh want 2, got %d", s2.TotalTraces)
	}
}

func TestRefresh_CorrectsAfterPurge(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()

	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t1", TenantID: "default", StartTime: now.Add(-time.Hour),
		Status: model.SpanStatusSuccess, DurationMs: 10,
	})
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t2", TenantID: "default", StartTime: now.Add(-30 * time.Minute),
		Status: model.SpanStatusSuccess, DurationMs: 10,
	})

	agg := NewAggregator(store)
	if err := agg.Refresh(ctx, "default", now); err != nil {
		t.Fatal(err)
	}
	if err := store.PurgeTrace(ctx, "default", "t1"); err != nil {
		t.Fatal(err)
	}
	if err := agg.Refresh(ctx, "default", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	snap, err := agg.Aggregate(ctx, "default", ScopeLast24h, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if snap.TotalTraces != 1 {
		t.Fatalf("after purge+refresh want 1, got %d", snap.TotalTraces)
	}
}

func TestSnapshot_TenantIsolation(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()

	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "ta", TenantID: "a", StartTime: now.Add(-time.Hour),
		Status: model.SpanStatusSuccess, DurationMs: 10,
	})
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "tb", TenantID: "b", StartTime: now.Add(-time.Hour),
		Status: model.SpanStatusSuccess, DurationMs: 10, TotalTokens: 99,
	})

	agg := NewAggregator(store)
	if err := agg.Refresh(ctx, "a", now); err != nil {
		t.Fatal(err)
	}
	sa, err := agg.Aggregate(ctx, "a", ScopeLast24h, now)
	if err != nil {
		t.Fatal(err)
	}
	if sa.TotalTraces != 1 {
		t.Fatalf("tenant a traces=%d", sa.TotalTraces)
	}
	_, err = store.GetMetricSnapshot(ctx, "b", ScopeLast24h)
	if !errors.Is(err, storage.ErrorNotFound) {
		t.Fatalf("tenant b should have no snapshot yet, err=%v", err)
	}
}

func TestAggregate_ColdStart(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t1", TenantID: "default", StartTime: now.Add(-time.Hour),
		Status: model.SpanStatusSuccess, DurationMs: 10,
	})

	agg := NewAggregator(store)
	snap, err := agg.Aggregate(ctx, "default", ScopeLast24h, now)
	if err != nil {
		t.Fatal(err)
	}
	if snap.TotalTraces != 1 {
		t.Fatalf("cold start traces=%d", snap.TotalTraces)
	}
	saved, err := store.GetMetricSnapshot(ctx, "default", ScopeLast24h)
	if err != nil {
		t.Fatal(err)
	}
	if saved.TotalTraces != 1 {
		t.Fatalf("persisted=%d", saved.TotalTraces)
	}
}
