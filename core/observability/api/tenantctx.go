package api

import "context"

type tenantCtxKey struct{}

func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantCtxKey{}, tenantID) // 将租户ID存储到上下文
}

func TenantFromCtx(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(tenantCtxKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
