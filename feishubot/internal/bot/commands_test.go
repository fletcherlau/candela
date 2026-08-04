package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSender 记录发送出口调用，替代飞书消息 API。
type fakeSender struct {
	mu       sync.Mutex
	replies  []sentMsg
	pushes   []sentMsg
	replyErr error
}

type sentMsg struct {
	to       string
	markdown string
}

func (f *fakeSender) ReplyCard(_ context.Context, messageID, markdown string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, sentMsg{to: messageID, markdown: markdown})
	return f.replyErr
}

func (f *fakeSender) PushCard(_ context.Context, chatID, markdown string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushes = append(f.pushes, sentMsg{to: chatID, markdown: markdown})
	return f.replyErr
}

func (f *fakeSender) lastReply() sentMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.replies[len(f.replies)-1]
}

// newFakeSyncer 起假 syncer：status 返回固定内容，sync 行为由 syncHandler 决定（nil 时返回空结果）。
func newFakeSyncer(t *testing.T, status *StatusResp, syncHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	if syncHandler == nil {
		syncHandler = func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(SyncResp{})
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(status)
	})
	mux.HandleFunc("/api/v1/sync/etf-daily", syncHandler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

var sampleStatus = &StatusResp{Instruments: []InstrumentStatusItem{
	{
		TsCode:          "510300.SH",
		Name:            "沪深300ETF",
		SyncEnabled:     true,
		LatestTradeDate: "2026-08-03",
		LatestAdjDate:   "2026-08-03",
		DailyRows:       100,
		AdjRows:         101,
	},
}}

func TestParseCommand(t *testing.T) {
	cases := map[string]string{
		"help":                "help",
		"帮助":                  "help",
		"随便说什么":             "help",
		"status":              "status",
		"  Status  ":          "status",
		"状态":                  "status",
		"sync":                "sync",
		"同步":                  "sync",
		"@_user_1 status":     "status", // 群聊 @ 机器人后接命令
		"@_user_1 @_user_2 同步": "sync",
	}
	for in, want := range cases {
		if got := parseCommand(in); got != want {
			t.Errorf("parseCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatusCommandRepliesReport(t *testing.T) {
	srv := newFakeSyncer(t, sampleStatus, nil)
	sender := &fakeSender{}
	h := NewCommandHandler(NewSyncerClient(srv.URL, ""), sender)
	h.now = func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, time.Local) }

	h.HandleText(context.Background(), "om_1", "status")

	if len(sender.replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sender.replies))
	}
	got := sender.lastReply()
	if got.to != "om_1" {
		t.Errorf("reply target = %q, want om_1", got.to)
	}
	for _, want := range []string{"沪深300ETF", "510300.SH", "2026-08-03", "生成时间：2026-08-04 12:00:00"} {
		if !strings.Contains(got.markdown, want) {
			t.Errorf("report missing %q:\n%s", want, got.markdown)
		}
	}
}

func TestStatusCommandSyncerUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // 立即关闭，模拟不可达
	sender := &fakeSender{}
	h := NewCommandHandler(NewSyncerClient(srv.URL, ""), sender)

	h.HandleText(context.Background(), "om_2", "状态")

	if len(sender.replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sender.replies))
	}
	if !strings.Contains(sender.lastReply().markdown, "查询失败") {
		t.Errorf("expected 查询失败 reply, got:\n%s", sender.lastReply().markdown)
	}
}

func TestUnknownCommandRepliesHelp(t *testing.T) {
	srv := newFakeSyncer(t, sampleStatus, nil)
	sender := &fakeSender{}
	h := NewCommandHandler(NewSyncerClient(srv.URL, ""), sender)

	h.HandleText(context.Background(), "om_3", "在吗")

	if len(sender.replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sender.replies))
	}
	if !strings.Contains(sender.lastReply().markdown, "可用命令") {
		t.Errorf("expected help reply, got:\n%s", sender.lastReply().markdown)
	}
}

func TestSyncCommandRepliesAckImmediately(t *testing.T) {
	// sync 端点故意阻塞，验证首条“已触发”回复不等待同步完成。
	release := make(chan struct{})
	srv := newFakeSyncer(t, sampleStatus, func(w http.ResponseWriter, r *http.Request) {
		<-release
		_ = json.NewEncoder(w).Encode(SyncResp{})
	})
	// 后注册先执行：先放行阻塞的 handler，再让 srv.Close() 收尾。
	t.Cleanup(func() { close(release) })
	sender := &fakeSender{}
	h := NewCommandHandler(NewSyncerClient(srv.URL, ""), sender)

	h.HandleText(context.Background(), "om_4", "sync")

	if len(sender.replies) != 1 {
		t.Fatalf("expected 1 immediate reply, got %d", len(sender.replies))
	}
	if !strings.Contains(sender.lastReply().markdown, "同步已触发") {
		t.Errorf("expected ack reply, got:\n%s", sender.lastReply().markdown)
	}
}

func TestFinishSyncPartialFailure(t *testing.T) {
	srv := newFakeSyncer(t, sampleStatus, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(SyncResp{
			Total:   2,
			Success: 1,
			Results: []SyncResultItem{
				{TsCode: "510300.SH", Upserted: 3, DailyUpserted: 2, AdjUpserted: 1, Message: "ok"},
				{TsCode: "510500.SH", Message: "tushare 限频，拉取失败"},
			},
		})
	})
	sender := &fakeSender{}
	h := NewCommandHandler(NewSyncerClient(srv.URL, ""), sender)

	h.finishSync("om_5")

	if len(sender.replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sender.replies))
	}
	got := sender.lastReply().markdown
	for _, want := range []string{"合计 2 只，成功 1 只", "510300.SH", "tushare 限频，拉取失败"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestFinishSyncSyncerUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	sender := &fakeSender{}
	h := NewCommandHandler(NewSyncerClient(srv.URL, ""), sender)

	h.finishSync("om_6")

	if len(sender.replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sender.replies))
	}
	if !strings.Contains(sender.lastReply().markdown, "同步失败") {
		t.Errorf("expected 同步失败 reply, got:\n%s", sender.lastReply().markdown)
	}
}
