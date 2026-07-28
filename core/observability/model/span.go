package model

import "time"

// Span 代表 Agent 执行过程中的一个步骤。
// 设计参考 Coze Loop loop_span.Span，简化为 llm/tool/agent 三类。
type Span struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	SpanID       string `gorm:"uniqueIndex;size:64;not null"`
	TraceID      string `gorm:"index;size:64;not null"`
	ParentSpanID string `gorm:"size:64"` // 空字符串表示根 Span

	SpanType string `gorm:"size:32;index;not null"` // llm | tool | agent
	SpanName string `gorm:"size:128"`

	// --- LLM 相关（SpanType=llm 时填充）---
	ModelName        string `gorm:"size:64"`
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	Cost             float64 // 美元，Step 2 采集时计算

	// --- Tool 相关（SpanType=tool 时填充）---
	ToolName   string `gorm:"size:64"`
	ToolInput  string `gorm:"type:text"`
	ToolOutput string `gorm:"type:text"`

	// --- 时间 ---
	StartTime  time.Time `gorm:"index;not null"`
	EndTime    time.Time
	DurationMs int64 // EndTime - StartTime，毫秒

	// --- 状态 ---
	Status    string `gorm:"size:16;index;not null;default:running"`
	ErrorMsg  string `gorm:"type:text"`
	Reasoning string `gorm:"type:text"` // Agent 推理过程（SpanType=agent 时用）
}

func (Span) TableName() string {
	return TableSpans
}
