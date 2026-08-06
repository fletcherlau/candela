// syncerclient.go 是 syncer HTTP API 的薄客户端，JSON 字段对齐 syncer 的 internal/types。
package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// statusTimeout 状态查询超时；syncTimeout 同步触发超时（全量回填可能很慢）；
// signalTimeout 盘中信号计算超时（拉实时行情 + 读 1200 点日线窗口 × 4 标的）；
// closeReportTimeout 收盘日报超时（内含一次增量同步 + 收盘重算，对齐 syncTimeout）。
const (
	statusTimeout      = 30 * time.Second
	syncTimeout        = 15 * time.Minute
	signalTimeout      = 60 * time.Second
	closeReportTimeout = 15 * time.Minute
)

// 以下结构体的 JSON 字段名与 syncer/internal/types 保持一致。
type InstrumentStatusItem struct {
	TsCode          string `json:"tsCode"`
	Name            string `json:"name"`
	SyncEnabled     bool   `json:"syncEnabled"`
	LatestTradeDate string `json:"latestTradeDate"`
	LatestAdjDate   string `json:"latestAdjDate"`
	DailyRows       int    `json:"dailyRows"`
	AdjRows         int    `json:"adjRows"`
}

type StatusResp struct {
	Instruments []InstrumentStatusItem `json:"instruments"`
}

type SyncResp struct {
	Total   int              `json:"total"`
	Success int              `json:"success"`
	Results []SyncResultItem `json:"results"`
}

type SyncResultItem struct {
	TsCode        string `json:"tsCode"`
	StartDate     string `json:"startDate"`
	EndDate       string `json:"endDate"`
	Fetched       int    `json:"fetched"`
	Upserted      int    `json:"upserted"`
	DailyFetched  int    `json:"dailyFetched"`
	AdjFetched    int    `json:"adjFetched"`
	DailyUpserted int    `json:"dailyUpserted"`
	AdjUpserted   int    `json:"adjUpserted"`
	Message       string `json:"message"`
}

type SyncerClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// SignalCardItem 是单个标的的盘中信号卡片；Score/YZVol/Quantile 历史不足时为 null。
type SignalCardItem struct {
	TsCode   string   `json:"tsCode"`
	Name     string   `json:"name"`
	Score    *float64 `json:"score"`
	YZVol    *float64 `json:"yzVol"`
	Quantile *float64 `json:"quantile"`
	Weight   float64  `json:"weight"`
	Rank     int      `json:"rank"`
	Stale    bool     `json:"stale"`
	Message  string   `json:"message"`
}

// AdviceItem 是单一持仓情形下的操作建议；TargetWeight 为 null 表示不下单或维持当前仓位不动。
type AdviceItem struct {
	Scenario     string   `json:"scenario"`
	Action       string   `json:"action"`
	TargetWeight *float64 `json:"targetWeight"`
	Note         string   `json:"note"`
}

type SignalResp struct {
	TradeDate    string `json:"tradeDate"`
	SnapshotDate string `json:"snapshotDate"`
	// TradingDay 为 false 表示非交易日（快照交易日 ≠ 今天）：cron 推送应短路；
	// signal 聊天命令不短路，改看 Basis。
	TradingDay bool `json:"tradingDay"`
	// Basis 标定信号口径：realtime = 盘中实时；
	// close = 非交易日回退，卡片由最近交易日（SnapshotDate）官方收盘日线重算。
	// 空串（旧版 syncer）按 realtime 处理。
	Basis string           `json:"basis"`
	Cards []SignalCardItem `json:"cards"`
	// Advice 是五情形交易建议（现金 + 各标的），由 syncer 从 Cards 推导。
	Advice []AdviceItem `json:"advice"`
}

// DiffField 单字段差值（官方日线 − 盘中快照）：Abs 绝对差，Bps 相对差（万分之一，快照为基准）。
type DiffField struct {
	Abs float64 `json:"abs"`
	Bps float64 `json:"bps"`
}

// SlippageDiffItem 单标的滑点差值；Available=false 时各差值字段为 null，Message 记原因。
type SlippageDiffItem struct {
	TsCode    string     `json:"tsCode"`
	Name      string     `json:"name"`
	Available bool       `json:"available"`
	Open      *DiffField `json:"open"`
	High      *DiffField `json:"high"`
	Low       *DiffField `json:"low"`
	Close     *DiffField `json:"close"`
	MeanBps   *float64   `json:"meanBps"`
	Message   string     `json:"message"`
}

// CloseReportResp 收盘日报：同步摘要 + 滑点差值 + 官方收盘重算卡片。
// TradingDay / HasSnapshot 任一为 false 时降级为纯同步摘要渲染。
type CloseReportResp struct {
	TradeDate   string             `json:"tradeDate"`
	TradingDay  bool               `json:"tradingDay"`
	HasSnapshot bool               `json:"hasSnapshot"`
	Sync        SyncResp           `json:"sync"`
	Diffs       []SlippageDiffItem `json:"diffs"`
	Cards       []SignalCardItem   `json:"cards"`
}

func NewSyncerClient(baseURL, apiKey string) *SyncerClient {
	return &SyncerClient{baseURL: baseURL, apiKey: apiKey, http: &http.Client{}}
}

// Status 调 syncer GET /api/v1/sync/status。
func (c *SyncerClient) Status(ctx context.Context) (*StatusResp, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	var out StatusResp
	if err := c.do(ctx, http.MethodGet, "/api/v1/sync/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SyncETFDaily 调 syncer POST /api/v1/sync/etf-daily 触发增量同步。
func (c *SyncerClient) SyncETFDaily(ctx context.Context) (*SyncResp, error) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	var out SyncResp
	if err := c.do(ctx, http.MethodPost, "/api/v1/sync/etf-daily", []byte(`{}`), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Signal 调 syncer POST /api/v1/rotation/signal 计算盘中信号（副作用：快照落库，cron 14:45 用）。
func (c *SyncerClient) Signal(ctx context.Context) (*SignalResp, error) {
	return c.signal(ctx, true)
}

// SignalNoPersist 同 Signal 但 persist=false：只读计算、不落盘中快照（signal 聊天命令用）。
// 非交易日 syncer 回退官方收盘口径（Basis=close），不短路。
func (c *SyncerClient) SignalNoPersist(ctx context.Context) (*SignalResp, error) {
	return c.signal(ctx, false)
}

func (c *SyncerClient) signal(ctx context.Context, persist bool) (*SignalResp, error) {
	ctx, cancel := context.WithTimeout(ctx, signalTimeout)
	defer cancel()
	path := "/api/v1/rotation/signal"
	if !persist {
		path += "?persist=false"
	}
	var out SignalResp
	if err := c.do(ctx, http.MethodPost, path, []byte(`{}`), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CloseReport 调 syncer POST /api/v1/rotation/close-report：
// 一条链完成增量同步 + 滑点差值 + 官方收盘重算（Close Report）。
func (c *SyncerClient) CloseReport(ctx context.Context) (*CloseReportResp, error) {
	ctx, cancel := context.WithTimeout(ctx, closeReportTimeout)
	defer cancel()
	var out CloseReportResp
	if err := c.do(ctx, http.MethodPost, "/api/v1/rotation/close-report", []byte(`{}`), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *SyncerClient) do(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call syncer %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("syncer %s %s 返回 %d: %s", method, path, resp.StatusCode, truncate(string(data), 200))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析 syncer 响应失败: %w", err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
