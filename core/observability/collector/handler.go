package collector

import (
	"context"

	obsmodel "github.com/T-Dwag/agent-observability/model"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	ucb "github.com/cloudwego/eino/utils/callbacks"
)

// spanIDKey 是 context 中存放当前 SpanID 的私有 key。
// OnStart 写入，OnEnd/OnError 读出，把同一次回调的起止关联起来。
type spanIDKey struct{}

// withSpanID 把 SpanID 挂到 ctx，供后续 OnEnd/OnError 取回。
func withSpanID(ctx context.Context, spanID string) context.Context {
	return context.WithValue(ctx, spanIDKey{}, spanID)
}

// spanIDFromCtx 从 ctx 取出 OnStart 写入的 SpanID；取不到时返回空串。
func spanIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(spanIDKey{}).(string)
	return id
}

// NewHandler 构建 Eino Callback Handler。
//
// 整体思路：
//  1. 用 HandlerHelper 分别挂 Agent / ChatModel / Tool 三类回调；
//  2. OnStart：创建对应类型的 Span，并把 SpanID 写入 ctx；
//  3. OnEnd：按类型补全字段，标记 success，再 finishSpan 异步落盘；
//  4. OnError：failSpan，标记 error 并记录 ErrorMsg。
//
// Span 生命周期由 State 管理；本文件只负责从 Eino 回调里抽取观测字段。
func NewHandler(state *State) callbacks.Handler {
	return ucb.NewHandlerHelper().
		// ---------- Agent：一次 Agent 执行的起止 ----------
		Agent(&ucb.AgentCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *adk.AgentCallbackInput) context.Context {
				// 创建 Agent Span，记录本次 Agent 调用的开始时间与名称
				sp := state.startSpan(obsmodel.SpanTypeAgent, info.Name)
				// 取输入消息列表的最后一条作为 Reasoning，便于回溯 Agent 决策依据
				if input != nil && input.Input != nil && len(input.Input.Messages) > 0 {
					sp.Reasoning = input.Input.Messages[len(input.Input.Messages)-1].Content
				}
				// 把 SpanID 写入 ctx，供 OnEnd 关联同一条 Span
				return withSpanID(ctx, sp.SpanID)
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *adk.AgentCallbackOutput) context.Context {
				// 标记成功并结束 Span（finishSpan 内部会算耗时并异步落盘）
				state.finishSpan(spanIDFromCtx(ctx), func(sp *obsmodel.Span) {
					sp.Status = obsmodel.SpanStatusSuccess
				})
				// Agent 可能产出异步事件流；后台排空，避免 iterator 阻塞上游
				if output != nil && output.Events != nil {
					go drainAgentEvents(output.Events)
				}
				return ctx
			},
		}).
		// ---------- ChatModel：一次 LLM 调用的起止与 token/费用 ----------
		ChatModel(&ucb.ModelCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *model.CallbackInput) context.Context {
				// 创建 LLM Span；优先用 Config.Model，否则回退到 RunInfo.Type
				sp := state.startSpan(obsmodel.SpanTypeLLM, info.Name)
				sp.ModelName = info.Type
				if input != nil && input.Config != nil {
					sp.ModelName = input.Config.Model
				}
				return withSpanID(ctx, sp.SpanID)
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *model.CallbackOutput) context.Context {
				state.finishSpan(spanIDFromCtx(ctx), func(sp *obsmodel.Span) {
					sp.Status = obsmodel.SpanStatusSuccess
					if output == nil {
						return
					}
					// 输出侧可能带回更准确的模型名，覆盖 OnStart 的初值
					if output.Config != nil && output.Config.Model != "" {
						sp.ModelName = output.Config.Model
					}
					// 记录 token 用量，估算费用，并累加到 Trace 级汇总
					if output.TokenUsage != nil {
						sp.PromptTokens = int64(output.TokenUsage.PromptTokens)
						sp.CompletionTokens = int64(output.TokenUsage.CompletionTokens)
						sp.TotalTokens = int64(output.TokenUsage.TotalTokens)
						sp.Cost = EstimateCost(sp.ModelName, sp.PromptTokens, sp.CompletionTokens)
						state.addLLMTokens(sp.PromptTokens, sp.CompletionTokens, sp.TotalTokens, sp.Cost)
					}
				})
				return ctx
			},
			OnError: func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
				// LLM 调用失败：标记 error 并写入 ErrorMsg
				state.failSpan(spanIDFromCtx(ctx), err)
				return ctx
			},
		}).
		// ---------- Tool：一次工具调用的入参/出参 ----------
		Tool(&ucb.ToolCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *tool.CallbackInput) context.Context {
				// 创建 Tool Span；工具名优先用 RunInfo.Name
				sp := state.startSpan(obsmodel.SpanTypeTool, info.Name)
				sp.ToolName = info.Name
				// 记录调用入参（JSON 字符串），便于排查工具调用问题
				if input != nil {
					sp.ToolInput = input.ArgumentsInJSON
				}
				return withSpanID(ctx, sp.SpanID)
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *tool.CallbackOutput) context.Context {
				state.finishSpan(spanIDFromCtx(ctx), func(sp *obsmodel.Span) {
					sp.Status = obsmodel.SpanStatusSuccess
					if output != nil {
						// 优先用 Response 文本；否则把结构化 ToolOutput 序列化为 JSON
						if output.Response != "" {
							sp.ToolOutput = output.Response
						} else if output.ToolOutput != nil {
							sp.ToolOutput = toJSON(output.ToolOutput)
						}
					}
				})
				return ctx
			},
			OnError: func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
				// 工具调用失败：标记 error 并写入 ErrorMsg
				state.failSpan(spanIDFromCtx(ctx), err)
				return ctx
			},
		}).
		Handler()
}

// drainAgentEvents 消费完 Agent 异步事件流，避免 iterator 阻塞上游。
func drainAgentEvents(events *adk.AsyncIterator[*adk.AgentEvent]) {
	for {
		if _, ok := events.Next(); !ok {
			break
		}
	}
}
