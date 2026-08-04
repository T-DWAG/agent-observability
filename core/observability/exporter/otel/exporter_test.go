package otel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// recordingClient 是一个实现 otlptrace.Client 接口的 mock 客户端，
// 用于测试 exporter 的 trace 上传逻辑。每次上传会将请求记录到 requests 字段。
// err 字段用于模拟上传失败的情况。
type recordingClient struct {
	requests [][]*tracepb.ResourceSpans // 保存每次 UploadTraces 上传的 ResourceSpans 请求
	err      error                      // 指定 UploadTraces 返回的错误
}

// Start 方法实现 otlptrace.Client 接口，无实际逻辑，直接返回 nil。
func (c *recordingClient) Start(context.Context) error { return nil }

// Stop 方法实现 otlptrace.Client 接口，无实际逻辑，直接返回 nil。
func (c *recordingClient) Stop(context.Context) error { return nil }

// UploadTraces 方法实现 otlptrace.Client 接口，将上传的请求保存，并返回 err。
func (c *recordingClient) UploadTraces(_ context.Context, request []*tracepb.ResourceSpans) error {
	c.requests = append(c.requests, request)
	return c.err
}

// 编译期类型断言，确保 recordingClient 满足 otlptrace.Client 接口。
var _ otlptrace.Client = (*recordingClient)(nil)

// TestExportTrace_MapsHierarchyAndMetadata 用于测试 ExportTrace 导出功能。
// 覆盖父子 span 关系、元数据转换、默认不导出内容等核心行为。
func TestExportTrace_MapsHierarchyAndMetadata(t *testing.T) {
	// 构造内存存储及测试上下文
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	// 固定时间，用于验证时间戳转换。
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	traceID := "11111111-1111-1111-1111-111111111111"

	// 保存 root trace
	if err := store.SaveTrace(ctx, &model.Trace{
		TraceID: traceID, TenantID: "tenant-a", StartTime: now,
		Status: model.SpanStatusSuccess,
	}); err != nil {
		t.Fatal(err)
	}
	// 保存 agent 类型的根 span
	if err := store.SaveSpan(ctx, &model.Span{
		SpanID: "22222222-2222-2222-2222-222222222222", TraceID: traceID,
		SpanType: model.SpanTypeAgent, SpanName: "assistant", StartTime: now,
		EndTime: now.Add(time.Second), Status: model.SpanStatusSuccess,
	}); err != nil {
		t.Fatal(err)
	}
	// 保存 llm 类型的子 span，包含 token、cost、父子关系等属性
	if err := store.SaveSpan(ctx, &model.Span{
		SpanID: "33333333-3333-3333-3333-333333333333", TraceID: traceID,
		ParentSpanID: "22222222-2222-2222-2222-222222222222",
		SpanType:     model.SpanTypeLLM, SpanName: "chat", ModelName: "deepseek-chat",
		PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30, Cost: 0.01,
		StartTime: now, EndTime: now.Add(500 * time.Millisecond), Status: model.SpanStatusSuccess,
	}); err != nil {
		t.Fatal(err)
	}

	// 准备 mock client 和导出器
	client := &recordingClient{}
	exporter := New(store, client, Config{Timeout: time.Second})

	// 导出 trace 并检查是否顺利
	if err := exporter.ExportTrace(ctx, "tenant-a", traceID); err != nil {
		t.Fatal(err)
	}
	// 检查上传的请求数量与结构（只导出一个 trace，只包含一个 ResourceSpans）
	if len(client.requests) != 1 || len(client.requests[0]) != 1 {
		t.Fatalf("requests=%d resource_spans=%d", len(client.requests), len(client.requests[0]))
	}
	// 判断 scope 下有 2 个 span（即 agent、llm）
	spans := client.requests[0][0].ScopeSpans[0].Spans
	if len(spans) != 2 {
		t.Fatalf("spans=%d want 2", len(spans))
	}
	// 检查两个 span 是否属于同一个 trace
	if string(spans[0].TraceId) != string(spans[1].TraceId) {
		t.Fatal("spans must share trace id")
	}
	// 检查 agent span 必须为根 span（无 parent）
	if len(spans[0].ParentSpanId) != 0 {
		t.Fatal("agent span should be root")
	}
	// 检查 llm span 的父 ID 是否为 agent span 的 ID
	if string(spans[1].ParentSpanId) != string(spans[0].SpanId) {
		t.Fatal("llm parent span mismatch")
	}
	// 检查 llm span 状态码是否为 OK
	if spans[1].Status.Code != tracepb.Status_STATUS_CODE_OK {
		t.Fatalf("status=%v", spans[1].Status.Code)
	}
	// 检查 llm span 中 token 属性已包含
	if attr := findAttribute(spans[1].Attributes, "gen_ai.usage.total_tokens"); attr == nil {
		t.Fatal("missing token attribute")
	}
	// 检查内容属性默认未导出
	if attr := findAttribute(spans[1].Attributes, "obs.content.reasoning"); attr != nil {
		t.Fatal("content must be disabled by default")
	}
}

