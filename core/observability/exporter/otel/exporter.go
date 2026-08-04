package otel

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

const (
	defaultEndpoint = "localhost:4318"
	defaultURLPath  = "/v1/traces"
	defaultTimeout  = 10 * time.Second
)

type Config struct {
	Endpoint      string
	URLPath       string
	Headers       map[string]string
	Insecure      bool          //是否忽略证书验证
	Timeout       time.Duration //请求超时时间
	ExportContent bool          //是否导出内容

}

type Exporter struct {
	store  storage.Storage
	client otlptrace.Client //OTLP 客户端
	cfg    Config
}

func New(store storage.Storage, client otlptrace.Client, cfg Config) *Exporter {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return &Exporter{
		store:  store,
		client: client,
		cfg:    cfg,
	}
}

func NewFromEnv(store storage.Storage) (*Exporter, error) {
	cfg := Config{
		Endpoint:      envOr("OBS_OTEL_ENDPOINT", defaultEndpoint),
		URLPath:       envOr("OBS_OTEL_URL_PATH", defaultURLPath),
		Headers:       parseHeaders(os.Getenv("OBS_OTEL_HEADERS")),
		Insecure:      envBool("OBS_OTEL_INSECURE", true),
		Timeout:       defaultTimeout,
		ExportContent: envBool("OBS_OTEL_EXPORT_CONTENT", false),
	}
	if raw := os.Getenv("OBS_OTEL_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("OBS_OTEL_TIMEOUT must be a positive duration: %q", raw)
		}
		cfg.Timeout = d
	}

	// 组装 OTLP/HTTP 客户端选项：按 Endpoint 形态选择配置方式，并附加路径、安全与请求头。
	opts := make([]otlptracehttp.Option, 0, 3)
	// Endpoint 若已包含协议（如 https://collector:4318），使用完整 URL；
	// 否则仅作为 host:port，后续再单独设置 URLPath。
	if strings.Contains(cfg.Endpoint, "://") {
		opts = append(opts, otlptracehttp.WithEndpointURL(cfg.Endpoint))
	} else {
		opts = append(opts, otlptracehttp.WithEndpoint(cfg.Endpoint))
	}
	// 仅在 Endpoint 为 host:port 形式时设置 URLPath，避免与完整 URL 冲突。
	if cfg.URLPath != "" && !strings.Contains(cfg.Endpoint, "://") {
		opts = append(opts, otlptracehttp.WithURLPath(cfg.URLPath))
	}
	// Insecure=true 时跳过 TLS 证书校验，便于本地/内网调试。
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	// 附加自定义请求头（如鉴权 Token），有配置时才注入。
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
	}

	// 基于上述选项创建 OTLP/HTTP Trace 导出客户端。
	client := otlptracehttp.NewClient(opts...)
	return New(store, client, cfg), nil
}

// ExportTrace 将指定租户的一条 Trace 及其 Spans 导出到 OTLP Collector。
func (e *Exporter) ExportTrace(ctx context.Context, tenantID, traceID string) error {
	// 导出前校验依赖是否齐全，避免空指针或未初始化客户端。
	if e == nil || e.store == nil || e.client == nil {
		return errors.New("otel exporter is not configured")
	}
	// 从存储读取 Trace 元数据；不存在或查询失败时直接返回。
	tr, err := e.store.GetTrace(ctx, tenantID, traceID)
	if err != nil {
		return err
	}
	// 读取该 Trace 下的全部 Span，作为 OTLP 导出的主体数据。
	spans, err := e.store.GetTraceSpans(ctx, tenantID, traceID)
	if err != nil {
		return fmt.Errorf("get trace spans: %w", err)
	}

	// 将内部 Trace/Span 模型映射为 OTLP ResourceSpans；
	// 规范化租户 ID，并按配置决定是否附带业务内容字段。
	request, err := mapTrace(tr, spans, normalizeTenantID(tenantID), e.cfg.ExportContent)
	if err != nil {
		return err
	}

	// 默认沿用调用方 context；若配置了超时，则叠加独立 deadline，防止导出阻塞过久。
	exportCtx := ctx
	cancel := func() {}
	if e.cfg.Timeout > 0 {
		exportCtx, cancel = context.WithTimeout(ctx, e.cfg.Timeout)
	}
	defer cancel()

	// 通过 OTLP/HTTP 客户端上传映射后的 Trace 数据。
	if err := e.client.UploadTraces(exportCtx, request); err != nil {
		return fmt.Errorf("export trace %s: %w", traceID, err)
	}
	return nil
}

