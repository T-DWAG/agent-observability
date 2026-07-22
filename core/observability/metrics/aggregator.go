package metrics

import (
	"sync"
	"time"

	"github.com/T-Dwag/agent-observability/storage"
)

type cacheEntry struct {
	snap      *Snapshot // 缓存的聚合快照
	expiresAt time.Time // 缓存过期时间
}

// Aggregator 基于 Storage 做时间窗聚合；带进程内短缓存。
type Aggregator struct {
	store    storage.Storage // 底层指标存储
	cacheTTL time.Duration   // 进程内缓存 TTL

	mu    sync.Mutex            // 保护 cache 的互斥锁
	cache map[string]cacheEntry // 聚合快照短缓存
}
