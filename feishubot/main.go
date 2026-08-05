// feishubot：飞书机器人服务。持飞书长连接收聊天命令，经 HTTP 调 syncer，
// 并暴露 cron 触发的日报推送端点。不连 MySQL，内部无调度逻辑。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"feishubot/internal/bot"
	"feishubot/internal/config"
	"feishubot/internal/httpapi"
)

func main() {
	c := config.Load()
	if c.FeishuAppID == "" || c.FeishuAppSecret == "" || len(c.FeishuPushChatIDs) == 0 {
		log.Fatal("FEISHU_APP_ID / FEISHU_APP_SECRET / FEISHU_PUSH_CHAT_ID are required: set them via environment (see .env.example)")
	}
	if c.SyncAPIKey == "" {
		log.Print("warn: SYNC_API_KEY is not set, HTTP endpoints are UNAUTHENTICATED (local debug only)")
	}

	larkClient := lark.NewClient(c.FeishuAppID, c.FeishuAppSecret)
	sender := bot.NewLarkSender(larkClient)
	syncerClient := bot.NewSyncerClient(c.SyncerAPIBase, c.SyncAPIKey)
	cmdHandler := bot.NewCommandHandler(syncerClient, sender)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 长连接收聊天命令；断线重连由 SDK 负责，进程级兜底靠 compose restart。
	go func() {
		if err := bot.RunWS(ctx, c.FeishuAppID, c.FeishuAppSecret, cmdHandler); err != nil && ctx.Err() == nil {
			log.Printf("feishubot: 长连接退出: %v", err)
		}
	}()

	server := httpapi.NewServer(c.BotListenAddr, c.SyncAPIKey, c.FeishuPushChatIDs, syncerClient, sender)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("feishubot: HTTP 服务监听 %s", c.BotListenAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server: %v", err)
	}
}
