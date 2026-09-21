package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const recoveryPrefix = "/api/rotation/recoveries"
const closeRecoveryPrefix = "/api/rotation/close-syncs"

var rotationRecoveryAction = regexp.MustCompile(`^/api/rotation/(reference-captures/[0-9]{8}|close-syncs/[a-f0-9]{32}|recoveries/[a-f0-9]{32})/retry$`)

// Recovery carries only a saved task identity. It cannot submit dates, symbols,
// credentials, arbitrary upstream paths or a replacement quote from the browser.
func (a *application) rotationRecoveries(w http.ResponseWriter, r *http.Request) {
	action := rotationRecoveryAction.MatchString(r.URL.Path)
	method := r.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}
	if action && method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "不支持此操作。", 405)
		return
	}
	if !action && r.URL.Path != recoveryPrefix && !(strings.HasPrefix(r.URL.Path, recoveryPrefix+"/") && syncRunID.MatchString(strings.TrimPrefix(r.URL.Path, recoveryPrefix+"/"))) {
		http.NotFound(w, r)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "不支持查询参数。", 400)
		return
	}
	if action {
		if strings.HasPrefix(r.URL.Path, capturePrefix+"/") {
			date := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, capturePrefix+"/"), "/retry")
			if _, err := time.Parse("20060102", date); err != nil {
				http.Error(w, "原交易日无效。", 400)
				return
			}
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
		if err != nil || len(body) != 0 {
			http.Error(w, "恢复只接受原任务标识，不允许改写日期或时点。", 400)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, a.cfg.SyncerURL+"/api/v1/rotation/"+strings.TrimPrefix(r.URL.Path, "/api/rotation/"), nil)
	if err != nil {
		http.Error(w, "恢复服务暂不可用。", 502)
		return
	}
	req.Header.Set("X-Api-Key", a.cfg.APIKey)
	res, err := a.client.Do(req)
	if err != nil {
		http.Error(w, "恢复服务暂不可用；提交结果不确定时，请先重新读取恢复记录。", 502)
		return
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 200, 202:
	case 400:
		http.Error(w, "原任务或恢复范围不适用，请核对原交易日与阶段。", 400)
		return
	case 404:
		http.Error(w, "原任务不存在。", 404)
		return
	case 409:
		http.Error(w, "原任务仍在执行、已发布或已取消，无需重复恢复，请重新读取。", 409)
		return
	default:
		http.Error(w, "恢复服务暂不可用，请重新读取记录。", 502)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	var decoded struct {
		Runs []json.RawMessage `json:"runs"`
		Run  *struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	list := r.URL.Path == recoveryPrefix
	if err != nil || len(payload) > 1<<20 || json.Unmarshal(payload, &decoded) != nil || (list && decoded.Runs == nil) || (!list && (decoded.Run == nil || !syncRunID.MatchString(decoded.Run.ID))) {
		http.Error(w, "恢复记录返回异常。", 502)
		return
	}
	if !list && !action && decoded.Run.ID != strings.TrimPrefix(r.URL.Path, recoveryPrefix+"/") {
		http.Error(w, "恢复记录标识不匹配。", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(res.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}
