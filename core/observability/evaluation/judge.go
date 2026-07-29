package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

func (j *Judge) EvaluateAsync(ctx context.Context, tenantID, traceID string) error {

	//1、校验trace存在不
	tr, err := j.store.GetTrace(ctx, tenantID, traceID)
	if err != nil {
		return err
	}

	_, err = j.store.GetTraceSpans(ctx, tenantID, traceID)
	if err != nil {
		return fmt.Errorf("get trace spans: %w", err)
	}

	// 原子创建任务行：一条 Trace 只允许评估一次。
	now := time.Now().UTC()
	placeholder := &model.Evaluation{
		TraceID:   traceID,
		Dimension: "overall",
		Status:    model.EvalStatusPending,
		CreatedAt: now,
	}
	if err := j.store.CreateEvaluationJob(ctx, placeholder); err != nil {
		return err
	}

	go j.runEvaluation(traceID, tr, tenantID)
	return nil
}

func (j *Judge) runEvaluation(traceID string, tr *model.Trace, tenantID string) {
	ctx := context.Background()

	if err := j.store.UpdateEvaluationStatus(ctx, traceID, model.EvalStatusRunning, ""); err != nil {
		log.Printf("[obs] mark evaluation running failed trace=%s: %v", traceID, err)
		return
	}

	spans, err := j.store.GetTraceSpans(ctx, tenantID, traceID)
	if err != nil {
		j.markFailed(ctx, traceID, fmt.Sprintf("get spans: %v", err))
		return
	}

	raw, err := j.llm.Complete(ctx, systemPrompt, buildUserPrompt(tr, spans))
	if err != nil {
		log.Printf("[obs] judge complete failed trace=%s: %v", traceID, err)
		j.markFailed(ctx, traceID, fmt.Sprintf("llm: %v", err))
		return
	}

	payload, err := parseJudgePayload(raw)
	if err != nil {
		j.markFailed(ctx, traceID, fmt.Sprintf("parse: %v", err))
		return
	}

	// 成功：写入 3 条维度评分
	now := time.Now().UTC()
	evals := []*model.Evaluation{
		{TraceID: traceID, Dimension: model.EvalDimensionAccuracy, Score: clamp01(payload.Accuracy.Score), Reason: payload.Accuracy.Reason, Status: model.EvalStatusDone, CreatedAt: now},
		{TraceID: traceID, Dimension: model.EvalDimensionToolUsage, Score: clamp01(payload.ToolUsage.Score), Reason: payload.ToolUsage.Reason, Status: model.EvalStatusDone, CreatedAt: now},
		{TraceID: traceID, Dimension: model.EvalDimensionEfficiency, Score: clamp01(payload.Efficiency.Score), Reason: payload.Efficiency.Reason, Status: model.EvalStatusDone, CreatedAt: now},
	}
	for _, e := range evals {
		if err := j.store.SaveEvaluation(ctx, e); err != nil {
			log.Printf("[obs] save eval failed trace=%s dim=%s: %v", traceID, e.Dimension, err)
			j.markFailed(ctx, traceID, fmt.Sprintf("save %s: %v", e.Dimension, err))
			return
		}
	}
	if err := j.store.UpdateEvaluationStatus(ctx, traceID, model.EvalStatusDone, ""); err != nil {
		log.Printf("[obs] mark evaluation done failed trace=%s: %v", traceID, err)
	}
}

func (j *Judge) markFailed(ctx context.Context, traceID, message string) {
	if err := j.store.UpdateEvaluationStatus(ctx, traceID, model.EvalStatusFailed, message); err != nil {
		log.Printf("[obs] mark evaluation failed trace=%s: %v", traceID, err)
	}
}
