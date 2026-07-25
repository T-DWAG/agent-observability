// Package collector 的 State 负责一次 Agent 执行周期内的内存态：
// Trace 汇总、Span 嵌套栈、以及通过 channel 异步交给 Storage 落盘。
//
// 生命周期（与 handler.go / context.go 配合）：
//
//	WithObsCallback → newState → runWorker(后台)
//	    → Handler OnStart  → startSpan  (入栈 pending)
//	    → Handler OnEnd    → finishSpan (出栈 → spanCh)
//	    → finish()         → finishTrace (close spanCh → traceCh → worker 退出)
package collector

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
	"github.com/google/uuid"
)

var spanChBuffer = 256 // span 通道缓冲；测试可改为 1 验证丢弃计数

// State 保存采集器运行时状态，管理当前 Trace、Span 栈与异步通道。
//
// 为什么需要 spanStack + pending 两套结构：
//   - spanStack：维护嵌套关系，算出 ParentSpanID（Agent 下挂 LLM/Tool）
//   - pending：OnStart 到 OnEnd 之间暂存未完成的 Span，按 spanID 配对结束回调
type State struct {
	storage   storage.Storage        // 持久化存储（Step2 用 Memory，Step3 换 PG）
	Trace     *model.Trace           // 当前活跃的 Trace，finishTrace 时汇总写入
	spanStack []string               // Span 嵌套栈（存 SpanID，LIFO）
	pending   map[string]*model.Span // 已开始、尚未 finish 的 Span
	cfg       Config                 // 采集器配置
	stats     *Stats                 // 采集器运行计数
	spanCh    chan *model.Span       // 已完成 Span → worker 异步 SaveSpan
	traceCh   chan *model.Trace      // 整次 Trace 完成 → worker SaveTrace 后退出
	done      chan struct{}          // worker 退出信号，finish() 里 waitDone 等待
}

// newState：只挂 cfg，UserInput 原样写入内存 Trace。
// 清空/打码一律留到 applyContentPolicy（落盘前），避免两处逻辑分叉。
func newState(store storage.Storage, cfg Config) *State {
	now := time.Now()
	return &State{
		storage: store,
		cfg:     cfg,
		stats:   &Stats{},
		Trace: &model.Trace{
			TraceID:   uuid.New().String(),
			SessionID: cfg.SessionID,
			UserInput: cfg.UserInput, // 原样；落盘前再过策略
			StartTime: now,
			Status:    model.SpanStatusPending,
			TenantID:  normalizeTenant(cfg.TenantID),
		},
		pending: make(map[string]*model.Span),
		spanCh:  make(chan *model.Span, spanChBuffer),
		traceCh: make(chan *model.Trace, 1),
		done:    make(chan struct{}),
	}
}

func normalizeTenant(id string) string {
	if id == "" {
		return "default"
	}
	return id
}

// runWorker 在独立 goroutine 中消费 channel，把落盘 IO 与 Agent 主路径解耦。
//
// 两阶段（不能把 spanCh 与 traceCh 放进同一个 select）：
//  1. 排空 spanCh（close 后 range 读尽缓冲）
//  2. 再收 traceCh → SaveTrace → 退出
//
// 若同 select 同时监听两者：spanCh 已 close 且缓冲未空时，trace 分支也可能就绪，
// select 可能先收 Trace 并 return，导致剩余 Span 丢失。
//
// 注意：Save 经有限重试后仍失败则计数并打日志，不影响 Agent 主流程。
func (s *State) runWorker(ctx context.Context) {
	defer close(s.done)

	// 阶段 1：排空全部 Span（close 后必须读尽缓冲再进阶段 2）
	for {
		select {
		case <-ctx.Done():
			return
		case span, ok := <-s.spanCh:
			if !ok {
				// 阶段 2：Span 全部落盘后再写 Trace
				select {
				case <-ctx.Done():
					return
				case trace, ok := <-s.traceCh:
					if !ok {
						return
					}
					s.saveTraceWithRetry(ctx, trace)
				}
				return
			}
			s.saveSpanWithRetry(ctx, span)
		}
	}
}

// parentSpanID 决定新 Span 挂在哪棵子树下。
//
// 典型嵌套：Agent OnStart 入栈 → LLM/Tool OnStart 时 parent=Agent 的 SpanID。
// 栈空返回 ""，表示当前 Span 是 Trace 的根（通常是 agent 类型）。
func (s *State) parentSpanID() string {
	if len(s.spanStack) == 0 {
		return ""
	}
	return s.spanStack[len(s.spanStack)-1]
}

// pushSpan 在 OnStart 时调用，与 finishSpan 里的 popSpan 成对。
func (s *State) pushSpan(spanID string) {
	s.spanStack = append(s.spanStack, spanID)
}

