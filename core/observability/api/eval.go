package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

type createEvalRequest struct {
	TraceID string `json:"trace_id"`
}

// handleCreateEvaluation 提交一条异步评估请求。
// 校验 Trace 存在 → 写入 pending 占位 → 返回 202。
// 成功与否不取决于 LLM 调用结果（后台异步跑）。

func (s *Server) handleCreateEvaluation(w http.ResponseWriter, r *http.Request) {

	//检查是否初始化
	if s.judge == nil {
		writeError(w, http.StatusServiceUnavailable, "judge not configured")
		return
	}

	var req createEvalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TraceID == "" {
		writeError(w, http.StatusBadRequest, "trace_id required")
		return
	}

	tenant := requestTenant(r)
	if err := s.judge.EvaluateAsync(r.Context(), tenant, req.TraceID); err != nil {
		if errors.Is(err, storage.ErrorNotFound) {
			writeError(w, http.StatusNotFound, "trace not found")
			return
		}
		if errors.Is(err, storage.ErrorEvaluationExists) {
			writeError(w, http.StatusConflict, "evaluation already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"trace_id": req.TraceID,
		"status":   model.EvalStatusPending,
	})
}

func (s *Server) handleListEvaluations(w http.ResponseWriter, r *http.Request) {
	tenant := requestTenant(r)
	traceID := r.PathValue("trace_id")
	if traceID == "" {
		writeError(w, http.StatusBadRequest, "trace_id required")
		return
	}
	if _, err := s.store.GetTrace(r.Context(), tenant, traceID); err != nil {
		if errors.Is(err, storage.ErrorNotFound) {
			writeError(w, http.StatusNotFound, "trace not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list, err := s.store.ListEvaluations(r.Context(), traceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// overall 是任务行，只用于顶层状态；items 只返回三维评分。
	status := ""
	errorMsg := ""
	items := make([]any, 0, len(list))
	for _, e := range list {
		if e.Dimension == "overall" {
			status = e.Status
			errorMsg = e.ErrorMsg
			continue
		}
		items = append(items, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"trace_id":  traceID,
		"status":    status,
		"error_msg": errorMsg,
		"items":     items,
		"total":     len(items),
	})
}
