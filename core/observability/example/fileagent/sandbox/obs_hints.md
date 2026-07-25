# 观测排查速查

1. Trace 查不到：确认 Runner 调用了 adk.WithCallbacks(handler)。
2. 只有 Agent Span 没有 Tool：模型可能没按 Instruction 调工具，检查工具描述。
3. 正文裸奔：打开 collector.Config.Redact 或 NoContent。
4. Span 丢了：看 Stats.DroppedSpans（专题 0）。
