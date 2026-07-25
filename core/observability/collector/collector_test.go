package collector

import (
	"context"
	"testing"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func stateFromCtx(ctx context.Context) *State {
	s, _ := ctx.Value(ctxKey{}).(*State)
	return s
}

func TestTraceIDFromCtx_Empty(t *testing.T) {
	if got := TraceIDFromCtx(context.Background()); got != "" {
		t.Fatalf("want empty trace id, got %q", got)
	}
}

func TestWithObsCallback_Pipeline(t *testing.T) {
	store := storage.NewMemoryStorage()

	ctx, handler, finish := WithObsCallback(context.Background(), store, Config{
		SessionID: "session-test-001",
		UserInput: "北京今天天气怎么样？",
	})
	if handler == nil {
		t.Fatal("handler is nil")
	}

	traceID := TraceIDFromCtx(ctx)
	if traceID == "" {
		t.Fatal("trace id should not be empty after WithObsCallback")
	}

	state := stateFromCtx(ctx)
	if state == nil {
		t.Fatal("state not found in ctx")
	}

	agentSpan := state.startSpan(model.SpanTypeAgent, "weather-agent")
	state.finishSpan(agentSpan.SpanID, func(sp *model.Span) {
		sp.Status = model.SpanStatusSuccess
		sp.Reasoning = "北京今天天气怎么样？"
	})

	llm1 := state.startSpan(model.SpanTypeLLM, "gpt-4o-mini")
	state.finishSpan(llm1.SpanID, func(sp *model.Span) {
		sp.Status = model.SpanStatusSuccess
		sp.ModelName = "gpt-4o-mini"
		sp.PromptTokens = 100
		sp.CompletionTokens = 20
		sp.TotalTokens = 120
		sp.Cost = EstimateCost("gpt-4o-mini", 100, 20)
		state.addLLMTokens(sp.PromptTokens, sp.CompletionTokens, sp.TotalTokens, sp.Cost)
	})

	toolSpan := state.startSpan(model.SpanTypeTool, "get_weather")
	state.finishSpan(toolSpan.SpanID, func(sp *model.Span) {
		sp.Status = model.SpanStatusSuccess
		sp.ToolName = "get_weather"
		sp.ToolInput = `{"city":"北京"}`
		sp.ToolOutput = `{"temp":25,"unit":"C"}`
	})

	llm2 := state.startSpan(model.SpanTypeLLM, "gpt-4o-mini")
	state.finishSpan(llm2.SpanID, func(sp *model.Span) {
		sp.Status = model.SpanStatusSuccess
		sp.ModelName = "gpt-4o-mini"
		sp.PromptTokens = 80
		sp.CompletionTokens = 40
		sp.TotalTokens = 120
		sp.Cost = EstimateCost("gpt-4o-mini", 80, 40)
		state.addLLMTokens(sp.PromptTokens, sp.CompletionTokens, sp.TotalTokens, sp.Cost)
	})

	const wantOutput = "北京今天 25°C，晴"
	finish(ctx, wantOutput, nil)

	if len(store.Traces) != 1 {
		t.Fatalf("want 1 trace, got %d", len(store.Traces))
	}
	tr := store.Traces[0]
	if tr.TraceID != traceID {
		t.Fatalf("trace id mismatch: ctx=%q store=%q", traceID, tr.TraceID)
	}
	if tr.SessionID != "session-test-001" {
		t.Fatalf("session id = %q", tr.SessionID)
	}
	if tr.UserInput != "北京今天天气怎么样？" {
		t.Fatalf("user input = %q", tr.UserInput)
	}
	if tr.AgentOutput != wantOutput {
		t.Fatalf("agent output = %q", tr.AgentOutput)
	}
	if tr.Status != model.SpanStatusSuccess {
		t.Fatalf("trace status = %q", tr.Status)
	}
	if tr.SpanCount != 4 {
		t.Fatalf("span count = %d, want 4", tr.SpanCount)
	}
	if tr.TotalTokens != 240 {
		t.Fatalf("total tokens = %d, want 240", tr.TotalTokens)
	}
	if tr.TotalCost <= 0 {
		t.Fatalf("total cost should be > 0, got %f", tr.TotalCost)
	}

	if len(store.Spans) != 4 {
		t.Fatalf("want 4 spans, got %d", len(store.Spans))
	}
	for _, sp := range store.Spans {
		if sp.TraceID != traceID {
			t.Fatalf("span %s trace_id = %q", sp.SpanID, sp.TraceID)
		}
	}
}

func TestWithObsCallback_FinishWithError(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx, _, finish := WithObsCallback(context.Background(), store, Config{
		SessionID: "s-err",
		UserInput: "fail case",
	})

	state := stateFromCtx(ctx)
	sp := state.startSpan(model.SpanTypeAgent, "agent")
	state.finishSpan(sp.SpanID, func(s *model.Span) {
		s.Status = model.SpanStatusSuccess
	})

	finish(ctx, "", context.Canceled)

	if len(store.Traces) != 1 {
		t.Fatalf("want 1 trace, got %d", len(store.Traces))
	}
	if store.Traces[0].Status != model.SpanStatusError {
		t.Fatalf("trace status = %q, want error", store.Traces[0].Status)
	}
}

func TestNoContent_ClearsBodies(t *testing.T) {
	store := storage.NewMemoryStorage()
	cfg := Config{
		SessionID: "s1",
		UserInput: "手机号13800138000保密",
		NoContent: true,
	}

	ctx, _, finish := WithObsCallback(context.Background(), store, cfg)
	st := stateFromCtx(ctx)

	sp := st.startSpan(model.SpanTypeTool, "t")
	st.finishSpan(sp.SpanID, func(s *model.Span) {
		s.Status = model.SpanStatusSuccess
		s.ToolInput = `{"phone":"13800138000"}`
		s.ToolOutput = "ok"
	})
	finish(ctx, "最终答案含13800138000", nil)

	got, err := store.GetTrace(context.Background(), "default", TraceIDFromCtx(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if got.UserInput != "" || got.AgentOutput != "" {
		t.Fatalf("want empty content, got input=%q output=%q", got.UserInput, got.AgentOutput)
	}
	spans, err := store.GetTraceSpans(context.Background(), "default", got.TraceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range spans {
		if sp.ToolInput != "" || sp.ToolOutput != "" || sp.Reasoning != "" {
			t.Fatalf("span content should be empty: %+v", sp)
		}
	}
}

func TestRedact_MasksPhone(t *testing.T) {
	store := storage.NewMemoryStorage()
	cfg := Config{
		SessionID: "s1",
		UserInput: "联系我13800138000",
		Redact:    true,
	}
	ctx, _, finish := WithObsCallback(context.Background(), store, cfg)
	finish(ctx, "回拨13800138000", nil)

	got, err := store.GetTrace(context.Background(), "default", TraceIDFromCtx(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if got.UserInput != "联系我1**********" {
		t.Fatalf("user_input = %q", got.UserInput)
	}
	if got.AgentOutput != "回拨1**********" {
		t.Fatalf("agent_output = %q", got.AgentOutput)
	}
}

func TestDropIncrementsCounter_Stable(t *testing.T) {
	old := spanChBuffer
	spanChBuffer = 1
	defer func() { spanChBuffer = old }()

	store := storage.NewMemoryStorage()
	st := newState(store, Config{SessionID: "s1", UserInput: "x"})
	// 不启动 worker：channel 只有 1 格，第二次 finish 必丢
	s1 := st.startSpan(model.SpanTypeTool, "a")
	st.finishSpan(s1.SpanID, nil)
	s2 := st.startSpan(model.SpanTypeTool, "b")
	st.finishSpan(s2.SpanID, nil)

	if st.Stats().DroppedSpans.Load() != 1 {
		t.Fatalf("dropped=%d want 1", st.Stats().DroppedSpans.Load())
	}
}
