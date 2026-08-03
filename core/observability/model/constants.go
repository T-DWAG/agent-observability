package model

// Span类型指的是对链路追踪中的不同操作或过程进行区分和分类的类型标签。通常用于标识某一段操作（Span）属于哪种类别，例如大模型推理（llm）、工具调用（tool）、或代理行为（agent）。
const (
	SpanTypeLLM   = "llm"
	SpanTypeTool  = "tool"
	SpanTypeAgent = "agent"
)

// Span 状态 — 对应 Coze Loop 的 SpanStatusModel / SpanStatusTool / SpanStatusAgent
const (
	SpanStatusSuccess = "success"
	SpanStatusError   = "error"
	SpanStatusPending = "running"
)

// 评估维度 — Step 5 会用到，现在先定义
const (
	EvalDimensionAccuracy   = "accuracy"
	EvalDimensionToolUsage  = "tool_usage"
	EvalDimensionEfficiency = "efficiency"
)

// 表名 — GORM 默认会用结构体名复数化，显式指定更清晰
const (
	TableSpans           = "obs_spans"
	TableTraces          = "obs_traces"
	TableEvaluations     = "obs_evaluations"
	TableMetricSnapshots = "obs_metric_snapshots"
)
