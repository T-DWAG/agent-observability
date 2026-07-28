package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

// newChatModel 按 OBS_LLM_PROVIDER 构造真模型。
//
//	openai → OpenAI / 兼容网关（可设 OBS_LLM_BASE_URL）
//	claude → Anthropic（A 社）
func newChatModel(ctx context.Context) (model.ToolCallingChatModel, error) {
	provider := strings.ToLower(strings.TrimSpace(envOr("OBS_LLM_PROVIDER", "openai")))
	apiKey := strings.TrimSpace(os.Getenv("OBS_LLM_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("OBS_LLM_API_KEY is required")
	}
	modelName := envOr("OBS_LLM_MODEL", defaultModel(provider))

	switch provider {
	case "openai":
		cfg := &openai.ChatModelConfig{
			APIKey: apiKey,
			Model:  modelName,
		}
		if base := strings.TrimSpace(os.Getenv("OBS_LLM_BASE_URL")); base != "" {
			cfg.BaseURL = base
		}
		return openai.NewChatModel(ctx, cfg)

	case "claude", "anthropic":
		return claude.NewChatModel(ctx, &claude.Config{
			APIKey:    apiKey,
			Model:     modelName,
			MaxTokens: 2048,
		})

	default:
		return nil, fmt.Errorf("unsupported OBS_LLM_PROVIDER=%q (use openai|claude)", provider)
	}
}

func defaultModel(provider string) string {
	if provider == "claude" || provider == "anthropic" {
		return "claude-sonnet-4-20250514"
	}
	return "gpt-4o-mini"
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func hasLLMKey() bool {
	return strings.TrimSpace(os.Getenv("OBS_LLM_API_KEY")) != ""
}