// Shutdown 释放底层 OTLP client。

func (e *Exporter) Shutdown(ctx context.Context) error {
	if e == nil || e.client == nil {
		return nil
	}
	return e.client.Stop(ctx)
}

// mapTrace 将内部 Trace/Span 模型转换为 OTLP ResourceSpans。
func mapTrace(
	tr *model.Trace,
	spans []*model.Span,
	tenantID string,
	exportContent bool,
) ([]*tracepb.ResourceSpans, error) {
	// 把字符串 TraceID 转为 OTLP 要求的 16 字节二进制形式。
	traceID := traceIDBytes(tr.TraceID)
	mapped := make([]*tracepb.Span, 0, len(spans))
	// 记录第一个无父 Span 的位置，后续用于挂载 Trace 级业务内容。
	rootIndex := -1

	// 逐个映射 Span，并顺带定位根 Span。
	for _, span := range spans {
		mappedSpan := mapSpan(traceID, span, tenantID, exportContent)
		if span.ParentSpanID == "" && rootIndex == -1 {
			rootIndex = len(mapped)
		}
		mapped = append(mapped, mappedSpan)
	}

	// Trace 的正文如果被明确允许，只挂到根 Span；默认完全不添加。
	if exportContent && rootIndex >= 0 {
		mapped[rootIndex].Attributes = append(mapped[rootIndex].Attributes,
			stringAttribute("obs.content.user_input", tr.UserInput),
			stringAttribute("obs.content.agent_output", tr.AgentOutput),
		)
	}

	// 组装 ResourceSpans：资源属性携带服务名与租户，ScopeSpans 承载全部 Span。
	resourceSpans := []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			stringAttribute("service.name", "agent-observability"),
			stringAttribute("obs.tenant.id", tenantID),
		}},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: mapped,
		}},
	}}

	// ==== 实际数据结构演示 ====
	//
	// resourceSpans 结构示例:
	// [
	//   ResourceSpans{
	//     Resource: {
	//       Attributes: [
	//         {Key: "service.name",      Value: "agent-observability"},
	//         {Key: "obs.tenant.id",     Value: "foo-tenant-id"}
	//       ]
	//     },
	//     ScopeSpans: [
	//       {
	//         Spans: [
	//           {
	//             TraceId: [16 bytes],
	//             SpanId: [8 bytes],
	//             ParentSpanId: [],
	//             Name: "root-operation",
	//             Attributes: [
	//               {Key: "foo", Value: "bar"},
	//               {Key: "obs.content.user_input",  Value: "..."},
	//               {Key: "obs.content.agent_output", Value: "..."}
	//             ],
	//             ... (其他 Span 字段)
	//           },
	//           {
	//             TraceId: [16 bytes],
	//             SpanId: [8 bytes],
	//             ParentSpanId: [8 bytes],
	//             Name: "child-op",
	//             Attributes: [
	//               {Key: "biz.key", Value: "biz-value"},
	//               ...
	//             ],
	//             ...
	//           }
	//           // ...更多 Span
	//         ]
	//       }
	//     ]
	//   }
	// ]
	//
	// 结构说明:
	// - 最外层为 ResourceSpans 数组，一般实际只有一个元素。
	// - 每个 ResourceSpans 携带资源属性(如服务名和租户)。
	// - Spans 是我们映射的所有追踪片段。根 Span（无 ParentSpanId）如有导出正文则挂业务内容字段。
	// ========================

	return resourceSpans, nil
}

