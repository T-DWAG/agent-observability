package storage

import (
	"context"
	"sync"

	"github.com/T-Dwag/agent-observability/model"
)

type MemoryStorage struct {
	mu          sync.RWMutex
	Traces      []*model.Trace
	Spans       []*model.Span
	Evaluations []*model.Evaluation
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

func (m *MemoryStorage) SaveEvaluation(_ context.Context, eval *model.Evaluation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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
