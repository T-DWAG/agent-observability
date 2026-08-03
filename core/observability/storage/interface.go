package storage

import (
	"context"
	"time"

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

	// CreateEvaluationJob 原子创建 dimension=overall 的任务行。
	// 同一 trace 只允许一条；重复创建返回 ErrorEvaluationExists。
	CreateEvaluationJob(ctx context.Context, evaluation *model.Evaluation) error
	SaveEvaluation(ctx context.Context, evaluation *model.Evaluation) error
	ListEvaluations(ctx context.Context, traceID string) ([]*model.Evaluation, error)

	// PurgeTrace 按租户 + TraceID 删除整条 Trace 及其关联 Span；
	// 用于采样丢弃后清理已落盘数据，避免残留半截记录。
	// 参数顺序与 GetTrace 一致：tenantID 在前。
	PurgeTrace(ctx context.Context, tenantID, traceID string) error
	// PurgeBefore 删除指定租户 start_time < before 的 Trace（含 Span）；
	// 返回实际删除的 Trace 条数，用于保留策略 / 定时 GC。
	PurgeBefore(ctx context.Context, tenantID string, before time.Time) (traces int64, err error)

	// UpdateEvaluationStatus 只更新 dimension=overall 的任务行，不修改三维评分。
	UpdateEvaluationStatus(ctx context.Context, traceID string, status, errorMsg string) error

	// SaveMetricSnapshot 保存某租户、某 scope 最新的指标快照。
	// 入参:
	//   - ctx：上下文
	//   - snapshot：待保存的指标快照结构体指针
	// 返回:
	//   - error：保存过程中的错误（如有），否则为 nil
	SaveMetricSnapshot(ctx context.Context, snapshot *model.MetricSnapshot) error

	// GetMetricSnapshot 获取某租户、某 scope 最近一次成功刷新的指标快照。
	// 入参:
	//   - ctx: 上下文
	//   - tenantID: 租户 ID
	//   - scope: 指标作用域
	// 返回:
	//   - *model.MetricSnapshot: 匹配的指标快照结构体指针，若无返回 nil
	//   - error: 读取过程中的错误（如有），否则为 nil
	GetMetricSnapshot(ctx context.Context, tenantID, scope string) (*model.MetricSnapshot, error)
}
