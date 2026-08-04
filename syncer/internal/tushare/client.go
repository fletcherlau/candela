// Package tushare 实现 core.QuoteSource，覆盖 Tushare fund_daily 接口（HTTP POST JSON）。
package tushare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"syncer/internal/core"
)

// Client 是 Tushare HTTP 客户端。
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient 构造 Tushare 客户端。
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

type request struct {
	ApiName string            `json:"api_name"`
	Token   string            `json:"token"`
	Params  map[string]string `json:"params"`
	Fields  string            `json:"fields"`
}

type response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Fields []string        `json:"fields"`
		Items  [][]interface{} `json:"items"`
	} `json:"data"`
}

const dailyFields = "ts_code,trade_date,open,high,low,close,pre_close,change,pct_chg,vol,amount"

// FetchDaily 实现 core.QuoteSource，拉取 fund_daily。
func (c *Client) FetchDaily(ctx context.Context, tsCode, startDate, endDate string) ([]core.Bar, error) {
	reqBody := request{
		ApiName: "fund_daily",
		Token:   c.token,
		Params:  map[string]string{"ts_code": tsCode, "start_date": startDate, "end_date": endDate},
		Fields:  dailyFields,
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	var resp response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("tushare code=%d msg=%s", resp.Code, resp.Msg)
	}

	idx := map[string]int{}
	for i, f := range resp.Data.Fields {
		idx[f] = i
	}
	str := func(item []interface{}, field string) string {
		i, ok := idx[field]
		if !ok || i >= len(item) || item[i] == nil {
			return ""
		}
		s, _ := item[i].(string)
		return s
	}
	num := func(item []interface{}, field string) float64 {
		i, ok := idx[field]
		if !ok || i >= len(item) || item[i] == nil {
			return 0
		}
		f, _ := item[i].(float64)
		return f
	}

	bars := make([]core.Bar, 0, len(resp.Data.Items))
	for _, item := range resp.Data.Items {
		bars = append(bars, core.Bar{
			TsCode:    str(item, "ts_code"),
			TradeDate: str(item, "trade_date"),
			Open:      num(item, "open"),
			High:      num(item, "high"),
			Low:       num(item, "low"),
			Close:     num(item, "close"),
			PreClose:  num(item, "pre_close"),
			ChangeAmt: num(item, "change"),
			PctChg:    num(item, "pct_chg"),
			Vol:       num(item, "vol"),
			Amount:    num(item, "amount"),
		})
	}
	return bars, nil
}

// Throttler 保证任意两次调用间隔不低于配置值，跨标的、跨接口生效。
type Throttler struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
}

// NewThrottler 构造节流器，interval 为最小调用间隔。
func NewThrottler(interval time.Duration) *Throttler {
	return &Throttler{interval: interval}
}

// Wait 阻塞直至距上次放行已过最小间隔。传给 core.Syncer 的 wait 钩子。
func (t *Throttler) Wait(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if d := time.Since(t.last); d < t.interval {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.interval - d):
		}
	}
	t.last = time.Now()
	return nil
}
