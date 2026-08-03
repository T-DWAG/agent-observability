package storage

import (
	"context"
	"sync"
	"time"

	"github.com/T-Dwag/agent-observability/model"
)

type MemoryStorage struct {
	mu              sync.RWMutex
	Traces          []*model.Trace
	Spans           []*model.Span
	Evaluations     []*model.Evaluation
	MetricSnapshots []*model.MetricSnapshot
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{}
}

func (m *MemoryStorage) SaveSpan(_ context.Context, span *model.Span) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Spans = append(m.Spans, span)
	return nil
}

func (m *MemoryStorage) SaveTrace(_ context.Context, trace *model.Trace) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Traces = append(m.Traces, trace)
	return nil
}

func (m *MemoryStorage) GetTrace(_ context.Context, tenantID, traceID string) (*model.Trace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tenantID = normalizeTenantID(tenantID)
	for _, tr := range m.Traces {
		if tr.TraceID == traceID && normalizeTenantID(tr.TenantID) == tenantID {
			cp := *tr
			return &cp, nil
		}
	}
	return nil, ErrorNotFound
}

// GetTraceSpans：先校验 Trace 属于该租户，再按 traceID 取 Span。
// 注意：不要在持有 m.mu 时调用 GetTrace（会再次 RLock → 死锁）。
func (m *MemoryStorage) GetTraceSpans(ctx context.Context, tenantID, traceID string) ([]*model.Span, error) {
	if _, err := m.GetTrace(ctx, tenantID, traceID); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	output := make([]*model.Span, 0)
	for _, sp := range m.Spans {
		if sp.TraceID == traceID {
			cp := *sp
			output = append(output, &cp)
		}
	}
	return output, nil
}

func (m *MemoryStorage) ListTraces(_ context.Context, filter TraceFilter) ([]*model.Trace, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	filter = filter.normalize()
	tenantID := normalizeTenantID(filter.TenantID)
	matched := make([]*model.Trace, 0)

	for _, tr := range m.Traces {
		if normalizeTenantID(tr.TenantID) != tenantID {
			continue
		}
		if filter.SessionID != "" && tr.SessionID != filter.SessionID {
			continue
		}
		if filter.Status != "" && tr.Status != filter.Status {
			continue
		}
		if !filter.StartTime.IsZero() && tr.StartTime.Before(filter.StartTime) {
			continue
		}
		if !filter.EndTime.IsZero() && tr.StartTime.After(filter.EndTime) {
			continue
		}
		cp := *tr
		matched = append(matched, &cp)
	}

	total := int64(len(matched))
	start := filter.offset()
	if start >= int(total) {
		return []*model.Trace{}, total, nil
	}
	end := start + filter.Size
	if end > int(total) {
		end = len(matched)
	}
	return matched[start:end], total, nil
}

func (m *MemoryStorage) CreateEvaluationJob(_ context.Context, eval *model.Evaluation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.Evaluations {
		if existing.TraceID == eval.TraceID && existing.Dimension == "overall" {
			return ErrorEvaluationExists
		}
	}
	cp := *eval
	m.Evaluations = append(m.Evaluations, &cp)
	return nil
}

func (m *MemoryStorage) SaveEvaluation(_ context.Context, eval *model.Evaluation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.Evaluations {
		if existing.TraceID == eval.TraceID && existing.Dimension == eval.Dimension {
			return ErrorEvaluationExists
		}
	}
	cp := *eval
	m.Evaluations = append(m.Evaluations, &cp)
	return nil
}

func (m *MemoryStorage) ListEvaluations(_ context.Context, traceID string) ([]*model.Evaluation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*model.Evaluation, 0)
	for _, e := range m.Evaluations {
		if e.TraceID == traceID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out, nil
}

// PurgeTrace 按 tenantID + traceID 删除单条 Trace，并级联清理其 Span。
//
// 语法要点：
//   - kept := m.Traces[:0] 复用底层数组做原地过滤，避免额外分配。
//   - normalizeTenantID 统一空租户语义，保证与写入侧一致。
//
// 逻辑：
//  1. 加写锁，保证与并发读写互斥。
//  2. 仅删除「TraceID 匹配且租户匹配」的 Trace。
//  3. 再按 TraceID 删除对应 Span（Span 本身无租户字段，依赖 Trace 归属）。
func (m *MemoryStorage) PurgeTrace(_ context.Context, tenantID, traceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tenantID = normalizeTenantID(tenantID)

	kept := m.Traces[:0]
	removed := false
	for _, tr := range m.Traces {
		if tr.TraceID == traceID && normalizeTenantID(tr.TenantID) == tenantID {
			removed = true
			continue
		}
		kept = append(kept, tr)
	}
	m.Traces = kept
	if !removed {
		return nil
	}

	keptSp := m.Spans[:0]
	for _, sp := range m.Spans {
		if sp.TraceID == traceID {
			continue
		}
		keptSp = append(keptSp, sp)
	}
	m.Spans = keptSp
	keptEval := m.Evaluations[:0]
	for _, eval := range m.Evaluations {
		if eval.TraceID != traceID {
			keptEval = append(keptEval, eval)
		}
	}
	m.Evaluations = keptEval
	return nil
}

