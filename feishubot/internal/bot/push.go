// push.go 封装飞书消息发送出口。Sender 是窄接口，单测用 fake 替换生产实现。
package bot

import (
	"context"
	"encoding/json"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// Sender 是飞书消息的发送出口：Push 主动发到会话，Reply 引用回复一条消息。
// PushCard/ReplyCard 发 markdown 卡片（1.0 schema）；
// PushCardJSON/ReplyCardJSON 发组装好的卡片 JSON（如 2.0 表格卡片）。
type Sender interface {
	PushCard(ctx context.Context, chatID, markdown string) error
	PushCardJSON(ctx context.Context, chatID, cardJSON string) error
	ReplyCard(ctx context.Context, messageID, markdown string) error
	ReplyCardJSON(ctx context.Context, messageID, cardJSON string) error
}

// LarkSender 走飞书消息 API 的生产实现。
type LarkSender struct {
	client *lark.Client
}

func NewLarkSender(client *lark.Client) *LarkSender {
	return &LarkSender{client: client}
}

// PushCard 把 markdown 卡片发到指定 chat_id（Daily Report Push 用）。
func (s *LarkSender) PushCard(ctx context.Context, chatID, markdown string) error {
	return s.createMessage(ctx, chatID, cardContent(markdown))
}

// PushCardJSON 把组装好的卡片 JSON 发到指定 chat_id（Signal Card 2.0 表格卡片用）。
func (s *LarkSender) PushCardJSON(ctx context.Context, chatID, cardJSON string) error {
	return s.createMessage(ctx, chatID, cardJSON)
}

// createMessage 发一条 interactive 卡片消息到指定 chat_id（PushCard/PushCardJSON 共用）。
func (s *LarkSender) createMessage(ctx context.Context, chatID, content string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("interactive").
			Content(content).
			Build()).
		Build()
	resp, err := s.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("飞书发消息失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("飞书发消息失败: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// ReplyCard 把 markdown 卡片作为引用回复发到 messageID 所在会话（命令回复用）。
func (s *LarkSender) ReplyCard(ctx context.Context, messageID, markdown string) error {
	return s.replyMessage(ctx, messageID, cardContent(markdown))
}

// ReplyCardJSON 把组装好的卡片 JSON 作为引用回复（signal 命令的 2.0 表格卡片用）。
func (s *LarkSender) ReplyCardJSON(ctx context.Context, messageID, cardJSON string) error {
	return s.replyMessage(ctx, messageID, cardJSON)
}

// replyMessage 作为引用回复发一条 interactive 卡片消息（ReplyCard/ReplyCardJSON 共用）。
func (s *LarkSender) replyMessage(ctx context.Context, messageID, content string) error {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("interactive").
			Content(content).
			Build()).
		Build()
	resp, err := s.client.Im.V1.Message.Reply(ctx, req)
	if err != nil {
		return fmt.Errorf("飞书回复消息失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("飞书回复消息失败: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// cardContent 组装最简形态的卡片 JSON（1.0 schema，单个 lark_md div）。
func cardContent(markdown string) string {
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"elements": []map[string]any{
			{
				"tag": "div",
				"text": map[string]string{
					"tag":     "lark_md",
					"content": markdown,
				},
			},
		},
	}
	data, _ := json.Marshal(card)
	return string(data)
}
