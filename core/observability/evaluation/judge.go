package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

type Judge struct {
	store storage.Storage
	llm   ChatCompleter
}

func NewJudge(store storage.Storage, llm ChatCompleter) *Judge {
	return &Judge{store: store, llm: llm}
}

type dimScore struct {
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

type judgePayload struct {
	Accuracy   dimScore `json:"accuracy"`
	ToolUsage  dimScore `json:"tool_usage"`
	Efficiency dimScore `json:"efficiency"`
}

func (j *Judge) Evaluate(ctx context.Context, tenantID, traceID string) ([]*model.Evaluation, error) {
	tr, err := j.store.GetTrace(ctx, tenantID, traceID)
	if err != nil {
		return nil, err
	}
	spans, err := j.store.GetTraceSpans(ctx, tenantID, traceID)
	if err != nil {
		return nil, fmt.Errorf("get trace spans: %w", err)
	}

	raw, err := j.llm.Complete(ctx, systemPrompt, buildUserPrompt(tr, spans))
	if err != nil {
		return nil, fmt.Errorf("complete: %w", err)
	}
	payload, err := parseJudgePayload(raw)
	if err != nil {
		return nil, fmt.Errorf("parse judge payload: %w", err)
	}

	now := time.Now().UTC()
	evals := []*model.Evaluation{
		{TraceID: traceID, Dimension: model.EvalDimensionAccuracy, Score: clamp01(payload.Accuracy.Score), Reason: payload.Accuracy.Reason, CreatedAt: now},
		{TraceID: traceID, Dimension: model.EvalDimensionToolUsage, Score: clamp01(payload.ToolUsage.Score), Reason: payload.ToolUsage.Reason, CreatedAt: now},
		{TraceID: traceID, Dimension: model.EvalDimensionEfficiency, Score: clamp01(payload.Efficiency.Score), Reason: payload.Efficiency.Reason, CreatedAt: now},
	}

	for _, e := range evals {
		if err := j.store.SaveEvaluation(ctx, e); err != nil {
			return nil, err
		}
	}
	return evals, nil
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func parseJudgePayload(raw string) (*judgePayload, error) {
	raw = strings.TrimSpace(raw)

	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j >= i {
			raw = raw[i : j+1]
		}
	}
	var payload judgePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("parse judge json: %w; raw=%q", err, raw)
	}
	return &payload, nil
}
