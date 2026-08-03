# Agent Observability

Go + CloudWeGo Eino + PostgreSQL 的 Agent 可观测模块（教学向自建主线）。

## 进度

### 教学主线

| Step | 内容 | 状态 |
|------|------|------|
| 1 | 数据模型 `model/` | ✅ |
| 2 | Eino Callbacks 采集 `collector/` | ✅ |
| 3 | PostgreSQL 存储 `storage/` | ✅ |
| 4 | API | ✅ |
| 5 | LLM-as-Judge | ✅ |
| 6 | 指标聚合 | ✅ |

### 实践篇 · 生产化

| 专题 | 内容 | 状态 |
|------|------|------|
| 0 | 可靠性 + 隐私（背压/脱敏/计数） | ✅ |
| 1 | 真实挂载（文件助手 + ADK） | ✅ |
| 2 | 租户与鉴权 | ✅ |
| 3 | 采样 · 保留 · 成本 | ✅ |
| 4 | Eval 生产化（异步评估） | ✅ |
| 5 | Metrics 预聚合（持久化快照） | ✅ |
| 6 | OTel 适配导出 | 待启动 |
| 7 | Trace UI / Replay | 后置 |

## 本地开发

```powershell
cd core\observability
go test ./collector/ ./storage/ -count=1

# PostgreSQL（可选联调）
cd ..\..\docker\postgres
docker compose up -d
$env:OBS_PG_DSN="host=127.0.0.1 user=postgres password=obs_dev dbname=observability port=5432 sslmode=disable TimeZone=Asia/Shanghai"
cd ..\..\core\observability
go test ./storage/ -v -run TestPostgresStorage
```

## Module

`github.com/T-Dwag/agent-observability`
