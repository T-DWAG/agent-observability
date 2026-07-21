package collector

import (
	"strings"
)

func EstimateCost(
	modelName string, //模型名称
	promptTokens int64, //提示词tokens
	completionTokens int64, //完成词tokens
) float64 { //返回估计成本
	promptRate, completionRate := lookupRates(modelName)
	return float64(promptTokens)/1_000_000*promptRate +
		float64(completionTokens)/1_000_000*completionRate //每百万tokens成本
}

func lookupRates(modelName string) (promptPerM, completionPerM float64) { //查询模型成本
	name := strings.ToLower(modelName)
	switch {
	case strings.Contains(name, "gpt-4o-mini"):
		return 0.15, 0.60
	case strings.Contains(name, "gpt-4o"):
		return 2.5, 10.0
	case strings.Contains(name, "deepseek"):
		return 0.27, 1.1
	case strings.Contains(name, "claude-3.5") || strings.Contains(name, "claude-3-5"):
		return 3.0, 15.0
	default:
		return 0.15, 0.60
	}
}
