package storage

import (
	"context"

	"github.com/T-Dwag/agent-observability/model"
)

// Storage 存储抽象；Step 3 提供 PostgreSQL 实现，Step 2 可用 MemoryStorage 验证采集。
type Storage interface {
	// SaveSpan 持久化单条 Span；用于采集链路中逐步落库。
	SaveSpan(ctx context.Context, span *model.Span) error
	// SaveTrace 持久化完整 Trace（含其 Span 集合）；用于按 Trace 维度写入或更新。
	SaveTrace(ctx context.Context, trace *model.Trace) error

	//添加租户ID过滤
	GetTrace(ctx context.Context, tenantID, traceID string) (*model.Trace, error)
	GetTraceSpans(ctx context.Context, tenantID, traceID string) ([]*model.Span, error)

	ListTraces(ctx context.Context, filter TraceFilter) ([]*model.Trace, int64, error)

	SaveEvaluation(ctx context.Context, evaluation *model.Evaluation) error
	ListEvaluations(ctx context.Context, traceID string) ([]*model.Evaluation, error)
}
