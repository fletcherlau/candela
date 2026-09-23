package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"
)

func (a *application) rotationDaily(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "查询参数无效", 400)
		return
	}
	for key, values := range query {
		if key != "tradeDate" || len(values) != 1 {
			http.Error(w, "查询参数无效", 400)
			return
		}
		if _, err := time.Parse("20060102", values[0]); err != nil {
			http.Error(w, "交易日无效", 400)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.SyncerURL+"/api/v1/rotation/daily?"+query.Encode(), nil)
	if err != nil {
		http.Error(w, "每日数据暂不可用", 502)
		return
	}
	req.Header.Set("X-Api-Key", a.cfg.APIKey)
	res, err := a.client.Do(req)
	if err != nil {
		http.Error(w, "每日数据暂不可用", 502)
		return
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusBadRequest {
		http.Error(w, "交易日或查询参数无效", 400)
		return
	}
	if res.StatusCode != http.StatusOK {
		http.Error(w, "每日数据读取失败，请稍后重试", 502)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var decoded struct {
		Status string `json:"status"`
	}
	if err != nil || json.Unmarshal(payload, &decoded) != nil || decoded.Status == "" {
		http.Error(w, "每日数据返回异常", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}