// popSpan 在 finishSpan 时调用，恢复父 Span 为栈顶，供后续兄弟 Span 使用同一 parent。
func (s *State) popSpan() {
	if len(s.spanStack) > 0 {
		s.spanStack = s.spanStack[:len(s.spanStack)-1]
	}
}

// startSpan 由 Handler 的 OnStart 触发：创建 Span、登记 pending、压栈。
// 返回 *Span 供 Handler 在同一次回调链中按需写 Reasoning/ToolInput 等字段。
func (s *State) startSpan(spanType, name string) *model.Span {
	span := &model.Span{
		SpanID:       uuid.New().String(),
		TraceID:      s.Trace.TraceID,
		ParentSpanID: s.parentSpanID(),
		SpanType:     spanType,
		SpanName:     name,
		StartTime:    time.Now(),
		Status:       model.SpanStatusPending,
	}

	s.pending[span.SpanID] = span
	s.pushSpan(span.SpanID)
	return span
}

// finishSpan 由 Handler 的 OnEnd/OnError 触发：补全时间、出栈、异步投递 spanCh。
//
// mutate 由调用方填入业务字段（token、tool 输出、error 等），再统一计算 DurationMs。
// 使用 select+default 非阻塞发送：channel 满则丢弃并打日志，避免死锁 Agent。
func (s *State) finishSpan(spanID string, mutate func(*model.Span)) {
	span, ok := s.pending[spanID]
	if !ok {
		// OnEnd 找不到对应 OnStart（例如 ctx 里 spanID 丢失），静默跳过
		return
	}
	if mutate != nil {
		mutate(span)
	}

	span.EndTime = time.Now()
	span.DurationMs = span.EndTime.Sub(span.StartTime).Milliseconds()
	delete(s.pending, spanID)
	s.popSpan()
	s.Trace.SpanCount++

	applyContentPolicy(s.cfg, nil, span)

	select {
	case s.spanCh <- span:
	default:
		s.stats.DroppedSpans.Add(1)
		log.Printf("[obs] span channel full, drop span=%s dropped=%d",
			spanID, s.stats.DroppedSpans.Load())
	}
}

// failSpan 是 finishSpan 的错误分支：标记 Status=error 并记录 ErrorMsg。
func (s *State) failSpan(spanID string, err error) {
	s.finishSpan(spanID, func(sp *model.Span) {
		sp.Status = model.SpanStatusError
		sp.ErrorMsg = err.Error()
	})
}

// addLLMTokens 在 LLM Span 完成时累加到 Trace 级汇总（列表页直接读 Trace，不必 SUM Span）。
func (s *State) addLLMTokens(_, _, total int64, cost float64) {
	s.Trace.TotalTokens += total
	s.Trace.TotalCost += cost
}

// finishTrace 在 Agent 整次执行结束后由 context.go 的 finish() 调用。
//
// 顺序很重要：
//  1. 补全 Trace 字段
//  2. close(spanCh) → worker 读完剩余 Span
//  3. 向 traceCh 发送 Trace → worker SaveTrace 后退出
func (s *State) finishTrace(output string, runErr error) {
	s.Trace.EndTime = time.Now()
	s.Trace.DurationMs = s.Trace.EndTime.Sub(s.Trace.StartTime).Milliseconds()
	s.Trace.AgentOutput = output
	if runErr != nil {
		s.Trace.Status = model.SpanStatusError
	} else {
		s.Trace.Status = model.SpanStatusSuccess
	}

	applyContentPolicy(s.cfg, s.Trace, nil)
	close(s.spanCh)
	s.traceCh <- s.Trace // 阻塞发送，配合 waitDone 保证 Trace 落盘
}

// waitDone 阻塞直到 runWorker 退出，保证 finish() 返回前数据已落盘（或已尝试落盘）。
func (s *State) waitDone() {
	<-s.done
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// Stats 供测试读取计数。
func (s *State) Stats() *Stats { return s.stats }

const saveMaxAttempts = 3

func (s *State) saveSpanWithRetry(ctx context.Context, span *model.Span) {
	var err error
	for i := 0; i < saveMaxAttempts; i++ {
		err = s.storage.SaveSpan(ctx, span)
		if err == nil {
			s.stats.SaveSpanOK.Add(1)
			return
		}
	}
	s.stats.SaveSpanFails.Add(1)
	log.Printf("[obs] save span failed after %d attempts: %v", saveMaxAttempts, err)
}

func (s *State) saveTraceWithRetry(ctx context.Context, trace *model.Trace) {
	var err error
	for i := 0; i < saveMaxAttempts; i++ {
		err = s.storage.SaveTrace(ctx, trace)
		if err == nil {
			s.stats.SaveTraceOK.Add(1)
			return
		}
	}
	s.stats.SaveTraceFails.Add(1)
	log.Printf("[obs] save trace failed after %d attempts: %v", saveMaxAttempts, err)
}
