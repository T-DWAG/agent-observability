package model

import "time"

// Evaluation 代表对某次 Trace 的自动化质量评估（LLM-as-Judge，Step 5 实现）。
type Evaluation struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	TraceID   string    `gorm:"index;uniqueIndex:idx_eval_trace_dimension;size:64;not null"`
	Dimension string    `gorm:"uniqueIndex:idx_eval_trace_dimension;size:32;not null"` // overall | accuracy | tool_usage | efficiency
	Score     float64   // 0.0 ~ 1.0 或 0 ~ 100，Step 5 统一约定
	Reason    string    `gorm:"type:text"`
	CreatedAt time.Time `gorm:"index;not null"`

	// Status：overall 行表示任务状态；维度行写入后固定为 done。
	Status string `gorm:"size:16;index;not null;default:'pending'"`
	// ErrorMsg 仅记录 overall 任务的失败原因。
	ErrorMsg string `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`
}

func (Evaluation) TableName() string {
	return TableEvaluations
}

// 评估状态 — 专题 4 异步化
const (
	EvalStatusPending = "pending"
	EvalStatusRunning = "running"
	EvalStatusDone    = "done"
	EvalStatusFailed  = "failed"
)
