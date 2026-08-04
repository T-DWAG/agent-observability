package api

import (
	"net/http"

	"github.com/T-Dwag/agent-observability/evaluation"
	otelsink "github.com/T-Dwag/agent-observability/exporter/otel"
	"github.com/T-Dwag/agent-observability/metrics"
	"github.com/T-Dwag/agent-observability/storage"
)

// Server 持有 Storage；所有 Handler 通过它访问数据。
type Server struct {
	store storage.Storage
	judge *evaluation.Judge
	agg   *metrics.Aggregator
	otel  *otelsink.Exporter
}

func NewServer(store storage.Storage) *Server {
	return &Server{store: store}
}

// Handler 返回已注册路由的 http.Handler。
func (s *Server) Handler(key APIKeyStore) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/v1/traces", s.handleListTraces)
	mux.HandleFunc("GET /api/v1/traces/{id}", s.handleGetTrace)
	mux.HandleFunc("GET /api/v1/traces/{id}/spans", s.handleGetTraceSpans)
	mux.HandleFunc("POST /api/v1/traces/{id}/export", s.handleExportTrace)

	mux.HandleFunc("POST /api/v1/evaluations", s.handleCreateEvaluation)
	mux.HandleFunc("GET /api/v1/evaluations/{trace_id}", s.handleListEvaluations)

	mux.HandleFunc("GET /api/v1/metrics", s.handleGetMetrics)
	return AuthMiddleware(key, mux)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) WithJudge(judge *evaluation.Judge) *Server {
	s.judge = judge
	return s
}

func (s *Server) WithAggregator(agg *metrics.Aggregator) *Server {
	s.agg = agg
	return s
}

// WithOTelExporter 注入 OTel 导出器；未注入时导出接口返回 503。
func (s *Server) WithOTelExporter(exporter *otelsink.Exporter) *Server {
	s.otel = exporter
	return s
}
