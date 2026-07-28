# Agent Observability

Go + CloudWeGo Eino + PostgreSQL 的 Agent 可观测模块（教学向自建主线）。

## 进度

| Step | 内容 | 状态 |
|------|------|------|
| 1 | 数据模型 `model/` | ✅ |
| 2 | Eino Callbacks 采集 `collector/` | ✅ |
| 3 | PostgreSQL 存储 `storage/` | ✅ |
| 4 | API | ✅ |
| 5 | LLM-as-Judge | ✅ |
| 6 | 指标聚合 | ✅ |

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
