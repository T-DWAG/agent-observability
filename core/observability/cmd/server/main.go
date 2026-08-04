package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/T-Dwag/agent-observability/api"
	"github.com/T-Dwag/agent-observability/evaluation"
	otelsink "github.com/T-Dwag/agent-observability/exporter/otel"
	"github.com/T-Dwag/agent-observability/metrics"
	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func main() {
	addr := envOr("OBS_HTTP_ADDR", ":8080")
	dsn := os.Getenv("OBS_PG_DSN")

	var store storage.Storage
	if dsn != "" {
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
		store = storage.NewMemoryStorage()
		log.Printf("storage: memory (set OBS_PG_DSN to use postgres)")
	}

	keys := api.LoadAPIKeysFromEnv()
	if len(keys) == 0 {
		log.Fatal("set OBS_API_KEYS=key:tenant (e.g. dev-key:default)")
	}

	// 先清理过期 Trace，再生成第一份快照，避免启动后立刻读到已过期数据。
	if days := os.Getenv("OBS_RETENTION_DAYS"); days != "" {
		d, err := strconv.Atoi(days)
		if err == nil && d > 0 {
			before := time.Now().AddDate(0, 0, -d)
			n, err := store.PurgeBefore(context.Background(), "default", before)
			if err != nil {
				log.Printf("[obs] retention purge error: %v", err)
			} else {
				log.Printf("[obs] retention purge %d records before %s", n, before.Format(time.RFC3339))
			}
		}
	}

	var completer evaluation.ChatCompleter = evaluation.FakeCompleter{}
	if modelName := os.Getenv("OBS_JUDGE_MODEL"); modelName != "" {
		timeout := 30 * time.Second
		if t := os.Getenv("OBS_JUDGE_TIMEOUT"); t != "" {
			if d, err := time.ParseDuration(t); err == nil {
				timeout = d
			} else if seconds, err := strconv.Atoi(t); err == nil && seconds > 0 {
				timeout = time.Duration(seconds) * time.Second
			}
		}
		maxRetry := 3
		if r := os.Getenv("OBS_JUDGE_MAX_RETRIES"); r != "" {
			if n, err := strconv.Atoi(r); err == nil && n >= 0 {
				maxRetry = n
			}
		}
		inner, err := evaluation.NewLLMCompleterFromEnv(modelName)
		if err != nil {
			log.Fatal(err)
		}
		completer = evaluation.NewRetryCompleter(inner, timeout, maxRetry)
	}
	judge := evaluation.NewJudge(store, completer)

	agg := metrics.NewAggregator(store)
	refreshInterval := time.Minute
	if raw := os.Getenv("OBS_METRICS_REFRESH_INTERVAL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			log.Fatal("OBS_METRICS_REFRESH_INTERVAL must be a positive duration, e.g. 1m")
		}
		refreshInterval = parsed
	}
	go metrics.RunRefresher(
		context.Background(),
		agg,
		tenantIDs(keys),
		refreshInterval,
	)
	var otelExporter *otelsink.Exporter
	if os.Getenv("OBS_OTEL_ENDPOINT") != "" {
		var err error
		otelExporter, err = otelsink.NewFromEnv(store)
		if err != nil {
			log.Fatal(err)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := otelExporter.Shutdown(shutdownCtx); err != nil {
				log.Printf("[obs] otel shutdown failed: %v", err)
			}
		}()
	}

	srv := api.NewServer(store).WithJudge(judge).WithAggregator(agg).WithOTelExporter(otelExporter)
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, srv.Handler(keys)))
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func tenantIDs(keys api.APIKeyStore) []string {
	seen := make(map[string]struct{})
	for _, tenantID := range keys {
		if tenantID == "" {
			continue
		}
		seen[tenantID] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for tenantID := range seen {
		out = append(out, tenantID)
	}
	sort.Strings(out)
	return out
}
