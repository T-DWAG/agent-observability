package api

import "net/http"

// requestTenant 从请求取出当前租户（由 AuthMiddleware 注入）。
// 无租户时回落 default（兼容未走鉴权的内部调用）。
func requestTenant(r *http.Request) string {
	if t, ok := TenantFromCtx(r.Context()); ok {
		return t
	}
	return "default"
}
