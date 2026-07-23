package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

// TestAggregate_Last24h 用于单元测试聚合器 Aggregate 在 ScopeLast24h（即当前时刻往前推24小时窗口）的正确性。
// “窗内”表示 trace 的 StartTime 落在 [now-24h, now] 这个时间段内，属于当前聚合窗口，应被聚合统计；
// “窗外”表示 StartTime 超出了该时间区间的 trace，不应被计入本次聚合。
// 本测试同时设计了“成功(trace.Status=success)”和“失败(trace.Status=error)”样本，是为了检查聚合中的成功率 (success_rate) 是否统计正确。
func TestAggregate_Last24h(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	// 新增一条“窗内成功”trace:
	// - StartTime 为 now-2小时，落在[now-24h,now]内，归入当前窗口（窗内）
	// - Status=SpanStatusSuccess，代表一次成功调用，便于后续校验 success_rate
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t1", StartTime: now.Add(-2 * time.Hour), EndTime: now.Add(-2*time.Hour + time.Second),
		Status: model.SpanStatusSuccess, TotalTokens: 100, TotalCost: 0.01, DurationMs: 1000,
	})
	// 新增一条“窗内失败”trace:
	// - StartTime 为 now-1小时，也在[now-24h,now]内（窗内）
	// - Status=SpanStatusError，模拟一次失败调用，用于覆盖异常路径测试和计算成功率
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t2", StartTime: now.Add(-1 * time.Hour), EndTime: now.Add(-1*time.Hour + time.Second),
		Status: model.SpanStatusError, TotalTokens: 50, TotalCost: 0.02, DurationMs: 2000,
	})
	// 新增一条“窗外成功”trace:
	// - StartTime 为 now-48小时，早于当前聚合窗口（窗外），主要用于验证是否被准确排除
	// - 尽管这条trace也是成功样本，但并不会影响当前窗口统计
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t-old", StartTime: now.Add(-48 * time.Hour),
		Status: model.SpanStatusSuccess, TotalTokens: 999, DurationMs: 1,
	})

	// Tool类Span。用于 TopTools 统计，其中 s1, s2 属于 t1（成功窗内），s3 属于 t2（失败窗内）。
	_ = store.SaveSpan(ctx, &model.Span{
		SpanID: "s1", TraceID: "t1", SpanType: model.SpanTypeTool, ToolName: "get_weather",
		StartTime: now.Add(-2 * time.Hour), Status: model.SpanStatusSuccess,
	})
	_ = store.SaveSpan(ctx, &model.Span{
		SpanID: "s2", TraceID: "t1", SpanType: model.SpanTypeTool, ToolName: "get_weather",
		StartTime: now.Add(-2 * time.Hour), Status: model.SpanStatusSuccess,
	})
	_ = store.SaveSpan(ctx, &model.Span{
		SpanID: "s3", TraceID: "t2", SpanType: model.SpanTypeTool, ToolName: "search",
		StartTime: now.Add(-1 * time.Hour), Status: model.SpanStatusSuccess,
	})

	agg := NewAggregator(store, time.Minute)
	snap, err := agg.Aggregate(ctx, ScopeLast24h, now)
	if err != nil {
		t.Fatal(err)
	}

	// 验证聚合结果
	// - 应只统计“窗内”数据，t1和t2
	// - t1是成功，t2是失败，success_rate 应为 0.5
	if snap.TotalTraces != 2 { // 只聚合 t1 + t2
		t.Fatalf("traces=%d", snap.TotalTraces)
	}
	if snap.TotalTokens != 150 { // t1:100 + t2:50
		t.Fatalf("tokens=%d", snap.TotalTokens)
	}
	if snap.SuccessRate != 0.5 { // 1/2 = 0.5
		t.Fatalf("success_rate=%v", snap.SuccessRate)
	}
	if snap.AvgDurationMs != 1500 { // (1000+2000)/2
		t.Fatalf("avg=%v", snap.AvgDurationMs)
	}
	// Tool调用统计：get_weather 在t1里有两条(s1,s2)，应为TopTool 且计数为2
	if len(snap.TopTools) < 1 || snap.TopTools[0].Name != "get_weather" || snap.TopTools[0].Count != 2 {
		t.Fatalf("top_tools=%+v", snap.TopTools)
	}
}

// 测试聚合接口遇到不受支持的scope时，是否能正确返回错误
func TestAggregate_InvalidScope(t *testing.T) {
	agg := NewAggregator(storage.NewMemoryStorage(), 0)
	_, err := agg.Aggregate(context.Background(), "yesterday", time.Now())
	if err == nil {
		t.Fatal("want error")
	}
}

// 测试缓存（cache）命中场景：
// 第一次聚合后（有1个窗内trace），紧接着插入新trace（仍然窗内），但是再次
// 查询时还在缓存有效期内，聚合结果里仍应只有1条（即未立刻命中新插入的trace）
func TestAggregate_CacheHit(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()

	// 初始写入1条“窗内成功”trace
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t1", StartTime: now.Add(-time.Hour),
		Status: model.SpanStatusSuccess, DurationMs: 10,
	})

	agg := NewAggregator(store, time.Minute) // 设置1分钟缓存
	s1, err := agg.Aggregate(ctx, ScopeLast24h, now)
	if err != nil {
		t.Fatal(err)
	}

	// 再写入第2条“窗内成功”trace
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "t2", StartTime: now.Add(-30 * time.Minute),
		Status: model.SpanStatusSuccess, DurationMs: 10,
	})

	// 因为距离上一次聚合仅过一秒，缓存尚未失效，应仍然命中缓存，结果未变
	s2, err := agg.Aggregate(ctx, ScopeLast24h, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if s1.TotalTraces != s2.TotalTraces {
		t.Fatalf("cache miss? %d vs %d", s1.TotalTraces, s2.TotalTraces)
	}
}