func mapSpan(traceID []byte, span *model.Span, tenantID string, exportContent bool) *tracepb.Span {
	name := span.SpanName
	if name == "" {
		name = span.SpanType
	}
	if name == "" {
		name = "observability.span"
	}

	start := span.StartTime
	end := span.EndTime
	if end.IsZero() || end.Before(start) {
		end = start
	}

	attrs := []*commonpb.KeyValue{
		stringAttribute("obs.tenant.id", tenantID),
		stringAttribute("obs.span.type", span.SpanType),
	}
	if span.ModelName != "" {
		attrs = append(attrs, stringAttribute("gen_ai.request.model", span.ModelName))
	}
	if span.SpanType == model.SpanTypeLLM {
		attrs = append(attrs,
			intAttribute("gen_ai.usage.input_tokens", span.PromptTokens),
			intAttribute("gen_ai.usage.output_tokens", span.CompletionTokens),
			intAttribute("gen_ai.usage.total_tokens", span.TotalTokens),
			floatAttribute("obs.cost.usd", span.Cost),
		)
	}
	if span.ToolName != "" {
		attrs = append(attrs, stringAttribute("gen_ai.tool.name", span.ToolName))
	}
	if exportContent {
		attrs = append(attrs,
			stringAttribute("obs.content.tool_input", span.ToolInput),
			stringAttribute("obs.content.tool_output", span.ToolOutput),
			stringAttribute("obs.content.reasoning", span.Reasoning),
		)
	}

	out := &tracepb.Span{
		TraceId:           traceID,
		SpanId:            spanIDBytes(span.SpanID),
		ParentSpanId:      parentSpanIDBytes(span.ParentSpanID),
		Name:              name,
		Kind:              tracepb.Span_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: unixNano(start),
		EndTimeUnixNano:   unixNano(end),
		Attributes:        attrs,
	}
	out.Status = statusFor(span.Status, span.ErrorMsg)
	return out
}

// ----- 数据结构示例 -----
// tracepb.Span 基本结构:
//
// type Span struct {
//     TraceId           []byte
//     SpanId            []byte
//     ParentSpanId      []byte
//     Name              string
//     Kind              Span_SpanKind
//     StartTimeUnixNano uint64
//     EndTimeUnixNano   uint64
//     Attributes        []*commonpb.KeyValue
//     Status            *Status
//     // ... 其他 OpenTelemetry 字段
// }
//
// commonpb.KeyValue 基本结构:
//
// type KeyValue struct {
//     Key   string
//     Value *AnyValue
// }
// type AnyValue struct {
//     // 可为 string, int, float64, bool 等
//     StringValue string
//     IntValue    int64
//     DoubleValue float64
//     BoolValue   bool
//     // ...其他类型
// }
//
// 示例序列化后的 Span:
// {
//     TraceId: [16]byte,
//     SpanId: [8]byte,
//     ParentSpanId: [8]byte 或空,
//     Name: "user-call-tool",
//     Kind: SPAN_KIND_INTERNAL,
//     StartTimeUnixNano: 1718007772000000000,
//     EndTimeUnixNano:   1718007773000000000,
//     Attributes: [
//         { Key: "obs.tenant.id", Value: {StringValue: "foo-tenant-id"} },
//         { Key: "obs.span.type", Value: {StringValue: "tool"} },
//         { Key: "gen_ai.tool.name", Value: {StringValue: "search-web"} },
//         { Key: "obs.content.tool_input", Value: {StringValue: "..."} },
//         // ...更多 tags
//     ],
//     Status: {Code: 0, Message: ""}
// }

func statusFor(status, errorMsg string) *tracepb.Status {
	switch status {
	case model.SpanStatusSuccess:
		return &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}
	case model.SpanStatusError:
		return &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: errorMsg}
	default:
		return &tracepb.Status{Code: tracepb.Status_STATUS_CODE_UNSET}
	}
}

