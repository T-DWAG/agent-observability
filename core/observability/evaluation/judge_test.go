package evaluation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func saveEvaluationTrace(t *testing.T, store storage.Storage, traceID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := store.SaveTrace(ctx, &model.Trace{
		TraceID: traceID, TenantID: "default", SessionID: "s1", UserInput: "北京天气？",
		AgentOutput: "25°C 晴", StartTime: now, Status: model.SpanStatusSuccess,
		TotalTokens: 120,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSpan(ctx, &model.Span{
		SpanID: "sp-" + traceID, TraceID: traceID, SpanType: model.SpanTypeTool,
		SpanName: "get_weather", ToolName: "get_weather", StartTime: now,
		Status: model.SpanStatusSuccess,
	}); err != nil {
		t.Fatal(err)
	}
}

func waitEvaluationStatus(t *testing.T, store storage.Storage, traceID string) (*model.Evaluation, []*model.Evaluation) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		list, err := store.ListEvaluations(context.Background(), traceID)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			if e.Dimension == "overall" &&
				(e.Status == model.EvalStatusDone || e.Status == model.EvalStatusFailed) {
				return e, list
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("evaluation %s did not finish", traceID)
	return nil, nil
}

func TestEvaluateAsync_Done(t *testing.T) {
	store := storage.NewMemoryStorage()
	saveEvaluationTrace(t, store, "tr-eval")

	j := NewJudge(store, FakeCompleter{})
	if err := j.EvaluateAsync(context.Background(), "default", "tr-eval"); err != nil {
		t.Fatal(err)
	}

	job, list := waitEvaluationStatus(t, store, "tr-eval")
	if job.Status != model.EvalStatusDone || job.ErrorMsg != "" {
		t.Fatalf("job=%+v", job)
	}
	if len(list) != 4 {
		t.Fatalf("stored=%d want overall+3 dimensions", len(list))
	}
	dimensions := 0
	for _, e := range list {
		if e.Dimension == "overall" {
			continue
		}
		dimensions++
		if e.Status != model.EvalStatusDone {
			t.Fatalf("dimension status=%q", e.Status)
		}
		if e.Score < 0 || e.Score > 1 {
			t.Fatalf("score out of range: %v", e)
		}
	}
	if dimensions != 3 {
		t.Fatalf("dimensions=%d", dimensions)
	}
}

func TestEvaluateAsync_TraceNotFound(t *testing.T) {
	j := NewJudge(storage.NewMemoryStorage(), FakeCompleter{})
	err := j.EvaluateAsync(context.Background(), "default", "missing")
	if !errors.Is(err, storage.ErrorNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestEvaluateAsync_Duplicate(t *testing.T) {
	store := storage.NewMemoryStorage()
	saveEvaluationTrace(t, store, "tr-duplicate")
	j := NewJudge(store, FakeCompleter{})
	if err := j.EvaluateAsync(context.Background(), "default", "tr-duplicate"); err != nil {
		t.Fatal(err)
	}
	err := j.EvaluateAsync(context.Background(), "default", "tr-duplicate")
	if !errors.Is(err, storage.ErrorEvaluationExists) {
		t.Fatalf("err=%v", err)
	}
}

func TestEvaluateAsync_LLMFailure(t *testing.T) {
	store := storage.NewMemoryStorage()
	saveEvaluationTrace(t, store, "tr-failed")
	j := NewJudge(store, FakeCompleter{Err: errors.New("llm unavailable")})
	if err := j.EvaluateAsync(context.Background(), "default", "tr-failed"); err != nil {
		t.Fatal(err)
	}
	job, _ := waitEvaluationStatus(t, store, "tr-failed")
	if job.Status != model.EvalStatusFailed || job.ErrorMsg == "" {
		t.Fatalf("job=%+v", job)
	}
}

type failDimensionStorage struct {
	storage.Storage
	dimension string
}

func (s *failDimensionStorage) SaveEvaluation(ctx context.Context, e *model.Evaluation) error {
	if e.Dimension == s.dimension {
		return fmt.Errorf("forced save failure")
	}
	return s.Storage.SaveEvaluation(ctx, e)
}

func TestEvaluateAsync_PartialSaveMarksFailed(t *testing.T) {
	memory := storage.NewMemoryStorage()
	store := &failDimensionStorage{Storage: memory, dimension: model.EvalDimensionToolUsage}
	saveEvaluationTrace(t, store, "tr-partial")
	j := NewJudge(store, FakeCompleter{})
	if err := j.EvaluateAsync(context.Background(), "default", "tr-partial"); err != nil {
		t.Fatal(err)
	}
	job, _ := waitEvaluationStatus(t, memory, "tr-partial")
	if job.Status != model.EvalStatusFailed || job.ErrorMsg == "" {
		t.Fatalf("job=%+v", job)
	}
}
