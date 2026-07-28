package api

import (
	"net/http"
	"os"
	"strings"
)

type APIKeyStore map[string]string

// ParseAPIKeys 示例，通过传入数据演示APIKeyStore的数据结构实际样子
func ParseAPIKeys(raw string) APIKeyStore {
	// 初始化 APIKeyStore 类型的 map：map[apiKey]tenantId
	out := make(APIKeyStore)

	// 假设传入示例: "abc123:tenant1, def456:tenant2,xyz789:tenant3"
	// 则拆分后结构如下:
	// {
	//   "abc123": "tenant1",
	//   "def456": "tenant2",
	//   "xyz789": "tenant3"
	// }
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

func LoadAPIKeysFromEnv() APIKeyStore {
	return ParseAPIKeys(os.Getenv("OBS_API_KEYS"))
}

// AuthMiddleware 是一个中间件，用于基于 API Key 对 /api/ 路径的请求鉴权。
// 若请求不需要鉴权（如健康检查接口），则直接放行。
// 鉴权逻辑：
//  1. 仅 /api/ 开头的请求才进行 API Key 校验，其它请求直接放行。
//  2. 检查 keys 配置是否为空，如果未配置则返回 503。
//  3. 检查 Authorization 头部，必须是 "Bearer <token>" 格式，否则返回 401。
//  4. 解析 Bearer Token，从 keys 字典查找对应租户。
//  5. 若找不到对应租户，则返回 401。
//  6. 若校验通过，将租户信息写入 context 并继续处理请求。
func AuthMiddleware(keys APIKeyStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. 非 /api/ 路径直接放行（如 healthz 检查等）
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		// 2. 如果 keys 未配置，返回 503
		if len(keys) == 0 {
			writeError(w, http.StatusServiceUnavailable, "OBS_API_KEYS not configured")
			return
		}
		// 3. 获取 Authorization 头，必须为 "Bearer <token>" 格式
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		// 4. 解析 token（去除前缀和前后空白）
		token := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		// 5. 校验 token 是否存在于 keys 中，并获取对应租户
		tenant, ok := keys[token]
		if !ok || tenant == "" {
			writeError(w, http.StatusUnauthorized, "invalid api key")
			return
		}
		// 6. 鉴权通过，将租户写入 context，进入后续处理链
		next.ServeHTTP(w, r.WithContext(WithTenant(r.Context(), tenant)))
	})
}

// 示例数据演示：
// raw := "abc123:tenant1, def456:tenant2,xyz789:tenant3"
// result := ParseAPIKeys(raw)
// result 实际内容：
// map[string]string{
//     "abc123": "tenant1",
//     "def456": "tenant2",
//     "xyz789": "tenant3",
// }
