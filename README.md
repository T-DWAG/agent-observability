# 🔭 Agent Observability

**Go · CloudWeGo Eino · PostgreSQL**  
为 Eino Agent 提供开箱即用的可观测能力：链路追踪、Token 计量、LLM-as-Judge 评估、指标看板、OTel 导出。

---

## ⚡ Quickstart（30 秒接入）

```go
import (
    "github.com/T-Dwag/agent-observability/collector"
    "github.com/T-Dwag/agent-observability/storage"
)

// 1. 选存储（内存开箱即用，PG 上生产）
store := storage.NewMemoryStorage()

// 2. 挂载采集器
ctx, obsHandler, finish := collector.WithObsCallback(ctx, store, collector.Config{
    SessionID: sessionID,
    UserInput: userMessage,
})
defer finish(ctx, finalOutput, runErr)

// 3. 传给 Eino Runner
runner := eino.NewRunner[any, any](ctx, config,
    compose.WithCallbacks(obsHandler),
)
```

运行后 `GET /api/v1/traces` 就能看到数据。

---

## 🧱 架构

```
┌──────────────┐     ┌─────────────────────────────────┐
│  Eino Agent  │────▶│  collector/                      │
│  (Callbacks) │     │  OnStart -> Span -> OnEnd -> Save│
└──────────────┘     └──────────┬──────────────────────┘
                                │
              ┌─────────────────▼────────────────────┐
              │  storage/                             │
              │  Memory（零依赖）/ PostgreSQL（生产）  │
              └──────┬──────────┬──────────┬─────────┘
                     │          │          │
          ┌──────────▼──┐ ┌────▼────┐ ┌───▼──────────┐
          │  api/        │ │metrics/ │ │ exporter/otel │
          │  REST API    │ │ 聚合看板 │ │ -> Jaeger     │
          │  + Judge 评估│ │ 持久快照 │ │               │
          └─────────────┘ └─────────┘ └───────────────┘
```

---

## 📦 安装

```bash
go get github.com/T-Dwag/agent-observability@v1.0.0
```

**前置依赖**：你的项目已经使用 `github.com/cloudwego/eino`。

---

## 🔧 采集器（Collector）

### 基本用法

```go
import (
    "github.com/T-Dwag/agent-observability/collector"
    "github.com/T-Dwag/agent-observability/storage"
)

store := storage.NewMemoryStorage()

ctx, handler, finish := collector.WithObsCallback(ctx, store, collector.Config{
    SessionID: "user-123",
    UserInput: "今天天气怎么样？",
})
defer finish(ctx, output, err)

runner := eino.NewRunner[any, any](ctx, config,
    compose.WithCallbacks(handler),
)
```

### Config 字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `SessionID` | `string` | 会话标识，用于按会话筛选 Trace |
| `UserInput` | `string` | 用户原始输入 |
| `TenantID` | `string` | 租户标识（多租户隔离） |
| `NoContent` | `bool` | 不采集消息正文（隐私模式） |
| `Redact` | `bool` | 对正文打码脱敏 |
| `SampleSuccessRate` | `float64` | 成功 Trace 采样率（0~1），0 表示全采 |
| `CostKeepUSD` | `float64` | 费用高于此值强制保留 |
| `CostAlertUSD` | `float64` | 费用高于此值打 warning 日志 |

### 自动采集的内容

| 类别 | 字段 |
|------|------|
| **Agent** | 执行起止、ReAct 推理过程 |
| **LLM** | 模型名、Prompt/Completion Token、费用估算 |
| **Tool** | 工具名、调用入参、返回结果 |
| **Trace** | 总耗时、总 Token、总费用、成功/失败状态 |

### 获取 TraceID

```go
traceID := collector.TraceIDFromCtx(ctx)
```

---

## 🌐 API 参考

启动服务：

```go
srv := api.NewServer(store).WithJudge(judge).WithAggregator(agg)
http.ListenAndServe(":8080", srv.Handler(keys))
```

### 端点一览

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/healthz` | 健康检查 |
| `GET` | `/api/v1/traces` | 分页查询 Trace 列表 |
| `GET` | `/api/v1/traces/{id}` | 获取单条 Trace 详情 |
| `GET` | `/api/v1/traces/{id}/spans` | 获取 Trace 下所有 Span |
| `POST` | `/api/v1/traces/{id}/export` | 导出到 OTel（Jaeger） |
| `POST` | `/api/v1/evaluations` | 创建 LLM-as-Judge 评估 |
| `GET` | `/api/v1/evaluations/{trace_id}` | 查询评估结果 |
| `GET` | `/api/v1/metrics` | 获取指标快照 |

### 示例

```bash
# 查最近 10 条 Trace
curl -H "Authorization: Bearer dev-key" \
  "http://localhost:8080/api/v1/traces?page=1&size=10"