// TestExportTrace_ContentOptIn 用于测试配置了 ExportContent:true 时内容属性会正确导出。
func TestExportTrace_ContentOptIn(t *testing.T) {
	store := storage.NewMemoryStorage()
	ctx := context.Background()
	now := time.Now().UTC()
	traceID := "t-content"
	// 保存带有敏感内容的 trace、span
	_ = store.SaveTrace(ctx, &model.Trace{
		TraceID: traceID, TenantID: "default", UserInput: "secret input",
		AgentOutput: "secret output", StartTime: now,
	})
	_ = store.SaveSpan(ctx, &model.Span{
		SpanID: "s1", TraceID: traceID, SpanType: model.SpanTypeTool,
		ToolInput: "secret tool input", ToolOutput: "secret tool output",
		StartTime: now, EndTime: now, Status: model.SpanStatusSuccess,
	})

	// ExportContent 打开，内容属性会被导出
	client := &recordingClient{}
	exporter := New(store, client, Config{Timeout: time.Second, ExportContent: true})
	if err := exporter.ExportTrace(ctx, "default", traceID); err != nil {
		t.Fatal(err)
	}
	attrs := client.requests[0][0].ScopeSpans[0].Spans[0].Attributes
	if findAttribute(attrs, "obs.content.tool_input") == nil {
		t.Fatal("content opt-in not applied")
	}
}

// TestExportTrace_TenantIsolation 验证同一 trace 如果租户不一致，导出时会报 not found 错误，确保多租户隔离。
func TestExportTrace_TenantIsolation(t *testing.T) {
	store := storage.NewMemoryStorage()
	traceID := "tenant-trace"
	// 保存 trace 的 tenant 为 tenant-a
	_ = store.SaveTrace(context.Background(), &model.Trace{
		TraceID: traceID, TenantID: "tenant-a", StartTime: time.Now(),
	})

	// 尝试用 tenant-b 导出，预期找不到数据
	exporter := New(store, &recordingClient{}, Config{Timeout: time.Second})
	err := exporter.ExportTrace(context.Background(), "tenant-b", traceID)
	if !errors.Is(err, storage.ErrorNotFound) {
		t.Fatalf("err=%v want not found", err)
	}
}

// TestStableIDs 验证 trace/Span ID 的字节生成是稳定且长度符合要求（16/8字节）。
func TestStableIDs(t *testing.T) {
	if got, want := traceIDBytes("legacy-trace"), traceIDBytes("legacy-trace"); string(got) != string(want) {
		t.Fatal("trace ID mapping must be stable")
	}
	if len(traceIDBytes("legacy-trace")) != 16 {
		t.Fatal("trace ID must be 16 bytes")
	}
	if len(spanIDBytes("legacy-span")) != 8 {
		t.Fatal("span ID must be 8 bytes")
	}
}

// findAttribute 工具函数，根据 key 在 OTLP 属性列表中查找属性。
// 如果找不到返回 nil。
func findAttribute(attrs []*commonpb.KeyValue, key string) *commonpb.KeyValue {
	for _, attr := range attrs {
		if attr.Key == key {
			return attr
		}
	}
	return nil
}
