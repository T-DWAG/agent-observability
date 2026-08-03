package metrics

import (
	"context"
	"log"
	"time"
)

// RunRefresher 按固定间隔周期性刷新各租户的指标快照。
//
// 入参：
//   - ctx：控制刷新循环生命周期，取消后函数退出
//   - aggregator：指标聚合器，负责按 scope 计算并落库快照
//   - tenantIDs：需要刷新的租户 ID 列表
//   - interval：两次全量刷新之间的时间间隔
//
// 出参：无；函数阻塞运行直至 ctx 被取消。
func RunRefresher(
	ctx context.Context,
	aggregator *Aggregator,
	tenantIDs []string,
	interval time.Duration,
) {
	// refreshAll：对所有租户执行一次全量刷新；单租户失败仅打日志，不中断其余租户。
	refreshAll := func() {
		now := time.Now().UTC()
		for _, tenantID := range tenantIDs {
			if err := aggregator.Refresh(ctx, tenantID, now); err != nil {
				log.Printf("[obs] metrics refresh failed tenant=%s: %v", tenantID, err)
			}
		}
	}

	// 启动时先立即刷新一次，避免等待首个 ticker 周期才有数据。
	refreshAll()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 主循环：ctx 取消则退出；ticker 触发则再次全量刷新。
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshAll()
		}
	}
}
