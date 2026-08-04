package middleware

import (
	"crypto/subtle"
	"net/http"
)

// ApiKeyAuthMiddleware 校验请求头 X-Api-Key 与配置密钥是否一致。
// 配置密钥为空时不鉴权（仅限本地调试），调用方应在启动日志中给出警告。
type ApiKeyAuthMiddleware struct {
	apiKey string
}

func NewApiKeyAuthMiddleware(apiKey string) *ApiKeyAuthMiddleware {
	return &ApiKeyAuthMiddleware{apiKey: apiKey}
}

func (m *ApiKeyAuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.apiKey == "" {
			next(w, r)
			return
		}
		provided := r.Header.Get("X-Api-Key")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(m.apiKey)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte(`{"error":"invalid or missing X-Api-Key"}`)); err != nil {
				return
			}
			return
		}
		next(w, r)
	}
}
