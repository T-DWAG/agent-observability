package collector

import (
	"context"
	"testing"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func TestShouldKeep_ErrorAlways(t *testing.T) {
	cfg := Config{SampleSuccessRate: 0}
	if !shouldKeep(cfg, model.SpanStatusError, 0) {
		t.Fatal("error must keep")
	}
}

func TestShouldKeep_SuccessRateZero(t *testing.T) {
	cfg := Config{SampleSuccessRate: 0}
	if shouldKeep(cfg, model.SpanStatusSuccess, 0) {
		t.Fatal("success rate 0 must drop")
	}
}

func TestShouldKeep_CostKeep(t *testing.T) {
	cfg := Config{SampleSuccessRate: 0, CostKeepUSD: 0.01}
	if !shouldKeep(cfg, model.SpanStatusSuccess, 0.02) {
		t.Fatal("high cost must keep")
	}
}

func TestShouldKeep_NegativeRateMeansAll(t *testing.T) {
	cfg := Config{SampleSuccessRate: -1}
	if !shouldKeep(cfg, model.SpanStatusSuccess, 0) {
		t.Fatal("rate<0 means keep all success")
	}
}

func TestSampleDrop_PurgesAndCounts(t *testing.T) {
	store := storage.NewMemoryStorage()
	cfg := Config{
		SessionID:         "s",
		UserInput:         "hi",
		SampleSuccessRate: 0,
	}
	ctx, _, finish := WithObsCallback(context.Background(), store, cfg)
	st := stateFromCtx(ctx)
	sp := st.startSpan(model.SpanTypeAgent, "a")
	st.finishSpan(sp.SpanID, func(s *model.Span) {
		s.Status = model.SpanStatusSuccess
	})
	finish(ctx, "ok", nil)

	if st.Stats().SampledOutTraces.Load() != 1 {
		t.Fatalf("sampled_out=%d want 1", st.Stats().SampledOutTraces.Load())
	}

	_, total, err := store.ListTraces(context.Background(), storage.TraceFilter{TenantID: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("total=%d want 0 (trace should be purged)", total)
	}
	if len(store.Spans) != 0 {
		t.Fatalf("spans=%d want 0 after purge", len(store.Spans))
	}
}
