package syncrun

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func ValidID(id string) bool { return idPattern.MatchString(id) }

// Handler is internal: the caller must wrap it with the syncer API-key middleware.
func (s *Store) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		send := func(status int, value any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
		fail := func(status int, message string) { send(status, map[string]string{"error": message}) }
		const path = "/api/v1/data/sync-runs"
		if r.URL.RawQuery != "" {
			fail(400, "不支持查询参数。")
			return
		}
		if r.URL.Path == path {
			switch r.Method {
			case http.MethodGet:
				runs, err := s.List(r.Context())
				if err != nil {
					fail(503, "任务列表读取失败。")
					return
				}
				send(200, map[string]any{"runs": runs})
			case http.MethodPost:
				var input struct {
					Mode string `json:"mode"`
				}
				decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
				decoder.DisallowUnknownFields()
				if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
					fail(400, "请求格式不正确。")
					return
				}
				run, duplicate, err := s.Submit(r.Context(), input.Mode, time.Now())
				if errors.Is(err, ErrMode) {
					fail(400, "仅支持 backfill 或 incremental。")
					return
				}
				if err != nil {
					fail(503, "任务提交未确认，请查询任务列表后重试。")
					return
				}
				status := http.StatusAccepted
				if duplicate {
					status = http.StatusOK
				}
				send(status, map[string]any{"run": run, "deduplicated": duplicate})
			default:
				w.Header().Set("Allow", "GET, POST")
				fail(405, "不支持此操作。")
			}
			return
		}
		id := strings.TrimPrefix(r.URL.Path, path+"/")
		cancelling := strings.HasSuffix(id, "/cancel")
		if cancelling {
			id = strings.TrimSuffix(id, "/cancel")
		}
		if !ValidID(id) {
			fail(404, "任务不存在。")
			return
		}
		if cancelling {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				fail(405, "不支持此操作。")
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
			if err != nil || len(body) != 0 {
				fail(400, "取消操作不接受请求内容。")
				return
			}
			run, err := s.Cancel(r.Context(), id)
			if errors.Is(err, sql.ErrNoRows) {
				fail(404, "任务不存在。")
				return
			}
			if err != nil {
				fail(503, "取消结果未确认，请查询任务后重试。")
				return
			}
			send(200, map[string]any{"run": run})
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			fail(405, "不支持此操作。")
			return
		}
		run, err := s.Get(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			fail(404, "任务不存在。")
			return
		}
		if err != nil {
			fail(503, "任务读取失败。")
			return
		}
		send(200, map[string]any{"run": run})
	}
}
