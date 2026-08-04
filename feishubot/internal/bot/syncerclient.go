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

// statusTimeout 状态查询超时；syncTimeout 同步触发超时（全量回填可能很慢）。
const (
	statusTimeout = 30 * time.Second
	syncTimeout   = 15 * time.Minute
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
