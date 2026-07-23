package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/metrics"
	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

// 测试 /api/v1/metrics 接口，验证其在成功场景下返回的聚合数据正确
func TestGetMetrics(t *testing.T) {
	store := storage.NewMemoryStorage()
	now := time.Now().UTC()
	// 构造 1 条窗内成功 trace，便于后续校验
	_ = store.SaveTrace(context.Background(), &model.Trace{
		TraceID: "tm", StartTime: now.Add(-time.Hour),
		Status: model.SpanStatusSuccess, TotalTokens: 10, DurationMs: 100,
	})

	// 初始化 Server 并配置聚合器
	srv := NewServer(store).WithAggregator(metrics.NewAggregator(store, time.Minute))
	// 构造 HTTP GET 请求，请求 scope=last_24h 的聚合快照
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics?scope=last_24h", nil)
	rec := httptest.NewRecorder()
	// 处理请求
	srv.Handler().ServeHTTP(rec, req)
	// 检查 HTTP 状态码（应为200 OK）
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 解析返回体为 metrics.Snapshot
	var body metrics.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// 校验聚合返回的 trace 总数为 1
	if body.TotalTraces != 1 {
		t.Fatalf("traces=%d", body.TotalTraces)
	}
}

// 测试 /api/v1/metrics 接口，传入不受支持的 scope 时应返回 400 BadRequest
func TestGetMetrics_BadScope(t *testing.T) {
	// 初始化 Server 并配置聚合器
	srv := NewServer(storage.NewMemoryStorage()).
		WithAggregator(metrics.NewAggregator(storage.NewMemoryStorage(), 0))
	// 构造带错误 scope 的 HTTP GET 请求
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics?scope=nope", nil)
	rec := httptest.NewRecorder()
	// 处理请求
	srv.Handler().ServeHTTP(rec, req)
	// 检查 HTTP 状态码，应为 400 BadRequest
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}
