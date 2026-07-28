package api

import (
	"encoding/json"
	"net/http"
)

// errorBody 是统一的错误响应体，对外只暴露 error 字段。
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON 将任意值以 JSON 写入响应：设置 Content-Type、状态码并编码 body。
// Encode 失败时忽略错误（教学示例简化处理；生产可改为打日志或返回 500）。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError 按约定格式返回错误 JSON，例如 {"error":"trace not found"}。
// 常见映射：缺参 400、ErrorNotFound 404、其它 500。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}
