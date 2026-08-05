// Package httpapi 是 feishubot 的 HTTP 面：ping 健康检查与 cron 触发的推送端点。
package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"feishubot/internal/bot"
)

// Server 组装 HTTP 路由。
func NewServer(addr, apiKey string, pushChatIDs []string, syncer *bot.SyncerClient, sender bot.Sender) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"message": "pong"})
	})
	mux.HandleFunc("POST /api/v1/push/daily-report",
		apiKeyAuth(apiKey, func(w http.ResponseWriter, r *http.Request) {
			pushDailyReport(w, r, pushChatIDs, syncer, sender)
		}))
	mux.HandleFunc("POST /api/v1/push/signal-card",
		apiKeyAuth(apiKey, func(w http.ResponseWriter, r *http.Request) {
			pushSignalCard(w, r, pushChatIDs, syncer, sender)
		}))
	mux.HandleFunc("POST /api/v1/push/close-report",
		apiKeyAuth(apiKey, func(w http.ResponseWriter, r *http.Request) {
			pushCloseReport(w, r, pushChatIDs, syncer, sender)
		}))
	return &http.Server{Addr: addr, Handler: mux}
}

// fanout 把一条消息依次推送到全部配置会话（多群推送）。
// 任一会话失败即返回错误；已推送成功的会话不回滚。
func fanout(ctx context.Context, chatIDs []string, send func(context.Context, string) error) error {
	for _, id := range chatIDs {
		if err := send(ctx, id); err != nil {
			return fmt.Errorf("推送会话 %s 失败: %v", id, err)
		}
	}
	return nil
}

// pushToAll 把 markdown 卡片推送到全部配置会话。
func pushToAll(ctx context.Context, sender bot.Sender, chatIDs []string, md string) error {
	return fanout(ctx, chatIDs, func(ctx context.Context, id string) error {
		return sender.PushCard(ctx, id, md)
	})
}

// pushCardJSONToAll 把组装好的卡片 JSON（2.0 表格卡片）推送到全部配置会话。
func pushCardJSONToAll(ctx context.Context, sender bot.Sender, chatIDs []string, cardJSON string) error {
	return fanout(ctx, chatIDs, func(ctx context.Context, id string) error {
		return sender.PushCardJSON(ctx, id, cardJSON)
	})
}

// pushDailyReport 组日报卡片并推送到配置的会话，由系统 crontab curl 触发。
func pushDailyReport(w http.ResponseWriter, r *http.Request, pushChatIDs []string, syncer *bot.SyncerClient, sender bot.Sender) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	st, err := syncer.Status(ctx)
	if err != nil {
		log.Printf("feishubot: 日报拉取状态失败: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	md := bot.RenderStatusReport(st, time.Now())
	if err := pushToAll(ctx, sender, pushChatIDs, md); err != nil {
		log.Printf("feishubot: 日报推送失败: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "daily report pushed"})
}

// pushSignalCard 组盘中信号卡片（2.0 表格卡片 JSON）并推送到配置的会话，由系统 crontab 14:45 curl 触发。
// 非交易日（syncer 判定快照交易日 ≠ 今天）短路不推送，仅记录日志。
func pushSignalCard(w http.ResponseWriter, r *http.Request, pushChatIDs []string, syncer *bot.SyncerClient, sender bot.Sender) {
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
	cardJSON := bot.BuildSignalCardJSON(sig, time.Now())
	if err := pushCardJSONToAll(ctx, sender, pushChatIDs, cardJSON); err != nil {
		log.Printf("feishubot: 信号卡片推送失败: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pushed": true})
}

// pushCloseReport 组收盘日报卡片并推送到配置的会话，由系统 crontab 18:00 curl 触发。
// 与 14:45 信号卡片不同：非交易日/无快照时降级为纯同步摘要，但每天照推，不短路。
// syncer 端一条链内含增量同步，超时与 client 对齐给足 15 分钟（正常为短增量，秒级完成）。
func pushCloseReport(w http.ResponseWriter, r *http.Request, pushChatIDs []string, syncer *bot.SyncerClient, sender bot.Sender) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	report, err := syncer.CloseReport(ctx)
	if err != nil {
		log.Printf("feishubot: 收盘日报拉取失败: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	md := bot.RenderCloseReport(report, time.Now())
	if err := pushToAll(ctx, sender, pushChatIDs, md); err != nil {
		log.Printf("feishubot: 收盘日报推送失败: %v", err)
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
