package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/T-Dwag/agent-observability/api"
	"github.com/T-Dwag/agent-observability/evaluation"
	"github.com/T-Dwag/agent-observability/metrics"
	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func main() {
	// 读取 HTTP 监听地址，未配置时默认使用 :8080
	addr := envOr("OBS_HTTP_ADDR", ":8080")
	// 读取 Postgres 连接串；为空则回退到内存存储
	dsn := os.Getenv("OBS_PG_DSN")

	var store storage.Storage
	if dsn != "" {
		// 已配置 DSN：打开 Postgres，执行自动迁移，再使用 Postgres 存储
		db, err := storage.OpenPostgres(dsn)
		if err != nil {
			log.Fatal(err)
		}
		if err := model.AutoMigrate(db); err != nil {
			log.Fatal(err)
		}
		store = storage.NewPostgresStorage(db)
		log.Printf("storage: postgres")
	} else {
		// 未配置 DSN：使用进程内内存存储，便于本地快速启动
		store = storage.NewMemoryStorage()
		log.Printf("storage: memory (set OBS_PG_DSN to use postgres)")
	}
	//生产化

	// 默认为 FakeCompleter（用于开发或本地调试无需真实模型评测）
	var completer evaluation.ChatCompleter = evaluation.FakeCompleter{}
	// 如果配置了 OBS_JUDGE_MODEL，启用 LLM 评测补全器（通常用于生产环境）
	if model := os.Getenv("OBS_JUDGE_MODEL"); model != "" {
		// 读取评测超时时间，若未配置则默认 30 秒
		timeout := 30 * time.Second
		if t := os.Getenv("OBS_JUDGE_TIMEOUT"); t != "" {
			if d, err := time.ParseDuration(t); err == nil {
				timeout = d
			} else if seconds, err := strconv.Atoi(t); err == nil && seconds > 0 {
				timeout = time.Duration(seconds) * time.Second
			}
		}
		// 读取最大重试次数，未配置则默认 3 次
		maxRetry := 3
		if r := os.Getenv("OBS_JUDGE_MAX_RETRIES"); r != "" {
			if n, err := strconv.Atoi(r); err == nil && n >= 0 {
				maxRetry = n
			}
		}
		inner, err := evaluation.NewLLMCompleterFromEnv(model)
		if err != nil {
			log.Fatal(err)
		}
		completer = evaluation.NewRetryCompleter(
			inner,
			timeout, maxRetry,
		)
	}
	judge := evaluation.NewJudge(store, completer)
	agg := metrics.NewAggregator(store, 30*time.Second)

	// 注入存储、Judge、Aggregator，创建 HTTP API 服务并开始监听
	srv := api.NewServer(store).WithJudge(judge).WithAggregator(agg)
	log.Printf("listening on %s", addr)

	keys := api.LoadAPIKeysFromEnv()
	if len(keys) == 0 {
		log.Fatal("set OBS_API_KEYS=key:tenant (e.g. dev-key:default)")
	}

	if days := os.Getenv("OBS_RETENTION_DAYS"); days != "" {
		d, err := strconv.Atoi(days)
		if err == nil && d > 0 {
			// 计算早于保留天数（OBS_RETENTION_DAYS）阈值的时间点，用于将来实现数据清理（尚未使用）
			before := time.Now().AddDate(0, 0, -d)
			n, err := store.PurgeBefore(context.Background(), "default", before)
			if err != nil {
				log.Printf("[obs] retention purge error: %v", err)

			} else {
				log.Printf("[obs] retention purge %d records before %s", n, before.Format(time.RFC3339))
				// before.Format(time.RFC3339) 是将 time.Time 类型的 before 变量格式化为 RFC3339 标准字符串（如 "2024-06-10T15:04:05Z"）
			}
		}
	}
	log.Fatal(http.ListenAndServe(addr, srv.Handler(keys)))
}

// envOr 读取环境变量；为空时返回默认值
func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
