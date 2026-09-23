package rotation

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/zeromicro/go-zero/rest"
	"syncer/internal/syncrun"
)

const closeRecoveryPath = "/api/v1/rotation/close-syncs"

const recoveryPath = "/api/v1/rotation/recoveries"

func (s *Service) RecoveryRoutes(auth func(http.HandlerFunc) http.HandlerFunc) []rest.Route {
	h := auth(s.recoveryHandler)
	return []rest.Route{{Method: "GET", Path: recoveryPath, Handler: h}, {Method: "GET", Path: recoveryPath + "/:id", Handler: h}, {Method: "GET", Path: recoveryPath + "/origins/:basis/:origin", Handler: h}, {Method: "POST", Path: recoveryPath + "/:id/retry", Handler: h}, {Method: "POST", Path: capturePath + "/:date/retry", Handler: h}, {Method: "POST", Path: closeRecoveryPath + "/:id/retry", Handler: h}}
}
func (s *Service) recoveryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	send := func(status int, v any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(v) }
	fail := func(err error) {
		var invalid *captureRequestError
		switch {
		case errors.As(err, &invalid):
			send(invalid.status, map[string]string{"error": invalid.message})
		case errors.Is(err, sql.ErrNoRows):
			send(404, map[string]string{"error": "原任务不存在。"})
		default:
			send(503, map[string]string{"error": "恢复状态暂不可读，请查询原任务后重试。"})
		}
	}
	if r.URL.RawQuery != "" {
		send(400, map[string]string{"error": "不支持查询参数。"})
		return
	}
	if r.Method == "GET" {
		if r.URL.Path == recoveryPath || strings.HasPrefix(r.URL.Path, recoveryPath+"/origins/") {
			basis, origin := "", ""
			if r.URL.Path != recoveryPath {
				parts := strings.Split(strings.TrimPrefix(r.URL.Path, recoveryPath+"/origins/"), "/")
				if len(parts) != 2 {
					send(404, map[string]string{"error": "原任务不存在。"})
					return
				}
				basis, origin = parts[0], parts[1]
				if basis == "reference_1445" {
					if _, err := captureTarget(origin); err != nil {
						send(400, map[string]string{"error": "原日期无效。"})
						return
					}
				} else if basis != "close" || !syncrun.ValidID(origin) {
					send(404, map[string]string{"error": "原任务不存在。"})
					return
				}
			}
			runs, err := s.recoveryList(r.Context(), basis, origin)
			if err != nil {
				fail(err)
				return
			}
			send(200, map[string]any{"runs": runs})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, recoveryPath+"/")
		if !syncrun.ValidID(id) {
			send(404, map[string]string{"error": "任务不存在。"})
			return
		}
		run, err := s.recoveryRun(r.Context(), id)
		if err != nil {
			fail(err)
			return
		}
		send(200, map[string]any{"run": run})
		return
	}
	if r.Method != "POST" {
		send(405, map[string]string{"error": "不支持此操作。"})
		return
	}
	value, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if err != nil || len(value) != 0 {
		send(400, map[string]string{"error": "恢复只接受原任务标识，不允许改写日期或时点。"})
		return
	}
	origin, parent, basis := "", "", "reference_1445"
	if strings.HasPrefix(r.URL.Path, capturePath+"/") && strings.HasSuffix(r.URL.Path, "/retry") {
		origin = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, capturePath+"/"), "/retry")
		if _, err := captureTarget(origin); err != nil || origin > s.today() {
			send(400, map[string]string{"error": "原交易日无效。"})
			return
		}
	} else if strings.HasPrefix(r.URL.Path, closeRecoveryPath+"/") && strings.HasSuffix(r.URL.Path, "/retry") {
		origin = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, closeRecoveryPath+"/"), "/retry")
		basis = "close"
		if !syncrun.ValidID(origin) {
			send(404, map[string]string{"error": "原任务不存在。"})
			return
		}
	} else if strings.HasPrefix(r.URL.Path, recoveryPath+"/") && strings.HasSuffix(r.URL.Path, "/retry") {
		parent = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, recoveryPath+"/"), "/retry")
		if !syncrun.ValidID(parent) {
			send(404, map[string]string{"error": "原任务不存在。"})
			return
		}
	} else {
		send(404, map[string]string{"error": "任务不存在。"})
		return
	}
	if parent != "" {
		previous, err := s.recoveryRun(r.Context(), parent)
		if err != nil {
			fail(err)
			return
		}
		origin, basis = previous.OriginID, previous.Basis
	}
	var run RecoveryRun
	var duplicate bool
	if basis == "close" {
		run, duplicate, err = s.acceptCloseRecovery(r.Context(), origin, parent)
	} else {
		run, duplicate, err = s.acceptReferenceRecovery(r.Context(), origin, parent)
	}
	if err != nil {
		fail(err)
		return
	}
	status := 202
	if duplicate {
		status = 200
	}
	send(status, map[string]any{"run": run, "deduplicated": duplicate})
}