// traceIDBytes 将字符串类型的 TraceID 转为 OpenTelemetry 需要的 16 字节数组。
// 如果 id 能够被解析为 uuid（即 16 字节的唯一标识），则直接返回其字节表示；
// 否则，将 id 以 "trace:" 前缀拼接后做 sha256 哈希，并取前 16 字节作为 TraceID。
// 这样设计可以保证：
// 1. 对于原始 TraceID 已为 uuid 的情况，能与标准格式兼容（如分布式系统溯源）；
// 2. 对于普通字符串 TraceID，也能稳定地生成唯一且格式正确的 TraceID 字节数组。
func traceIDBytes(id string) []byte {
	if parsed, err := uuid.Parse(id); err == nil {
		out := make([]byte, 16)
		copy(out, parsed[:])
		return out
	}
	sum := sha256.Sum256([]byte("trace:" + id))
	return append([]byte(nil), sum[:16]...)
}

func spanIDBytes(id string) []byte {
	if parsed, err := uuid.Parse(id); err == nil {
		out := make([]byte, 8)
		copy(out, parsed[8:])
		return out
	}
	sum := sha256.Sum256([]byte("span:" + id))
	return append([]byte(nil), sum[:8]...)
}

// parentSpanIDBytes 将父 Span 的 ID（字符串）转为 OpenTelemetry 需要的 8 字节 SpanID。
// OpenTelemetry 协议规定：如果该 Span 是根节点（即没有父 span），则 ParentSpanId 字段应为全 0（nil/空）。
// 因此，当 id 为空字符串时，意味着当前 span 是根 span，没有 parent，按照协议返回 nil（或长度为 0 的字节切片）。
func parentSpanIDBytes(id string) []byte {
	if id == "" {
		// 根 Span：没有父 Span。otel.proto 约定该字段应为 nil/空。
		return nil
	}
	return spanIDBytes(id)
}

// unixNano 返回时间 t 的纳秒时间戳（自 Unix 纪元起算，单位纳秒）。
// 如果 t 为空（即 time.Time 的零值），返回 0。
func unixNano(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	return uint64(t.UnixNano())
}

func stringAttribute(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{
		Value: &commonpb.AnyValue_StringValue{StringValue: value},
	}}
}

func intAttribute(key string, value int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{
		Value: &commonpb.AnyValue_IntValue{IntValue: value},
	}}
}

func floatAttribute(key string, value float64) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{
		Value: &commonpb.AnyValue_DoubleValue{DoubleValue: value},
	}}
}

func normalizeTenantID(id string) string {
	if id == "" {
		return "default"
	}
	return id
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// envBool 尝试从环境变量中读取布尔值。
// 如果指定的环境变量 key 存在且可以被解析为布尔值（true/false、1/0、"yes"/"no"等），则返回解析后的值。
// 否则，如果环境变量未设置或无法解析为布尔值，则返回 fallback 作为默认值。
func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// parseHeaders 展现一个实际的数据处理流程：
// 输入如 "Authorization=Bearer xxx, X-Tenant=abc" 的原始字符串，解析为 map 结构。
func parseHeaders(raw string) map[string]string {
	result := make(map[string]string)
	// 第一步：用逗号分割成若干组形如 "key=value" 的片段
	chunks := strings.Split(raw, ",")
	for _, chunk := range chunks {
		// 第二步：去除每组的前后空白
		clean := strings.TrimSpace(chunk)

		// 第三步：以第一个等号拆分为 key 和 value
		key, value, ok := strings.Cut(clean, "=")
		if !ok {
			continue // 跳过不符合"key=value"格式的片段
		}

		// 第四步：分别对 key 和 value 去除空白
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue // 跳过空 key
		}
		// 第五步：写入结果 map
		result[key] = value
	}
	// 最终得到 key->value 映射的 map
	return result
}
