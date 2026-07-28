package main

import (
	"context"
	"testing"

	"github.com/T-Dwag/agent-observability/collector"
	"github.com/T-Dwag/agent-observability/model"
	"github.com/T-Dwag/agent-observability/storage"
)

func TestMount_Live(t *testing.T) {
	if !hasLLMKey() {
		t.Skip("set OBS_LLM_API_KEY for live mount test")
	}
	store := storage.NewMemoryStorage()
	cfg := collector.Config{
		SessionID:         "mount-live",
		UserInput:         "sandbox 里有哪些文件？请根据工具结果简要说明 obs_hints.md 在讲什么。",
		SampleSuccessRate: -1,
	}
	traceID, answer, err := runOnce(context.Background(), store, cfg, cfg.UserInput)
	if err != nil {
		t.Fatal(err)
	}
	if traceID == "" || answer == "" {
		t.Fatalf("traceID=%q answer=%q", traceID, answer)
	}
	tenant := cfg.TenantID
	if tenant == "" {
		tenant = "default"
	}
	tr, err := store.GetTrace(context.Background(), tenant, traceID)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Status != model.SpanStatusSuccess {
		t.Fatalf("status=%q", tr.Status)
	}
	if tenant == "" {
		tenant = "default"
	}

	spans, err := store.GetTraceSpans(context.Background(), tenant, traceID)
	if err != nil {
		t.Fatal(err)
	}
	var sawList, sawRead bool
	for _, sp := range spans {
		if sp.SpanType != model.SpanTypeTool {
			continue
		}
		switch sp.ToolName {
		case "list_files":
			sawList = true
		case "read_file":
			sawRead = true
		}
	}
	if !sawList && !sawRead {
		t.Fatalf("expected tool spans, got %d spans total", len(spans))
	}
}

func TestMount_LiveRedact(t *testing.T) {
	if !hasLLMKey() {
		t.Skip("set OBS_LLM_API_KEY for live redact test")
	}
	store := storage.NewMemoryStorage()
	cfg := collector.Config{
		SessionID:         "mount-redact",
		UserInput:         "联系我13800138000，列出 sandbox 文件",
		Redact:            true,
		SampleSuccessRate: -1,
	}
	traceID, _, err := runOnce(context.Background(), store, cfg, cfg.UserInput)
	if err != nil {
		t.Fatal(err)
	}
	tenant := cfg.TenantID
	if tenant == "" {
		tenant = "default"
	}
	tr, err := store.GetTrace(context.Background(), tenant, traceID)
	if err != nil {
		t.Fatal(err)
	}
	if tr.UserInput != "联系我1**********，列出 sandbox 文件" {
		t.Fatalf("user_input=%q", tr.UserInput)
	}
}
