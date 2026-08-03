package metrics

import "time"

// Snapshot 一段时间内的聚合看板数据。
type Snapshot struct {
	Scope         string     `json:"scope"`           // 时间范围标识，如 last_24h / last_7d / last_30d
	From          time.Time  `json:"from"`            // 聚合窗口起始时间
	To            time.Time  `json:"to"`              // 聚合窗口结束时间
	TotalTraces   int64      `json:"total_traces"`    // 窗口内 Trace 总数
	TotalTokens   int64      `json:"total_tokens"`    // 窗口内累计 Token 数
	TotalCost     float64    `json:"total_cost"`      // 窗口内累计费用估算
	AvgDurationMs float64    `json:"avg_duration_ms"` // 平均 Trace 耗时（毫秒）
	SuccessRate   float64    `json:"success_rate"`    // 成功率，取值范围 0~1
	TopTools      []ToolStat `json:"top_tools"`       // 调用次数最多的工具排行

	RefreshedAt time.Time `json:"refreshed_at"`
}

// ToolStat 单个工具的调用统计。
type ToolStat struct {
	Name  string `json:"name"`  // 工具名称
	Count int64  `json:"count"` // 调用次数
}

// 支持的 scope 常量
const (
	ScopeLast24h = "last_24h" // 最近 24 小时
	ScopeLast7d  = "last_7d"  // 最近 7 天
	ScopeLast30d = "last_30d" // 最近 30 天
)
