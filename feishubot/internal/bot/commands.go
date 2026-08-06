// commands.go 聊天命令的解析与分派：help / status / sync / signal。
package bot

import (
	"context"
	"log"
	"strings"
	"time"
)

const helpMarkdown = "**可用命令**\n\n" +
	"- `status` / `状态`：查看各标的数据同步状态\n" +
	"- `signal` / `信号` / `买什么`：查看当前轮动信号与操作建议（盘中实时；非交易日为最近交易日收盘口径）\n" +
	"- `sync` / `同步`：触发一次增量同步，完成后推结果摘要\n" +
	"- `help` / `帮助`：显示本说明"

// CommandHandler 依赖 syncer 客户端与飞书发送出口，两者都可被单测替换。
type CommandHandler struct {
	syncer *SyncerClient
	sender Sender
	// now 注入时钟，让日报时间可断言。
	now func() time.Time
}

func NewCommandHandler(syncer *SyncerClient, sender Sender) *CommandHandler {
	return &CommandHandler{syncer: syncer, sender: sender, now: time.Now}
}

// HandleText 分派一条文本消息。耗时操作（sync）在内部 goroutine 完成，
// 调用方（事件回调）不会被阻塞。
func (h *CommandHandler) HandleText(ctx context.Context, messageID, text string) {
	switch parseCommand(text) {
	case "status":
		st, err := h.syncer.Status(ctx)
		var md string
		if err != nil {
			md = "**查询失败**\n\nsyncer 不可达：" + err.Error()
		} else {
			md = RenderStatusReport(st, h.now())
		}
		h.reply(ctx, messageID, md)
	case "signal":
		// 与 cron 推送不同：非交易日不短路（syncer 回退官方收盘口径 Basis=close）；
		// 查询恒为 persist=false，不落盘中快照。
		resp, err := h.syncer.SignalNoPersist(ctx)
		if err != nil {
			h.reply(ctx, messageID, "**信号查询失败**\n\nsyncer 不可达："+err.Error())
			return
		}
		h.replyCardJSON(ctx, messageID, BuildSignalCardJSON(resp, h.now()))
	case "sync":
		h.reply(ctx, messageID, "**同步已触发**\n\n完成后会再推一条结果摘要。")
		// 全量回填可能耗时很久，脱离事件回调的 ctx 异步执行。
		go h.finishSync(messageID)
	default:
		h.reply(ctx, messageID, helpMarkdown)
	}
}

// finishSync 完成增量同步并引用原消息推结果摘要。
func (h *CommandHandler) finishSync(messageID string) {
	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()
	resp, err := h.syncer.SyncETFDaily(ctx)
	var md string
	if err != nil {
		md = "**同步失败**\n\n" + err.Error()
	} else {
		md = RenderSyncSummary(resp)
	}
	if err := h.sender.ReplyCard(ctx, messageID, md); err != nil {
		log.Printf("feishubot: 推同步结果失败: %v", err)
	}
}

func (h *CommandHandler) reply(ctx context.Context, messageID, markdown string) {
	if err := h.sender.ReplyCard(ctx, messageID, markdown); err != nil {
		log.Printf("feishubot: 回复消息失败: %v", err)
	}
}

// replyCardJSON 以组装好的卡片 JSON（2.0 表格卡片）引用回复。
func (h *CommandHandler) replyCardJSON(ctx context.Context, messageID, cardJSON string) {
	if err := h.sender.ReplyCardJSON(ctx, messageID, cardJSON); err != nil {
		log.Printf("feishubot: 回复消息失败: %v", err)
	}
}

// parseCommand 解析文本命令：trim、小写化、剥离群聊 @ 机器人留下的 @_user_N 占位。
// 无法识别的输入一律回 help。
func parseCommand(text string) string {
	var kept []string
	for _, f := range strings.Fields(text) {
		if strings.HasPrefix(f, "@_user_") {
			continue
		}
		kept = append(kept, f)
	}
	switch strings.ToLower(strings.Join(kept, " ")) {
	case "status", "状态":
		return "status"
	case "signal", "信号", "买什么":
		return "signal"
	case "sync", "同步":
		return "sync"
	default:
		return "help"
	}
}
