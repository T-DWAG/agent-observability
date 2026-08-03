package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/model"
)

func TestMemoryMetricSnapshot_Upsert(t *testing.T) {
	store := NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()

	first := &model.MetricSnapshot{
		TenantID: "default", Scope: "last_24h",
		WindowFrom: now.Add(-time.Hour), WindowTo: now,
		TotalTraces: 1, TopToolsJSON: "[]", RefreshedAt: now,
	}
	if err := store.SaveMetricSnapshot(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := &model.MetricSnapshot{
		TenantID: "default", Scope: "last_24h",
		WindowFrom: now.Add(-2 * time.Hour), WindowTo: now,
		TotalTraces: 9, TopToolsJSON: "[]", RefreshedAt: now.Add(time.Minute),
	}
	if err := store.SaveMetricSnapshot(ctx, second); err != nil {
		t.Fatal(err)
	}
	if len(store.MetricSnapshots) != 1 {
		t.Fatalf("len=%d want 1", len(store.MetricSnapshots))
	}
	got, err := store.GetMetricSnapshot(ctx, "default", "last_24h")
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalTraces != 9 {
		t.Fatalf("TotalTraces=%d want 9", got.TotalTraces)
	}
}

func TestMemoryMetricSnapshot_TenantIsolation(t *testing.T) {
	store := NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()

	_ = store.SaveMetricSnapshot(ctx, &model.MetricSnapshot{
		TenantID: "a", Scope: "last_24h", TotalTraces: 1,
		TopToolsJSON: "[]", RefreshedAt: now, WindowFrom: now, WindowTo: now,
	})
	_ = store.SaveMetricSnapshot(ctx, &model.MetricSnapshot{
		TenantID: "b", Scope: "last_24h", TotalTraces: 2,
		TopToolsJSON: "[]", RefreshedAt: now, WindowFrom: now, WindowTo: now,
	})

	a, err := store.GetMetricSnapshot(ctx, "a", "last_24h")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.GetMetricSnapshot(ctx, "b", "last_24h")
	if err != nil {
		t.Fatal(err)
	}
	if a.TotalTraces != 1 || b.TotalTraces != 2 {
		t.Fatalf("a=%d b=%d", a.TotalTraces, b.TotalTraces)
	}
}

func TestMemoryMetricSnapshot_NotFound(t *testing.T) {
	store := NewMemoryStorage()
	_, err := store.GetMetricSnapshot(context.Background(), "default", "last_24h")
	if !errors.Is(err, ErrorNotFound) {
		t.Fatalf("err=%v", err)
	}
}
