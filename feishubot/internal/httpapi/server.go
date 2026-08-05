// Package httpapi 是 feishubot 的 HTTP 面：ping 健康检查与 cron 触发的推送端点。
package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"feishubot/internal/bot"
)

// Server 组装 HTTP 路由。
func NewServer(addr, apiKey, pushChatID string, syncer *bot.SyncerClient, sender bot.Sender) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"message": "pong"})
	})
	mux.HandleFunc("POST /api/v1/push/daily-report",
		apiKeyAuth(apiKey, func(w http.ResponseWriter, r *http.Request) {
			pushDailyReport(w, r, pushChatID, syncer, sender)
		}))
	mux.HandleFunc("POST /api/v1/push/signal-card",
		apiKeyAuth(apiKey, func(w http.ResponseWriter, r *http.Request) {
			pushSignalCard(w, r, pushChatID, syncer, sender)
		}))
	return &http.Server{Addr: addr, Handler: mux}
}

// pushDailyReport 组日报卡片并推送到配置的 chat_id，由系统 crontab curl 触发。
func pushDailyReport(w http.ResponseWriter, r *http.Request, pushChatID string, syncer *bot.SyncerClient, sender bot.Sender) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	st, err := syncer.Status(ctx)
	if err != nil {
		log.Printf("feishubot: 日报拉取状态失败: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	md := bot.RenderStatusReport(st, time.Now())
	if err := sender.PushCard(ctx, pushChatID, md); err != nil {
		log.Printf("feishubot: 日报推送失败: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "daily report pushed"})
}

// pushSignalCard 组盘中信号卡片并推送到配置的 chat_id，由系统 crontab 14:45 curl 触发。
// 非交易日（syncer 判定快照交易日 ≠ 今天）短路不推送，仅记录日志。
func pushSignalCard(w http.ResponseWriter, r *http.Request, pushChatID string, syncer *bot.SyncerClient, sender bot.Sender) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	sig, err := syncer.Signal(ctx)
	if err != nil {
		log.Printf("feishubot: 信号卡片拉取失败: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if !sig.TradingDay {
		log.Printf("feishubot: 非交易日（快照 %s ≠ 今天 %s），信号卡片不推送", sig.SnapshotDate, sig.TradeDate)
		writeJSON(w, http.StatusOK, map[string]any{"pushed": false, "reason": "non-trading day"})
		return
	}
	md := bot.RenderSignalCard(sig, time.Now())
	if err := sender.PushCard(ctx, pushChatID, md); err != nil {
		log.Printf("feishubot: 信号卡片推送失败: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pushed": true})
}

// apiKeyAuth 校验 X-Api-Key，逻辑照搬 syncer 的 ApiKeyAuthMiddleware：
// 密钥为空时不鉴权（仅限本地调试），比对走两侧摘要的常量时间比较。
func apiKeyAuth(apiKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" {
			next(w, r)
			return
		}
		want := sha256.Sum256([]byte(apiKey))
		got := sha256.Sum256([]byte(r.Header.Get("X-Api-Key")))
		if subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
			log.Printf("feishubot: auth rejected: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or missing X-Api-Key"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
