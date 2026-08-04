package api

import (
	"errors"
	"net/http"

	otelsink "github.com/T-Dwag/agent-observability/exporter/otel"
	"github.com/T-Dwag/agent-observability/storage"
)

// handleExportTrace 处理 OpenTelemetry Trace 导出接口请求。
// 路径形如 /api/trace/{id}/export。流程：
// 1. 检查 otel 导出器是否已配置，如未配置返回 503。
// 2. 从请求中提取租户 ID 与 trace ID（traceID 从 URL 路径参数获取）。
// 3. 校验 traceID 是否为空，若为空返回 400。
// 4. 调用导出逻辑，并根据结果返回：
//   - 导出成功返回 200 及 trace 信息；
//   - 如 trace 不存在，返回 404；
//   - 其它任何导出异常，返回 502。
func (s *Server) handleExportTrace(w http.ResponseWriter, r *http.Request) {
	// 步骤 1：确保 OTEL 导出器已配置
	if s.otel == nil {
		writeError(w, http.StatusServiceUnavailable, "otel exporter not configured") // 503: 服务不可用
		return
	}

	// 步骤 2：解析租户 ID 与 trace ID
	tenantID := requestTenant(r) // 租户通常来自请求 Header/上下文
	traceID := r.PathValue("id") // traceID 从 URL 路径变量获取（如 /trace/{id}/export）
	if traceID == "" {
		writeError(w, http.StatusBadRequest, "trace id is required") // 400: traceID 缺失
		return
	}

	// 步骤 3：调用导出逻辑并根据错误类型进行返回
	if err := s.otel.ExportTrace(r.Context(), tenantID, traceID); err != nil {
		if errors.Is(err, storage.ErrorNotFound) {
			writeError(w, http.StatusNotFound, "trace not found") // 404: trace 不存在
			return
		}
		writeError(w, http.StatusBadGateway, err.Error()) // 502: 其它导出相关错误（如下游 collector 不可用）
		return
	}

	// 步骤 4：成功导出时返回 200 及 JSON 响应
	writeJSON(w, http.StatusOK, map[string]string{
		"trace_id": traceID,
		"status":   "exported",
	})
}

// 静态引用 otel.Config，保证类型被编译器引用，可用于代码静态分析或防止被误删。
var _ = otelsink.Config{}
