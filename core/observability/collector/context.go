package collector

import (
	"context"

	"github.com/T-Dwag/agent-observability/storage"
	"github.com/cloudwego/eino/callbacks"
)

// ctxKey 是把 *State 挂到 context 上的私有键。
// OnStart / OnEnd 是两次独立调用，靠 ctx 里的 State（及其中的 spanID）串联同一次执行。
type ctxKey struct{}

// withState 将采集状态写入 ctx，供后续 Callback 与 TraceIDFromCtx 读取。
func withState(ctx context.Context, s *State) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// Config 创建 Trace 时的业务上下文（会话与用户输入）。
type Config struct {
	SessionID string
	UserInput string
}

// WithObsCallback 为一次 Agent 执行准备采集器。
//
// 逻辑：
//  1. newState：创建内存中的 Trace/Span 状态，绑定 Storage
//  2. runWorker：后台消费落盘任务（与 Eino 回调解耦，避免阻塞执行路径）
//  3. NewHandler：生成传给 adk.WithCallbacks 的 Handler（OnStart 建 Span，OnEnd 补全并 finish）
//  4. withState：把 State 写入 ctx，供回调与 TraceIDFromCtx 使用
//  5. finish：Agent 跑完后由调用方显式调用——收尾 Trace、取消 worker、等待落盘完成
//
// 返回 ctx、handler（传给 adk.WithCallbacks）、finish（Agent 结束后调用）。
func WithObsCallback(
	ctx context.Context,
	store storage.Storage,
	cfg Config,
) (context.Context, callbacks.Handler, func(context.Context, string, error)) {
	// 一次执行对应一份 State（内含 Trace 与进行中的 Span）
	state := newState(store, cfg.SessionID, cfg.UserInput)

	// 独立 Background，避免业务 ctx 取消后 worker 提前退出导致未落盘
	workerCtx, cancel := context.WithCancel(context.Background())
	go state.runWorker(workerCtx)

	handler := NewHandler(state)
	ctx = withState(ctx, state)

	// finish：非 Eino 回调；在 Agent 拿到最终结果后调用，更新 obs_traces 并收尾 worker
	finish := func(_ context.Context, output string, runErr error) {
		state.finishTrace(output, runErr)
		state.waitDone() // 先等 worker 落盘
		cancel()         // 再取消 worker ctx
	}
	return ctx, handler, finish
}

// TraceIDFromCtx 从 context 读取当前 TraceID。
// 若未经过 WithObsCallback 注入 State，返回空字符串。
func TraceIDFromCtx(ctx context.Context) string {
	if s, ok := ctx.Value(ctxKey{}).(*State); ok && s != nil {
		return s.Trace.TraceID
	}
	return ""
}
