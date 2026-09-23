package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const capturePrefix = "/api/rotation/reference-captures"

// This browser boundary exposes saved records only. External cron submits to
// the service's authenticated endpoint; reading a page cannot acquire quotes.
func (a *application) rotationCaptures(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		http.Error(w, "不支持查询参数", 400)
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, capturePrefix)
	if suffix != "" {
		if len(suffix) != 9 || suffix[0] != '/' {
			http.NotFound(w, r)
			return
		}
		if _, err := time.Parse("20060102", suffix[1:]); err != nil {
			http.Error(w, "交易日无效", 400)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.SyncerURL+"/api/v1/rotation/reference-captures"+suffix, nil)
	if err != nil {
		http.Error(w, "采集记录暂不可用", 502)
		return
	}
	req.Header.Set("X-Api-Key", a.cfg.APIKey)
	res, err := a.client.Do(req)
	if err != nil {
		http.Error(w, "采集记录读取失败，请稍后重试", 502)
		return
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 400:
		http.Error(w, "交易日无效", 400)
		return
	case 404:
		http.Error(w, "该交易日尚无采集记录", 404)
		return
	case 200:
	default:
		http.Error(w, "采集记录读取失败，请稍后重试", 502)
		return
	}
	const limit = 8 << 20
	payload, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	var decoded struct {
		Runs []json.RawMessage `json:"runs"`
		Run  *struct {
			TradeDate string `json:"tradeDate"`
		} `json:"run"`
	}
	if err != nil || len(payload) > limit || json.Unmarshal(payload, &decoded) != nil || (suffix == "" && decoded.Runs == nil) || (suffix != "" && (decoded.Run == nil || decoded.Run.TradeDate != suffix[1:])) {
		http.Error(w, "采集记录返回异常", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}
