package main

import (
	"log"
	"net/http"
	"os"
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

	judge := evaluation.NewJudge(store, evaluation.FakeCompleter{})
	agg := metrics.NewAggregator(store, 30*time.Second)

	// 注入存储、Judge、Aggregator，创建 HTTP API 服务并开始监听
	srv := api.NewServer(store).WithJudge(judge).WithAggregator(agg)
	log.Printf("listening on %s", addr)

	keys := api.LoadAPIKeysFromEnv()
	if len(keys) == 0 {
		log.Fatal("set OBS_API_KEYS=key:tenant (e.g. dev-key:default)")
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
