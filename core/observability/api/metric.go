package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/T-Dwag/agent-observability/metrics"
)

func (s *Server) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	// 如果聚合器尚未配置，则返回 503 错误
	if s.agg == nil {
		writeError(w, http.StatusServiceUnavailable, "metrics not configured")
		return
	}
	// 从 query 参数中获取 scope，若未指定则默认使用 ScopeLast24h
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = metrics.ScopeLast24h
	}
	scope = strings.TrimSpace(scope)

	tenant := requestTenant(r)
	// 调用聚合器，根据指定 scope 和当前时间聚合数据
	snap, err := s.agg.Aggregate(r.Context(), tenant, scope, time.Now().UTC())
	if err != nil {
		// 如果报错原因是 scope 不合法，返回 400 错误
		if strings.Contains(err.Error(), "invalid scope") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// 其他错误返回 500
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 聚合成功，返回聚合数据
	writeJSON(w, http.StatusOK, snap)
}
