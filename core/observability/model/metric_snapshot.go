package model

import "time"

// MetricSnapshot 是某租户、某固定 scope 最近一次成功刷新的指标快照。
type MetricSnapshot struct {
	ID uint `gorm:"primaryKey"` // 主键，自增ID

	TenantID string `gorm:"uniqueIndex:idx_metric_snapshot_tenant_scope;size:64;not null"` // 租户ID，联合唯一索引
	Scope    string `gorm:"uniqueIndex:idx_metric_snapshot_tenant_scope;size:16;not null"` // 指标所属 scope，联合唯一索引

	WindowFrom    time.Time `gorm:"not null"` // 快照窗口起始时间（统计周期起点，闭区间）
	WindowTo      time.Time `gorm:"not null"` // 快照窗口结束时间（统计周期终点，闭区间）
	TotalTraces   int64     // 快照时间窗内累计 trace 数
	TotalTokens   int64     // 快照时间窗内累计 token 数
	TotalCost     float64   // 快照时间窗内累计消耗（USD）
	AvgDurationMs float64   // 快照时间窗内平均耗时（毫秒）
	SuccessRate   float64   // 成功率
	TopToolsJSON  string    `gorm:"type:text;not null"` // 热门工具TopN，JSON序列化
	RefreshedAt   time.Time `gorm:"index;not null"`     // 刷新更新时间（快照生成时间）
}

func (MetricSnapshot) TableName() string {
	return TableMetricSnapshots
}
