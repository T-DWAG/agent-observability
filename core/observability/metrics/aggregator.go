package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

var SupportedScopes = []string{
	ScopeLast24h,
	ScopeLast7d,
	ScopeLast30d,
}

type Aggregator struct {
	store storage.Storage
}

func NewAggregator(store storage.Storage) *Aggregator {
	return &Aggregator{store: store}
}

// Aggregate 是 API 读路径：优先 O(1) 读取持久化快照。
// 首次尚无快照时，同步刷新一次作为冷启动兜底。
func (a *Aggregator) Aggregate(
	ctx context.Context,
	tenantID, scope string,
	now time.Time,
) (*Snapshot, error) {
	if _, _, err := windowForScope(scope, now); err != nil {
		return nil, err
	}

	saved, err := a.store.GetMetricSnapshot(ctx, tenantID, scope)
	if err == nil {
		return snapshotFromModel(saved)
	}
	if !errors.Is(err, storage.ErrorNotFound) {
		return nil, err
	}
	return a.refreshScope(ctx, tenantID, scope, now)
}

// Refresh 重算并覆盖一个租户的全部固定窗口。
func (a *Aggregator) Refresh(ctx context.Context, tenantID string, now time.Time) error {
	for _, scope := range SupportedScopes {
		if _, err := a.refreshScope(ctx, tenantID, scope, now); err != nil {
			return fmt.Errorf("refresh %s: %w", scope, err)
		}
	}
	return nil
}

func (a *Aggregator) refreshScope(
	ctx context.Context,
	tenantID, scope string,
	now time.Time,
) (*Snapshot, error) {
	snapshot, err := a.calculate(ctx, tenantID, scope, now)
	if err != nil {
		return nil, err
	}
	record, err := snapshotToModel(tenantID, snapshot)
	if err != nil {
		return nil, err
	}
	if err := a.store.SaveMetricSnapshot(ctx, record); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// calculate 是后台计算路径：扫描真实 Trace/Span，产出权威快照。
func (a *Aggregator) calculate(
	ctx context.Context,
	tenantID, scope string,
	now time.Time,
) (*Snapshot, error) {
	from, to, err := windowForScope(scope, now)
	if err != nil {
		return nil, err
	}
	traces, err := listAllTraces(ctx, a.store, storage.TraceFilter{
		TenantID:  tenantID,
		StartTime: from,
		EndTime:   to,
	})
	if err != nil {
		return nil, err
	}

	snapshot := &Snapshot{
		Scope:       scope,
		From:        from,
		To:          to,
		RefreshedAt: now.UTC(),
	}
	var durationSum int64
	var successCount int64
	toolCounts := map[string]int64{}

	for _, trace := range traces {
		snapshot.TotalTraces++
		snapshot.TotalTokens += trace.TotalTokens
		snapshot.TotalCost += trace.TotalCost
		durationSum += trace.DurationMs
		if trace.Status == model.SpanStatusSuccess {
			successCount++
		}

		spans, err := a.store.GetTraceSpans(ctx, tenantID, trace.TraceID)
		if err != nil {
			return nil, fmt.Errorf("spans %s: %w", trace.TraceID, err)
		}
		for _, span := range spans {
			if span.SpanType == model.SpanTypeTool && span.ToolName != "" {
				toolCounts[span.ToolName]++
			}
		}
	}

	if snapshot.TotalTraces > 0 {
		snapshot.AvgDurationMs = float64(durationSum) / float64(snapshot.TotalTraces)
		snapshot.SuccessRate = float64(successCount) / float64(snapshot.TotalTraces)
	}
	snapshot.TopTools = topNTools(toolCounts, 5)
	return snapshot, nil
}

func snapshotToModel(tenantID string, snapshot *Snapshot) (*model.MetricSnapshot, error) {
	raw, err := json.Marshal(snapshot.TopTools)
	if err != nil {
		return nil, fmt.Errorf("marshal top tools: %w", err)
	}
	return &model.MetricSnapshot{
		TenantID:      tenantID,
		Scope:         snapshot.Scope,
		WindowFrom:    snapshot.From,
		WindowTo:      snapshot.To,
		TotalTraces:   snapshot.TotalTraces,
		TotalTokens:   snapshot.TotalTokens,
		TotalCost:     snapshot.TotalCost,
		AvgDurationMs: snapshot.AvgDurationMs,
		SuccessRate:   snapshot.SuccessRate,
		TopToolsJSON:  string(raw),
		RefreshedAt:   snapshot.RefreshedAt,
	}, nil
}

func snapshotFromModel(saved *model.MetricSnapshot) (*Snapshot, error) {
	tools := make([]ToolStat, 0)
	if err := json.Unmarshal([]byte(saved.TopToolsJSON), &tools); err != nil {
		return nil, fmt.Errorf("unmarshal top tools: %w", err)
	}
	return &Snapshot{
		Scope:         saved.Scope,
		From:          saved.WindowFrom,
		To:            saved.WindowTo,
		TotalTraces:   saved.TotalTraces,
		TotalTokens:   saved.TotalTokens,
		TotalCost:     saved.TotalCost,
		AvgDurationMs: saved.AvgDurationMs,
		SuccessRate:   saved.SuccessRate,
		TopTools:      tools,
		RefreshedAt:   saved.RefreshedAt,
	}, nil
}

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

func listAllTraces(
	ctx context.Context,
	store storage.Storage,
	filter storage.TraceFilter,
) ([]*model.Trace, error) {
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
			return all, nil
		}
		filter.Page++
	}
}

func topNTools(counts map[string]int64, n int) []ToolStat {
	out := make([]ToolStat, 0, len(counts))
	for name, count := range counts {
		out = append(out, ToolStat{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Name < out[j].Name
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
