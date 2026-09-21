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

const etfSyncPrefix = "/api/etf-syncs"

var etfSyncAction = regexp.MustCompile(`^/api/etf-syncs/[a-f0-9]{32}/(retry|cancel)$`)
var etfCode = regexp.MustCompile(`^[0-9]{6}\.(SH|SZ)$`)

// Only batch creation and the original batch's retry/cancel intent cross this
// boundary. Only new historical batches accept a date range; retries cannot
// replace their saved scope. Credentials and upstream paths stay server-owned.
func (a *application) etfSyncs(w http.ResponseWriter, r *http.Request) {
	suffix := strings.TrimPrefix(r.URL.Path, etfSyncPrefix)
	action := etfSyncAction.MatchString(r.URL.Path)
	if suffix != "" && !action && !syncRunID.MatchString(strings.TrimPrefix(suffix, "/")) {
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
	if action && method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "不支持此操作。", 405)
		return
	}
	var body []byte
	if method == http.MethodPost {
		if action {
			value, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
			if err != nil || len(value) != 0 {
				http.Error(w, "重试或取消只接受原批次标识。", 400)
				return
			}
		} else {
			var input struct {
				Codes     []string `json:"codes"`
				Mode      string   `json:"mode,omitempty"`
				StartDate string   `json:"startDate,omitempty"`
				EndDate   string   `json:"endDate,omitempty"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || len(input.Codes) > 500 {
				http.Error(w, "ETF 同步请求格式不正确。", 400)
				return
			}
			validScope := false
			switch input.Mode {
			case "", "incremental":
				validScope = input.StartDate == "" && input.EndDate == ""
			case "historical":
				from, firstErr := time.Parse("20060102", input.StartDate)
				_, lastErr := time.Parse("20060102", input.EndDate)
				validScope = firstErr == nil && lastErr == nil && from.Year() >= 1 && input.StartDate <= input.EndDate
			}
			// The synchronization service owns the clock and the final cutoff.
			if !validScope {
				if input.Mode == "historical" {
					w.Header().Set("X-Validation-Field", "range")
				}
				http.Error(w, "请选择有效的同步方式和起止日期。", 400)
				return
			}
			for _, code := range input.Codes {
				if !etfCode.MatchString(code) {
					http.Error(w, "ETF 代码格式不正确。", 400)
					return
				}
			}
			body, _ = json.Marshal(input)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, a.cfg.SyncerURL+"/api/v1/data/etf-syncs"+suffix, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "任务服务暂时不可用。", 502)
		return
	}
	req.Header.Set("X-Api-Key", a.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		http.Error(w, "任务服务暂时不可用；提交结果不确定时，请先刷新批次列表。", 502)
		return
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 200, 202:
	case 404:
		http.Error(w, "批次不存在。", 404)
		return
	case 400:
		// Forward only this explicit field marker, never upstream error details.
		if res.Header.Get("X-Validation-Field") == "range" {
			w.Header().Set("X-Validation-Field", "range")
		}
		http.Error(w, "提交范围或恢复条件不满足，请检查批次。", 400)
		return
	case 409:
		http.Error(w, "批次尚未结束或没有可重试的失败对象，请刷新批次。", 409)
		return
	default:
		http.Error(w, "任务服务请求失败，请刷新批次列表后重试。", 502)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	var decoded struct {
		Batch   json.RawMessage   `json:"batch"`
		Batches []json.RawMessage `json:"batches"`
	}
	if err != nil || len(payload) > 2<<20 || json.Unmarshal(payload, &decoded) != nil || ((method == "POST" || suffix != "") && (len(decoded.Batch) == 0 || string(decoded.Batch) == "null")) || (method == "GET" && suffix == "" && decoded.Batches == nil) {
		http.Error(w, "任务服务返回异常。", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(res.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}
