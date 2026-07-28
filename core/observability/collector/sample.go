package collector

import (
	"math/rand"

	"github.com/T-Dwag/agent-observability/model"
)

// 采样概率
var sampleFloat = rand.Float64()

// shouldKeep 判定当前 Trace/Span 是否需要保留（写入观察数据）。基于成功率采样，异常情况与高消耗必保留。
//   - 失败或异常（status ≠ Success）：全部保留，便于排查问题
//   - 成本高于阈值（CostKeepUSD > 0 且 totalCost 超过）：全部保留，便于分析贵价调用
//   - 其他成功流程：按采样率保持（SampleSuccessRate）
//
// cfg:    当前采集配置（含采样率、成本阈值等）
// status: Span/Trace 状态（成功、失败等）
// totalCost: 本次执行总成本（单位 USD）
func shouldKeep(cfg Config, status string, totalCost float64) bool {

	// 非成功状态（失败/异常）全部保留
	if status != model.SpanStatusSuccess {
		return true
	}

	// 总成本达阈值，强制保留（>= 与 CostAlert 一致）
	if cfg.CostKeepUSD > 0 && totalCost >= cfg.CostKeepUSD {
		return true
	}

	// 获取配置中的采样率，并对边界值做归一化：
	// - rate < 0：视为未正确配置，按 100% 全量保留，避免误丢成功链路
	// - rate > 1：同样按 100% 处理（调用方可能写成 100 表示百分比）
	rate := cfg.SampleSuccessRate
	if rate < 0 {
		rate = 1
	}

	// rate >= 1：成功链路也全部保留，等价于关闭成功采样
	if rate >= 1 {
		return true
	}

	// rate == 0：明确关闭成功采样，成功链路全部丢弃
	// （失败/高成本已在上方强制保留，不受此处影响）
	if rate <= 0 {
		return false
	}

	// 按采样率抽样。这里刻意复用进程启动时生成的全局 sampleFloat，
	// 而不是每次 Trace 重新 rand：同一进程内成功链路要么整体保留、要么整体丢弃，
	// 便于本地联调时结果稳定可复现，也避免高频调用反复掷骰子带来抖动。
	return sampleFloat <= rate
}
