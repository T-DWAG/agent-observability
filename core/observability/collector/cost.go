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
	for key, r := range priceTable {
		if strings.Contains(name, key) {
			return r.PromptPerM, r.CompletionPerM
		}
	}
	return 0.15, 0.60
}

type TokenRates struct {
	PromptPerM     float64 //提示词每百万tokens成本
	CompletionPerM float64 //完成词每百万tokens成本
}

var defaultRates = map[string]TokenRates{
	"gpt-4o-mini": {0.15, 0.60},
	"gpt-4o":      {2.5, 10.0},
	"deepseek":    {0.27, 1.1},
	"claude-3.5":  {3.0, 15.0},
	"claude-3-5":  {3.0, 15.0},
}

var priceTable = defaultRates

func SetPriceTable(m map[string]TokenRates) {
	if m == nil {
		priceTable = defaultRates
		return
	}
	priceTable = m
}
