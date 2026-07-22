package api

import (
	"net/http"

	"github.com/T-Dwag/agent-observability/evaluation"
	"github.com/T-Dwag/agent-observability/storage"
)

// Server 持有 Storage；所有 Handler 通过它访问数据。
type Server struct {
	store storage.Storage
	judge *evaluation.Judge
}

func NewServer(store storage.Storage) *Server {
	return &Server{store: store}
}

// Handler 返回已注册路由的 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/v1/traces", s.handleListTraces)
	mux.HandleFunc("GET /api/v1/traces/{id}", s.handleGetTrace)
	mux.HandleFunc("GET /api/v1/traces/{id}/spans", s.handleGetTraceSpans)

	mux.HandleFunc("POST /api/v1/evaluations", s.handleCreateEvaluation)
	mux.HandleFunc("GET /api/v1/evaluations/{trace_id}", s.handleListEvaluations)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) WithJudge(judge *evaluation.Judge) *Server {
	s.judge = judge
	return s
}
