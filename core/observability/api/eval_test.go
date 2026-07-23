package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/evaluation"
	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

// TestCreateAndListEvaluations 验证创建评估与按 Trace 列出评估的 HTTP 接口联调流程。
func TestCreateAndListEvaluations(t *testing.T) {
	// 准备内存存储，并写入一条可被评估的成功 Trace。
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: "tr-e", UserInput: "hi", AgentOutput: "hello",
		StartTime: now, Status: model.SpanStatusSuccess,
	})

	// 注入 FakeCompleter 的 Judge，构造带评估能力的 API Server。
	srv := NewServer(store).WithJudge(evaluation.NewJudge(store, evaluation.FakeCompleter{}))

	// POST 创建评估：请求体携带 trace_id，期望返回 200。
	body, _ := json.Marshal(map[string]string{"trace_id": "tr-e"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post status=%d body=%s", rec.Code, rec.Body.String())
	}

	// GET 按 TraceID 列出评估结果，期望同样返回 200。
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/tr-e", nil)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("get status=%d", rec2.Code)
	}
}
