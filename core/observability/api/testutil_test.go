package api

import "net/http"

// 旧测试统一用这对 Key/租户；鉴权专项测见 auth_test.go。
func testAPIKeys() APIKeyStore {
	return ParseAPIKeys("test-key:default")
}

func withTestAuth(req *http.Request) *http.Request {
	req.Header.Set("Authorization", "Bearer test-key")
	return req
}
