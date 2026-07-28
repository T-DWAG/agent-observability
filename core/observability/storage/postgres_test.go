package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/google/uuid"
)

func TestPostgresStorage_SaveAndGet(t *testing.T) {
	dsn := os.Getenv("OBS_PG_DSN")
	if dsn == "" {
		t.Skip("set OBS_PG_DSN to run postgres integration test")
	}

	db, err := OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	store := NewPostgresStorage(db)
	ctx := context.Background()
	traceID := uuid.New().String()
	now := time.Now()

	tr := &model.Trace{
		TraceID:   traceID,
		TenantID:  "default",
		SessionID: "s-pg-test",
		UserInput: "hello",
		StartTime: now,
		Status:    model.SpanStatusSuccess,
		SpanCount: 1,
	}
	sp := &model.Span{
		SpanID:    uuid.New().String(),
		TraceID:   traceID,
		SpanType:  model.SpanTypeAgent,
		SpanName:  "agent",
		StartTime: now,
		Status:    model.SpanStatusSuccess,
	}

	if err := store.SaveSpan(ctx, sp); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTrace(ctx, tr); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetTrace(ctx, "default", traceID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserInput != "hello" {
		t.Fatalf("user_input = %q", got.UserInput)
	}
	if got.SessionID != "s-pg-test" {
		t.Fatalf("session_id = %q", got.SessionID)
	}

	spans, err := store.GetTraceSpans(ctx, "default", traceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	if spans[0].SpanName != "agent" {
		t.Fatalf("span_name = %q", spans[0].SpanName)
	}

	// 幂等：同一 trace_id 再写一次应覆盖，不报错
	tr.AgentOutput = "world"
	if err := store.SaveTrace(ctx, tr); err != nil {
		t.Fatal(err)
	}
	got2, err := store.GetTrace(ctx, "default", traceID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.AgentOutput != "world" {
		t.Fatalf("agent_output after upsert = %q", got2.AgentOutput)
	}

	list, total, err := store.ListTraces(ctx, TraceFilter{SessionID: "s-pg-test", Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total < 1 || len(list) < 1 {
		t.Fatalf("list traces: total=%d len=%d", total, len(list))
	}
}

func TestMemoryListTraces_EndTimeUsesStartTime(t *testing.T) {
	store := NewMemoryStorage()
	ctx := context.Background()
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID:   "t1",
		SessionID: "s1",
		StartTime: base,
		EndTime:   base.Add(2 * time.Hour), // EndTime 很晚
		Status:    model.SpanStatusSuccess,
	})

	// EndTime 过滤应对齐 PG：按 StartTime <= filter.EndTime
	_, total, err := store.ListTraces(ctx, TraceFilter{
		SessionID: "s1",
		EndTime:   base.Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("want 1 (start within range), got %d", total)
	}

	_, total2, err := store.ListTraces(ctx, TraceFilter{
		SessionID: "s1",
		EndTime:   base.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if total2 != 0 {
		t.Fatalf("want 0 (start after EndTime), got %d", total2)
	}
}