# 查某条 Trace 的详情
curl -H "Authorization: Bearer dev-key" \
  "http://localhost:8080/api/v1/traces/abc-123"

# 24 小时指标看板
curl -H "Authorization: Bearer dev-key" \
  "http://localhost:8080/api/v1/metrics?scope=last_24h"

# 触发 LLM-as-Judge 评估
curl -X POST -H "Authorization: Bearer dev-key" \
  -H "Content-Type: application/json" \
  -d '{"trace_id":"abc-123"}' \
  "http://localhost:8080/api/v1/evaluations"

# 导出到 Jaeger 看瀑布图
curl -X POST -H "Authorization: Bearer dev-key" \
  "http://localhost:8080/api/v1/traces/abc-123/export"
```

---

## ⚙️ 环境变量

### 存储

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `OBS_PG_DSN` | PostgreSQL 连接串（不设则用内存存储） | — |
| `OBS_RETENTION_DAYS` | Trace 保留天数 | — |

### 鉴权

| 变量 | 格式 | 示例 |
|------|------|------|
| `OBS_API_KEYS` | `key:tenant,key:tenant` | `dev-key:default,prod-key:tenant-a` |

### LLM-as-Judge

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `OBS_JUDGE_MODEL` | 评估用模型名（不设则用 Fake 评估器） | — |
| `OBS_JUDGE_TIMEOUT` | 评估超时 | `30s` |
| `OBS_JUDGE_MAX_RETRIES` | 评估重试次数 | `3` |

### 指标

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `OBS_METRICS_REFRESH_INTERVAL` | 指标快照刷新间隔 | `1m` |

### OTel 导出

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `OBS_OTEL_ENDPOINT` | OTLP HTTP 接收端（不设则不启用） | — |

### 服务

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `OBS_HTTP_ADDR` | HTTP 监听地址 | `:8080` |

---

## 🗄️ 存储

### 内存（开发 / 测试）

```go
store := storage.NewMemoryStorage()
```

零依赖，进程重启数据丢失。

### PostgreSQL（生产）

```bash
export OBS_PG_DSN="host=127.0.0.1 user=postgres password=obs_dev dbname=observability port=5432 sslmode=disable TimeZone=Asia/Shanghai"
```

Docker 快速启动：

```bash
cd docker/postgres
docker compose up -d
```

---

## 🧪 高级特性

### LLM-as-Judge（自动评估）

```go
judge := evaluation.NewJudge(store, completer)
srv := api.NewServer(store).WithJudge(judge)
```

支持异步评估，同一 Trace 只评估一次（幂等），失败自动重试。

### 指标看板（Metrics）

```go
agg := metrics.NewAggregator(store)

// 后台定时刷新（持久化到 obs_metric_snapshots）
go metrics.RunRefresher(ctx, agg, tenantIDs, 1*time.Minute)

srv := api.NewServer(store).WithAggregator(agg)
```

支持 `last_24h` / `last_7d` / `last_30d` 三个固定窗口，返回总量、费用、成功率、Top Tools。

### OTel 导出（Jaeger / Grafana）

```go
exporter, _ := otelsink.NewFromEnv(store)
srv := api.NewServer(store).WithOTelExporter(exporter)
```

将 Trace 按 OTLP 标准格式导出到 Jaeger、Grafana Tempo 等后端。

---

## 🛠️ 本地开发

```bash
cd core/observability

# 全量测试
go test ./... -count=1

# 单包测试
go test ./collector/ ./storage/ -count=1 -v

# 编译
go build ./...

# 启动服务（内存存储 + 测试 API Key）
export OBS_API_KEYS=dev-key:default
go run ./cmd/server/
```

---

## 📂 目录

| 目录 | 职责 |
|------|------|
| `collector/` | Eino Callbacks -> Span/Trace 采集 |
| `model/` | 数据模型 + GORM 自动迁移 |
| `storage/` | 存储抽象（Memory / PostgreSQL） |
| `api/` | REST API + 鉴权中间件 |
| `evaluation/` | LLM-as-Judge 评估引擎 |
| `metrics/` | 指标聚合 + 后台刷新器 |
| `exporter/otel/` | OTLP 格式 Trace 导出 |
| `example/fileagent/` | 完整示例：文件助手 Agent |
| `cmd/server/` | 服务启动入口 |
| `docker/` | PostgreSQL Docker Compose |

---

## 📄 License

MIT
