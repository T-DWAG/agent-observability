package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/T-Dwag/agent-observability/collector"
	"github.com/T-Dwag/agent-observability/storage"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func runOnce(ctx context.Context, store storage.Storage, cfg collector.Config, q string) (traceID string, answer string, err error) {
	//创建上下文和处理器
	ctx, handler, finish := collector.WithObsCallback(ctx, store, cfg)
	defer func() { finish(ctx, answer, err) }() //结束时调用finish

	cm, err := newChatModel(ctx)
	if err != nil {
		return "", "", err
	}

	tools, err := newFileTools()
	if err != nil {
		return "", "", err
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "file-agent",
		Description: "A agent that can search the file in the sandbox",
		Instruction: `You are a helpful assistant that can search the file in the sandbox,
		规则：1. 需要知道有哪些文件时，先调用 list_files。
				2. 需要文件内容或总结某文件时，先调用 read_file。
				3. 不要编造文件内容；工具没返回的内容不要假装读过。
				4. 用简洁中文回答。`,
		Model: cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
	})
	if err != nil {
		return "", "", err
	}

	// 创建 Runner（关闭流式输出）
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: false})
	// 发起查询，并挂上可观测性回调
	iter := runner.Query(ctx, q, adk.WithCallbacks(handler))
	// 消费迭代器，汇总最终回答
	answer, err = drainAgent(iter)
	// 从上下文取出本次 trace ID
	traceID = collector.TraceIDFromCtx(ctx)
	return traceID, answer, err
}

// drainAgent 消费 Agent 异步事件迭代器，从中提取最终的助手回答文本。
//
// 参数：
//   - iter: adk.AsyncIterator，按顺序产出 *adk.AgentEvent。
//     事件可能包含中间推理、工具调用结果、最终消息，也可能为空或带错误。
//
// 返回值：
//   - string: 最后一次非空的 Assistant 消息正文（即用户可见的最终回答）。
//   - error:  迭代过程中遇到的事件错误；若全程没有可用助手文本，则返回自定义错误。
//
// 处理逻辑说明：
//  1. 循环调用 iter.Next()，直到迭代器关闭（ok == false）。
//  2. 跳过 nil 事件，避免空指针解引用。
//  3. 若事件自带 Err，立即中断并向上返回（同时带回已收集到的 last，便于排障）。
//  4. 仅当 Output → MessageOutput → Message 链路完整时才继续解析消息。
//  5. 只采纳 Role 为 Assistant 且 Content 去空白后非空的消息，覆盖写入 last，
//     这样多轮/流式场景下保留的是“最后一条有效助手回复”。
//  6. 迭代结束后若 last 仍为空，说明没有任何可用回答，返回明确错误。
func drainAgent(iter *adk.AsyncIterator[*adk.AgentEvent]) (string, error) {
	// last 保存目前为止看到的最后一条有效助手文本；初始为空字符串。
	var last string

	// 持续拉取事件，直到迭代器结束。
	for {
		// Next 返回下一个事件，以及是否还有数据（ok）。
		// ok == false 表示迭代器已关闭，没有更多事件。
		ev, ok := iter.Next()
		if !ok {
			// 迭代器耗尽，跳出循环，进入最终校验。
			break
		}

		// 防御性检查：个别实现可能产出 nil 事件，直接跳过。
		if ev == nil {
			continue
		}

		// 事件级错误优先处理：一旦出现，立即返回，不再继续消费后续事件。
		// 同时返回已收集的 last，方便调用方在失败时仍能看到部分输出。
		if ev.Err != nil {
			return last, ev.Err
		}

		// 消息链路可能不完整（例如仅有工具调用、状态更新等非文本事件）。
		// 任一环节为 nil 都表示本事件不含可展示的聊天消息，跳过即可。
		if ev.Output == nil || ev.Output.MessageOutput == nil || ev.Output.MessageOutput.Message == nil {
			continue
		}

		// 取出具体的 schema.Message（含 Role、Content 等字段）。
		msg := ev.Output.MessageOutput.Message

		// 只关心助手角色的非空正文：
		//   - Role 必须是 schema.Assistant（过滤用户/系统/工具消息）
		//   - Content 去掉首尾空白后仍非空（过滤纯空白占位）
		// 满足条件则覆盖 last，保证最终拿到的是“最后一条”有效回答。
		if msg.Role == schema.Assistant && strings.TrimSpace(msg.Content) != "" {
			last = msg.Content
		}
	}

	// 全部事件消费完毕后，若从未拿到有效助手文本，视为异常结果。
	if last == "" {
		return "", fmt.Errorf("no assistant text in agent events")
	}

	// 正常路径：返回最后一条助手回答，错误为 nil。
	return last, nil
}
