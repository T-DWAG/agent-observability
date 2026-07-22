package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/T-Dwag/agent-observability/storage"
)

type createEvalRequest struct {
	TraceID string `json:"trace_id"`
}

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

	evals, err := s.judge.Evaluate(r.Context(), req.TraceID)
	if err != nil {
		if errors.Is(err, storage.ErrorNotFound) {
			writeError(w, http.StatusNotFound, "trace not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"trace_id": req.TraceID,
		"items":    evals,
		"total":    len(evals),
	})
}

func (s *Server) handleListEvaluations(w http.ResponseWriter, r *http.Request) {
	traceID := r.PathValue("trace_id")
	if traceID == "" {
		writeError(w, http.StatusBadRequest, "trace_id required")
		return
	}
	if _, err := s.store.GetTrace(r.Context(), traceID); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{
		"trace_id": traceID,
		"items":    list,
		"total":    len(list),
	})
}
