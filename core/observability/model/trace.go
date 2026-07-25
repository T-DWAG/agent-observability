package model

import "time"

// Trace 代表一次完整的 Agent 执行（从用户输入到最终输出）。
// 对应 Coze Loop 中 TraceData + 根 Span 的聚合视图。
type Trace struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	TraceID   string `gorm:"uniqueIndex;size:64;not null"`
	TenantID  string `gorm:"index;size:64;not null;default:default"`
	SessionID string `gorm:"index;size:64"` // 同一会话多次 Trace 可关联

	UserInput   string `gorm:"type:text"`
	AgentOutput string `gorm:"type:text"`

	StartTime  time.Time `gorm:"index;not null"`
	EndTime    time.Time
	DurationMs int64

	SpanCount   int   // 本次 Trace 包含多少 Span
	TotalTokens int64 // 所有 LLM Span 的 token 之和
	TotalCost   float64
	Status      string `gorm:"size:16;index;not null;default:running"`
}

func (Trace) TableName() string {
	return TableTraces
}
