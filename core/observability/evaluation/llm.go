package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type ChatCompleter interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

type FakeCompleter struct {
	Response string
	Err      error
}

func (f FakeCompleter) Complete(_ context.Context, _, _ string) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	if f.Response != "" {
		return f.Response, nil
	}
	// 默认三分都给中等分
	return `{
  "accuracy":   {"score": 0.9, "reason": "回答覆盖用户问题"},
  "tool_usage": {"score": 0.8, "reason": "工具调用合理"},
  "efficiency": {"score": 0.7, "reason": "token 消耗可接受"}
}`, nil
}

// RetryCompleter 是一个带有重试机制的 ChatCompleter 实现。
// 当内部 ChatCompleter.Complete 方法调用失败时，会自动重试，且支持超时与退避配置。
type RetryCompleter struct {
	inner    ChatCompleter // 实际执行 LLM 请求的底层实现
	timeout  time.Duration // 每次请求的超时时间
	maxRetry int           // 最大重试次数
	backoff  time.Duration // 重试之间的退避时间 指的是每次重试之间等待的时间间隔
}

// 创建一个带有重试机制的 ChatCompleter
func NewRetryCompleter(inner ChatCompleter, timeout time.Duration, maxRetry int) *RetryCompleter {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if maxRetry < 0 {
		maxRetry = 0
	}
	return &RetryCompleter{
		inner:    inner,
		timeout:  timeout,
		maxRetry: maxRetry,
		backoff:  1 * time.Second,
	}
}

func (r *RetryCompleter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {

	var lastErr error

	for i := 0; i <= r.maxRetry; i++ {
		if i > 0 {
			// 这里的表达式是指数退避(backoff)的常见实现方式。
			// r.backoff 是基础间隔时间（如 1 秒），
			// i 表示当前重试次数（从 1 开始，因为 i>0），
			// 1<<(i-1) 是 2 的 (i-1) 次方，因此每次重试等待时间会翻倍。
			// 例如：第一次重试(i=1)，wait = 1 * (2^0) = 1s；
			//      第二次重试(i=2)，wait = 1 * (2^1) = 2s；
			//      第三次重试(i=3)，wait = 1 * (2^2) = 4s。
			wait := r.backoff * time.Duration(1<<(i-1))
			log.Printf("[obs] judge retry %d/%d after %v", i, r.maxRetry, wait)
			select {
			case <-ctx.Done(): // 如果上下文已取消，则立即返回错误
				return "", ctx.Err()
			case <-time.After(wait):
			}
		}

		callCtx, cancel := context.WithTimeout(ctx, r.timeout)
		raw, err := r.inner.Complete(callCtx, systemPrompt, userPrompt)
		cancel()
		if err == nil {
			return raw, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("judge complete failed after %d attempts: %w", r.maxRetry+1, lastErr)
}

type openaiCompleter struct {
	model   string
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewLLMCompleterFromEnv(model string) (ChatCompleter, error) {
	baseURL := strings.TrimRight(os.Getenv("OBS_LLM_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	apiKey := strings.TrimSpace(os.Getenv("OBS_LLM_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("OBS_LLM_API_KEY is required when OBS_JUDGE_MODEL is set")
	}
	return &openaiCompleter{
		model:   model,
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (o *openaiCompleter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	// 构造 OpenAI Chat Completion 接口所需的请求体
	body := map[string]any{
		"model": o.model,
		"messages": []map[string]string{
			// 系统提示信息
			{"role": "system", "content": systemPrompt},
			// 用户问题/输入
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0, // 采样温度为 0，保证结果确定性
	}
	// 序列化请求体为 JSON
	b, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	// 创建 HTTP POST 请求，请求 OpenAI 的 Chat Completion 接口
	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	// 设置请求的 Content-Type 和授权信息
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	// 发送 HTTP 请求
	resp, err := o.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 读取响应内容
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	// 检查响应状态码，非 200 视为错误并返回错误信息和原始响应
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("openai %d: %s", resp.StatusCode, string(raw))
	}

	// 解析响应 JSON，提取模型返回结果
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	// 若未返回任何选项，则报错
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	// 返回第一个选项中的内容
	return out.Choices[0].Message.Content, nil
}
