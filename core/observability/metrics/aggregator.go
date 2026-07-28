package metrics

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

// cacheEntry 用于缓存聚合快照及其过期时间
type cacheEntry struct {
	snap      *Snapshot
	expiresAt time.Time
}

// Aggregator 基于 Storage 做时间窗聚合；带进程内短缓存。
// store: 存储后端实现
// cacheTTL: 缓存存活时长
// mu: 互斥锁保护 cache 并发
// cache: 按 scope 存储聚合快照和到期时间
type Aggregator struct {
	store    storage.Storage
	cacheTTL time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewAggregator 创建 Aggregator 实例，参数校验 cacheTTL 合理性
func NewAggregator(store storage.Storage, cacheTTL time.Duration) *Aggregator {
	if cacheTTL <= 0 {
		cacheTTL = 30 * time.Second
	}
	return &Aggregator{
		store:    store,
		cacheTTL: cacheTTL,
		cache:    make(map[string]cacheEntry),
	}
}

// Aggregate 按 scope 聚合当窗指标；有缓存优先走缓存，否则实时聚合
// now 便于单测注入“当前时间”
func (a *Aggregator) Aggregate(ctx context.Context, tenantID, scope string, now time.Time) (*Snapshot, error) {
	// 1. 计算 scope 对应的时间窗口
	from, to, err := windowForScope(scope, now)
	if err != nil {
		return nil, err
	}

	cacheKey := tenantID + "|" + scope

	// 2. 优先返回未过期缓存（按租户隔离，避免串缓存）
	a.mu.Lock()
	if e, ok := a.cache[cacheKey]; ok && now.Before(e.expiresAt) {
		snap := *e.snap // 浅拷贝快照副本
		a.mu.Unlock()
		return &snap, nil
	}
	a.mu.Unlock()

	// 3. 全量查询窗口内 trace 数据（强制租户）
	traces, err := listAllTraces(ctx, a.store, storage.TraceFilter{
		TenantID:  tenantID,
		StartTime: from,
		EndTime:   to,
	})
	if err != nil {
		return nil, err
	}

	// 4. 初始化聚合快照
	snap := &Snapshot{
		Scope: scope,
		From:  from,
		To:    to,
	}
	var durSum int64                // 总耗时累计
	var success int64               // 成功次数
	toolCount := map[string]int64{} // 工具调用次数统计

	// 5. 遍历 traces 聚合总指标
	for _, tr := range traces {
		snap.TotalTraces++
		snap.TotalTokens += tr.TotalTokens
		snap.TotalCost += tr.TotalCost
		durSum += tr.DurationMs
		if tr.Status == model.SpanStatusSuccess {
			success++
		}

		// 6. 获取该 trace 的所有 span，统计工具调用量
		spans, err := a.store.GetTraceSpans(ctx, tenantID, tr.TraceID)
		if err != nil {
			return nil, fmt.Errorf("spans %s: %w", tr.TraceID, err)
		}
		for _, sp := range spans {
			if sp.SpanType == model.SpanTypeTool && sp.ToolName != "" {
				toolCount[sp.ToolName]++
			}
		}
	}

	// 7. 计算平均耗时与成功率
	if snap.TotalTraces > 0 {
		snap.AvgDurationMs = float64(durSum) / float64(snap.TotalTraces)
		snap.SuccessRate = float64(success) / float64(snap.TotalTraces)
	}
	// 8. 挑选 top-N 工具使用次数
	snap.TopTools = topNTools(toolCount, 5)

	// 9. 缓存聚合结果
	a.mu.Lock()
	a.cache[cacheKey] = cacheEntry{snap: snap, expiresAt: now.Add(a.cacheTTL)}
	a.mu.Unlock()

	// 10. 返回聚合快照副本
	out := *snap
	return &out, nil
}

// windowForScope 根据 scope 名称计算窗口区间
func windowForScope(scope string, now time.Time) (from, to time.Time, err error) {
	to = now.UTC()
	switch scope {
	case ScopeLast24h:
		from = to.Add(-24 * time.Hour)
	case ScopeLast7d:
		from = to.Add(-7 * 24 * time.Hour)
	case ScopeLast30d:
		from = to.Add(-30 * 24 * time.Hour)
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("invalid scope %q", scope)
	}
	return from, to, nil
}

// listAllTraces 分页遍历所有结果，汇集至 all
func listAllTraces(ctx context.Context, store storage.Storage, filter storage.TraceFilter) ([]*model.Trace, error) {
	filter.Page = 1
	filter.Size = 100
	var all []*model.Trace
	for {
		items, total, err := store.ListTraces(ctx, filter)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if int64(len(all)) >= total || len(items) == 0 {
			break
		}
		filter.Page++
	}
	return all, nil
}

// topNTools 按使用次数降序选出前 n 个工具
func topNTools(m map[string]int64, n int) []ToolStat {
	// topNTools 函数的作用是：从一组工具的调用次数统计中，挑选出使用次数最多的前 n 个工具，并按调用次数从高到低排序。
	// 输入参数 m 是一个 map，key 为工具名称，value 为该工具的调用次数。
	// 返回值是一个长度不超过 n 的 ToolStat 切片，按调用次数降序（多到少）排列。

	// 第一步：把工具名和调用次数从 map 结构转为 ToolStat 结构体切片，便于后续排序和截取
	out := make([]ToolStat, 0, len(m))
	for name, c := range m {
		// 将每个工具的名称和调用次数装入 ToolStat，并加入切片
		out = append(out, ToolStat{Name: name, Count: c})
	}

	// 第二步：按调用次数降序排列，调用次数相同时，按名称升序排列，保证结果稳定、可重复
	// 这里参考 sort.Slice 的实现，相当于用 less(i, j) 判断 out[i] 是否应该排在 out[j] 前面
	// https://cs.opensource.google/go/go/+/refs/tags/go1.21.0:src/sort/slice.go;l=22
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			// 调用次数相同时，按工具名称字典序升序排列
			return out[i].Name < out[j].Name
		}
		// 调用次数多的排前面
		return out[i].Count > out[j].Count
	})

	// 第三步：如果工具总数超过 n，则只保留前 n 名，多余的截断掉
	if len(out) > n {
		out = out[:n]
	}

	// 最终返回的数据即为调用次数最多的前 n 个工具及其调用次数
	return out
}
