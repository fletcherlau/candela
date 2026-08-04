// bot.go 飞书长连接装配：接收 im.message.receive_v1 事件并分派给命令处理器。
// WebSocket 的心跳与断线重连由官方 SDK 内部维护。
package bot

import (
	"context"
	"encoding/json"
	"log"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// RunWS 启动长连接事件循环，阻塞到 ctx 取消或连接不可恢复。
func RunWS(ctx context.Context, appID, appSecret string, handler *CommandHandler) error {
	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			onMessage(event, handler)
			return nil
		})
	cli := larkws.NewClient(appID, appSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
	)
	log.Print("feishubot: 长连接启动")
	return cli.Start(ctx)
}

// onMessage 解出文本消息并异步分派，事件回调本身立即返回。
func onMessage(event *larkim.P2MessageReceiveV1, handler *CommandHandler) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return
	}
	msg := event.Event.Message

	// 忽略机器人消息，避免机器人之间互相触发。
	if s := event.Event.Sender; s != nil && s.SenderType != nil && *s.SenderType == "bot" {
		return
	}
	// 只处理文本消息；其他类型（图片/表情等）不回应。
	if msg.MessageType == nil || *msg.MessageType != "text" || msg.Content == nil {
		return
	}
	text := extractText(*msg.Content)
	if text == "" {
		return
	}

	messageID := deref(msg.MessageId)
	// 打印 chat_id：首次部署时从这里拿到 FEISHU_PUSH_CHAT_ID。
	log.Printf("feishubot: 收到消息 chat_id=%s chat_type=%s message_id=%s",
		deref(msg.ChatId), deref(msg.ChatType), messageID)
	if messageID == "" {
		return
	}
	go handler.HandleText(context.Background(), messageID, text)
}

// extractText 解文本消息 content（形如 {"text":"..."}）。
func extractText(content string) string {
	var c struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &c); err != nil {
		return ""
	}
	return c.Text
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
