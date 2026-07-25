package evaluation

import (
	"context"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

// TestJudge_Evaluate_FakeLLM 验证 Judge 在 FakeCompleter 下能对完整 Trace 产出并持久化评估结果。
func TestJudge_Evaluate_FakeLLM(t *testing.T) {
	// 准备内存存储与上下文，构造一条成功 Trace 及其工具 Span 作为评估输入。
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "tr-eval", TenantID: "default", SessionID: "s1", UserInput: "北京天气？",
		AgentOutput: "25°C 晴", StartTime: now, Status: model.SpanStatusSuccess,
		TotalTokens: 120,
	})
	_ = store.SaveSpan(ctx, &model.Span{
		SpanID: "sp1", TraceID: "tr-eval", SpanType: model.SpanTypeTool,
		SpanName: "get_weather", ToolName: "get_weather", StartTime: now,
		Status: model.SpanStatusSuccess,
	})

	// 使用 FakeCompleter 执行评估，期望无错误且返回 3 条评估结果。
	j := NewJudge(store, FakeCompleter{})
	evals, err := j.Evaluate(ctx, "default", "tr-eval")
	if err != nil {
		t.Fatal(err)
	}
	if len(evals) != 3 {
		t.Fatalf("want 3 evals, got %d", len(evals))
	}

	// 从存储回读评估记录，确认已持久化且分数落在 [0, 1] 合法区间。
	list, err := store.ListEvaluations(ctx, "tr-eval")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("stored = %d", len(list))
	}
	for _, e := range list {
		if e.Score < 0 || e.Score > 1 {
			t.Fatalf("score out of range: %v", e)
		}
	}
}

// TestJudge_TraceNotFound 验证对不存在的 TraceID 调用 Evaluate 时应返回错误。
func TestJudge_TraceNotFound(t *testing.T) {
	j := NewJudge(storage.NewMemoryStorage(), FakeCompleter{})
	_, err := j.Evaluate(context.Background(), "default", "missing")
	if err == nil {
		t.Fatal("want error")
	}
}
