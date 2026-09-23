package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (a *application) rotationDaily(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "查询参数无效", 400)
		return
	}
	index := r.URL.Path == "/api/rotation/daily/dates"
	for key, values := range query {
		allowed := key == "tradeDate" && !index || index && (key == "before" || key == "limit")
		if !allowed || len(values) != 1 || values[0] == "" {
			http.Error(w, "查询参数无效", 400)
			return
		}
		if key == "limit" {
			n, err := strconv.Atoi(values[0])
			if err != nil || n < 1 || n > 100 {
				http.Error(w, "分页数量无效", 400)
				return
			}
		} else if _, err := time.Parse("20060102", values[0]); err != nil {
			http.Error(w, "交易日无效", 400)
			return
		}
	}
	path := "/api/v1/rotation/daily"
	if index {
		path += "/dates"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.SyncerURL+path+"?"+query.Encode(), nil)
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
