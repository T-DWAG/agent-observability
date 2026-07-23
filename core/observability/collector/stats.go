package collector

import "sync/atomic"

// Stats 采集器运行计数（进程内，供测试与以后挂 /metrics）。
// 零值可用；用 atomic 避免与 worker / Handler 竞态。
type Stats struct {
	DroppedSpans   atomic.Int64 // 因队列满等原因丢弃的 Span 数
	SaveSpanFails  atomic.Int64 // 持久化 Span 失败次数
	SaveTraceFails atomic.Int64 // 持久化 Trace 失败次数
	SaveSpanOK     atomic.Int64 // 持久化 Span 成功次数
	SaveTraceOK    atomic.Int64 // 持久化 Trace 成功次数
}

// snapshot 原子读取当前各计数快照，供测试断言或 /metrics 导出。
func (s *Stats) snapshot() (dropped, spanFail, traceFail, spanOK, traceOK int64) {
	return s.DroppedSpans.Load(),
		s.SaveSpanFails.Load(),
		s.SaveTraceFails.Load(),
		s.SaveSpanOK.Load(),
		s.SaveTraceOK.Load()
}
