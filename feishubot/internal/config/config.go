// Package config 从环境变量读取 feishubot 配置，注入方式与 syncer 一致。
package config

import (
	"os"
	"strings"
)

type Config struct {
	// 飞书自建应用凭证（必填，缺失即拒绝启动）
	FeishuAppID     string
	FeishuAppSecret string
	// 定时推送的目标会话 chat_id 列表（必填；FEISHU_PUSH_CHAT_ID 逗号分隔多个，多群推送）
	FeishuPushChatIDs []string
	// syncer 服务地址
	SyncerAPIBase string
	// 调 syncer 与本服务 HTTP 端点共用的 X-Api-Key
	SyncAPIKey string
	// 本服务 HTTP 监听地址
	BotListenAddr string
}

func Load() Config {
	c := Config{
		FeishuAppID:       os.Getenv("FEISHU_APP_ID"),
		FeishuAppSecret:   os.Getenv("FEISHU_APP_SECRET"),
		FeishuPushChatIDs: parseChatIDs(os.Getenv("FEISHU_PUSH_CHAT_ID")),
		SyncerAPIBase:     os.Getenv("SYNCER_API_BASE"),
		SyncAPIKey:        os.Getenv("SYNC_API_KEY"),
		BotListenAddr:     os.Getenv("BOT_LISTEN_ADDR"),
	}
	if c.SyncerAPIBase == "" {
		c.SyncerAPIBase = "http://127.0.0.1:8888"
	}
	if c.BotListenAddr == "" {
		c.BotListenAddr = ":8889"
	}
	return c
}

// parseChatIDs 解析逗号分隔的 chat_id 列表，忽略空白与空段。
func parseChatIDs(s string) []string {
	var ids []string
	for _, part := range strings.Split(s, ",") {
		if id := strings.TrimSpace(part); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
