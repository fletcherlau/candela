package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var syncRunID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// Only this explicit index-run API is writable. Authentication and same-origin
// CSRF have already run in ServeHTTP; no arbitrary upstream path is accepted.
func (a *application) syncRuns(w http.ResponseWriter, r *http.Request) {
	suffix := strings.TrimPrefix(r.URL.Path, "/api/sync-runs")
	if suffix != "" && !syncRunID.MatchString(strings.TrimPrefix(suffix, "/")) {
		http.NotFound(w, r)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "不支持查询参数。", 400)
		return
	}
	method := r.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}
	var body []byte
	if method == http.MethodPost {
		var input struct {
			Mode string `json:"mode"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || (input.Mode != "backfill" && input.Mode != "incremental") {
			http.Error(w, "同步请求格式不正确。", 400)
			return
		}
		body, _ = json.Marshal(input)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, a.cfg.SyncerURL+"/api/v1/data/sync-runs"+suffix, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "任务服务暂时不可用。", 502)
		return
	}
	req.Header.Set("X-Api-Key", a.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		http.Error(w, "任务服务暂时不可用；提交结果不确定时，请先刷新任务列表。", 502)
		return
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		http.Error(w, "任务不存在。", 404)
		return
	}
	if res.StatusCode != 200 && res.StatusCode != 202 {
		http.Error(w, "任务服务请求失败，请刷新任务列表后重试。", 502)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	var decoded struct {
		Run  json.RawMessage   `json:"run"`
		Runs []json.RawMessage `json:"runs"`
	}
	if err != nil || len(payload) > 1<<20 || json.Unmarshal(payload, &decoded) != nil || ((method == http.MethodPost || suffix != "") && len(decoded.Run) == 0) || (method == http.MethodGet && suffix == "" && decoded.Runs == nil) {
		http.Error(w, "任务服务返回异常。", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(res.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}
