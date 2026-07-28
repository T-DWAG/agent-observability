package model

import "time"

// Evaluation 代表对某次 Trace 的自动化质量评估（LLM-as-Judge，Step 5 实现）。
type Evaluation struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	TraceID   string    `gorm:"index;size:64;not null"`
	Dimension string    `gorm:"size:32;not null"` // accuracy | tool_usage | efficiency
	Score     float64   // 0.0 ~ 1.0 或 0 ~ 100，Step 5 统一约定
	Reason    string    `gorm:"type:text"`
	CreatedAt time.Time `gorm:"index;not null"`
}

func (Evaluation) TableName() string {
	return TableEvaluations
}
