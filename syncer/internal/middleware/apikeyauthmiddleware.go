package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/zeromicro/go-zero/core/logx"
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
		if !m.match(r.Header.Get("X-Api-Key")) {
			logx.WithContext(r.Context()).Infof("auth rejected: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid or missing X-Api-Key"}`))
			return
		}
		next(w, r)
	}
}

// match 对两侧摘要（而非原始值）做常量时间比对，
// 避免 ConstantTimeCompare 在长度不一致时提前返回而泄露密钥长度。
func (m *ApiKeyAuthMiddleware) match(provided string) bool {
	want := sha256.Sum256([]byte(m.apiKey))
	got := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}
