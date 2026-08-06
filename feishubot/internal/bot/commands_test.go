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

// ReplyCardJSON 记录 2.0 卡片 JSON 回复（与 ReplyCard 共用 replies 台账）。
func (f *fakeSender) ReplyCardJSON(_ context.Context, messageID, cardJSON string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, sentMsg{to: messageID, markdown: cardJSON})
	return f.replyErr
}

func (f *fakeSender) PushCard(_ context.Context, chatID, markdown string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushes = append(f.pushes, sentMsg{to: chatID, markdown: markdown})
	return f.replyErr
}

// PushCardJSON 记录 2.0 卡片 JSON 推送（与 PushCard 共用 pushes 台账）。
func (f *fakeSender) PushCardJSON(_ context.Context, chatID, cardJSON string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushes = append(f.pushes, sentMsg{to: chatID, markdown: cardJSON})
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

// newSignalSyncer 起假 syncer：仅 signal 端点，返回固定响应并把请求 query 记入 gotQuery
// （断言 signal 命令恒为 persist=false）。
func newSignalSyncer(t *testing.T, resp *SignalResp, gotQuery *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/rotation/signal" {
			http.NotFound(w, r)
			return
		}
		*gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(resp)
	}))
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
		"help":                 "help",
		"帮助":                   "help",
		"随便说什么":                "help",
		"status":               "status",
		"  Status  ":           "status",
		"状态":                   "status",
		"signal":               "signal",
		"信号":                   "signal",
		"买什么":                  "signal",
		"sync":                 "sync",
		"同步":                   "sync",
		"@_user_1 status":      "status", // 群聊 @ 机器人后接命令
		"@_user_1 @_user_2 同步": "sync",
		"@_user_1 signal":      "signal",
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

// --- signal 命令 ---

func TestSignalCommandRealtimeBasis(t *testing.T) {
	// 交易日盘中：回复 2.0 表格卡片，口径行为「盘中实时」，请求恒 persist=false。
	var query string
	resp := signalFixture() // TradingDay=true
	resp.Basis = "realtime"
	srv := newSignalSyncer(t, resp, &query)
	sender := &fakeSender{}
	h := NewCommandHandler(NewSyncerClient(srv.URL, ""), sender)
	h.now = func() time.Time { return time.Date(2026, 8, 5, 14, 45, 0, 0, beijingTZ) }

	h.HandleText(context.Background(), "om_s1", "signal")

	if len(sender.replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sender.replies))
	}
	if query != "persist=false" {
		t.Errorf("signal 命令必须 persist=false（不落快照），实际 query = %q", query)
	}
	got := sender.lastReply()
	if got.to != "om_s1" {
		t.Errorf("reply target = %q, want om_s1", got.to)
	}
	for _, want := range []string{`"schema":"2.0"`, `"tag":"table"`, "口径：盘中实时", "数据时间：2026-08-05（盘中快照）"} {
		if !strings.Contains(got.markdown, want) {
			t.Errorf("signal card missing %q:\n%s", want, got.markdown)
		}
	}
}

func TestSignalCommandRealtimeAfterClose(t *testing.T) {
	// 交易日北京时间 15:00 后：实时接口最新价已是收盘价，口径行为「已收盘」。
	var query string
	resp := signalFixture()
	resp.Basis = "realtime"
	srv := newSignalSyncer(t, resp, &query)
	sender := &fakeSender{}
	h := NewCommandHandler(NewSyncerClient(srv.URL, ""), sender)
	h.now = func() time.Time { return time.Date(2026, 8, 5, 15, 30, 0, 0, beijingTZ) }

	h.HandleText(context.Background(), "om_s2", "信号")

	if len(sender.replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sender.replies))
	}
	if !strings.Contains(sender.lastReply().markdown, "口径：已收盘（实时接口，最新价=收盘）") {
		t.Errorf("15:00 后应为已收盘口径:\n%s", sender.lastReply().markdown)
	}
	if query != "persist=false" {
		t.Errorf("signal 命令必须 persist=false，实际 query = %q", query)
	}
}

func TestSignalCommandCloseBasisNonTradingDay(t *testing.T) {
	// 非交易日不短路：syncer 回退收盘口径（basis=close），命令照常回复卡片。
	var query string
	resp := signalFixture()
	resp.TradeDate = "20260808"
	resp.SnapshotDate = "20260807"
	resp.TradingDay = false
	resp.Basis = "close"
	srv := newSignalSyncer(t, resp, &query)
	sender := &fakeSender{}
	h := NewCommandHandler(NewSyncerClient(srv.URL, ""), sender)
	h.now = func() time.Time { return time.Date(2026, 8, 8, 12, 0, 0, 0, beijingTZ) }

	h.HandleText(context.Background(), "om_s3", "买什么")

	if len(sender.replies) != 1 {
		t.Fatalf("非交易日不应短路，expected 1 reply, got %d", len(sender.replies))
	}
	got := sender.lastReply().markdown
	for _, want := range []string{"口径：非交易日，最近交易日 2026-08-07 收盘数据", "数据时间：2026-08-07（官方收盘）"} {
		if !strings.Contains(got, want) {
			t.Errorf("close 口径卡片 missing %q:\n%s", want, got)
		}
	}
	if query != "persist=false" {
		t.Errorf("signal 命令必须 persist=false，实际 query = %q", query)
	}
}

func TestSignalCommandSyncerUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // 立即关闭，模拟不可达
	sender := &fakeSender{}
	h := NewCommandHandler(NewSyncerClient(srv.URL, ""), sender)

	h.HandleText(context.Background(), "om_s4", "signal")

	if len(sender.replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sender.replies))
	}
	if !strings.Contains(sender.lastReply().markdown, "信号查询失败") {
		t.Errorf("expected 信号查询失败 reply, got:\n%s", sender.lastReply().markdown)
	}
}