// PurgeBefore 删除指定租户在 before 之前（StartTime < before）的全部 Trace，并级联清理 Span。
//
// 语法要点：
//   - drop 用 map[string]struct{} 记录待删 TraceID，O(1) 判断 Span 是否级联删除。
//   - kept := slice[:0] 同样是原地压缩，保留未命中记录。
//
// 逻辑：
//  1. 加写锁后规范化 tenantID。
//  2. 遍历 Trace：租户匹配且 StartTime.Before(before) 则计入 drop，并累加删除数 n。
//  3. 遍历 Span：TraceID 落在 drop 中则丢弃，否则保留。
//  4. 返回删除的 Trace 数量；本实现无失败路径，error 恒为 nil。
func (m *MemoryStorage) PurgeBefore(_ context.Context, tenantID string, before time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tenantID = normalizeTenantID(tenantID)

	drop := map[string]struct{}{}
	kept := m.Traces[:0]
	var n int64
	for _, tr := range m.Traces {
		if normalizeTenantID(tr.TenantID) == tenantID && tr.StartTime.Before(before) {
			drop[tr.TraceID] = struct{}{}
			n++
			continue
		}
		kept = append(kept, tr)
	}
	m.Traces = kept

	keptSp := m.Spans[:0]
	for _, sp := range m.Spans {
		if _, ok := drop[sp.TraceID]; ok {
			continue
		}
		keptSp = append(keptSp, sp)
	}
	m.Spans = keptSp
	keptEval := m.Evaluations[:0]
	for _, eval := range m.Evaluations {
		if _, ok := drop[eval.TraceID]; !ok {
			keptEval = append(keptEval, eval)
		}
	}
	m.Evaluations = keptEval
	return n, nil
}

func (m *MemoryStorage) UpdateEvaluationStatus(_ context.Context, traceID, status, errorMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	updated := false
	for _, e := range m.Evaluations {
		if e.TraceID == traceID && e.Dimension == "overall" {
			e.Status = status
			e.ErrorMsg = errorMsg
			updated = true
		}
	}
	if !updated {
		return ErrorNotFound
	}
	return nil
}

func (m *MemoryStorage) SaveMetricSnapshot(_ context.Context, snapshot *model.MetricSnapshot) error {
	//s
	m.mu.Lock()
	defer m.mu.Unlock()

	tenantID := normalizeTenantID(snapshot.TenantID)
	for i, existing := range m.MetricSnapshots {
		// 匹配：若已有快照的 TenantID（标准化后）和 Scope 与待存 snapshot 完全一致
		if normalizeTenantID(existing.TenantID) == tenantID && existing.Scope == snapshot.Scope {
			// 更新：直接修改已有快照的值
			cp := *snapshot
			cp.TenantID = tenantID
			m.MetricSnapshots[i] = &cp
			return nil
		}
	}

	// 新增：若无匹配快照，则创建新快照
	cp := *snapshot
	cp.TenantID = tenantID
	m.MetricSnapshots = append(m.MetricSnapshots, &cp)
	return nil
}

// GetMetricSnapshot 方法：根据标准化后的 tenantID 和 scope 查询内存中的 MetricSnapshot。
// 逻辑：
// 1. 加读锁，保证并发安全；
// 2. 标准化传入的 tenantID；
// 3. 遍历已有快照，若某快照的（已标准化）TenantID 和 Scope 与入参完全一致，则复制一份并返回；
// 4. 若未找到，则返回 ErrorNotFound。
func (m *MemoryStorage) GetMetricSnapshot(
	_ context.Context,
	tenantID, scope string,
) (*model.MetricSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tenantID = normalizeTenantID(tenantID)
	for _, snapshot := range m.MetricSnapshots {
		// 判断已保存的快照是否与目标 tenantID 和 scope 完全匹配
		if normalizeTenantID(snapshot.TenantID) == tenantID && snapshot.Scope == scope {
			cp := *snapshot // 解引用复制，cp 是新对象；即便返回指针，指向的是副本，外部无法影响底层切片内容
			return &cp, nil
		}
	}
	// 未找到匹配快照
	return nil, ErrorNotFound
}
