package collector

import (
	"regexp"

	"github.com/T-Dwag/agent-observability/model"
)

// phoneRE 极简手机号匹配：1 开头共 11 位数字。
// 专题0只证明「有开关」；生产环境可替换为更完整的规则库（身份证、邮箱、银行卡等）。
var phoneRE = regexp.MustCompile(`1\d{10}`)

// redactText 对单段文本做敏感信息打码。
// 空串直接返回，避免无意义的正则扫描。
func redactText(s string) string {
	if s == "" {
		return s
	}
	return phoneRE.ReplaceAllString(s, "1**********")
}

// applyContentPolicy 在落盘前统一执行内容策略，顺序固定为两步：
//
//  1. NoContent=true → 清空 Trace/Span 正文类字段后直接返回
//  2. NoContent=false 且 Redact=true → 对保留正文打码后再落盘
//
// tr / sp 均可为 nil：调用方可能只处理 Trace 或只处理 Span。
func applyContentPolicy(cfg Config, tr *model.Trace, sp *model.Span) {
	if cfg.NoContent {

		//重置Trace/Span上的正文类字段
		if tr != nil {
			tr.UserInput = ""
			tr.AgentOutput = ""
		}
		if sp != nil {
			sp.Reasoning = ""
			sp.ToolInput = ""
			sp.ToolOutput = ""
			sp.ErrorMsg = ""
		}
		return
	}
	if !cfg.Redact {
		return
	}

	//对Trace/Span上的正文类字段做打码
	if tr != nil {
		tr.UserInput = redactText(tr.UserInput)
		tr.AgentOutput = redactText(tr.AgentOutput)
	}
	if sp != nil {
		sp.Reasoning = redactText(sp.Reasoning)
		sp.ToolInput = redactText(sp.ToolInput)
		sp.ToolOutput = redactText(sp.ToolOutput)
		sp.ErrorMsg = redactText(sp.ErrorMsg)
	}
}
