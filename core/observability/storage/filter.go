package storage

import "time"

type TraceFilter struct {
	SessionID string
	Status    string
	Page      int
	Size      int
	StartTime time.Time
	EndTime   time.Time
}

func (f TraceFilter) normalize() TraceFilter {
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
