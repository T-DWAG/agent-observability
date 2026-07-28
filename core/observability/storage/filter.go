package storage

import "time"

type TraceFilter struct {
	TenantID  string // List 必填（API 层从 Key 注入；空则调用方应先 normalize）
	SessionID string
	Status    string
	Page      int
	Size      int
	StartTime time.Time
	EndTime   time.Time
}

func (f TraceFilter) normalize() TraceFilter {
	f.TenantID = normalizeTenantID(f.TenantID)
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.Size <= 0 {
		f.Size = 20
	}
	if f.Size > 100 {
		f.Size = 100
	}
	return f
}

func (f TraceFilter) offset() int {
	return (f.Page - 1) * f.Size
}
