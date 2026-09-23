package rotation

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/zeromicro/go-zero/rest"
	"io"
	"net/http"
	"strings"
)

const capturePath = "/api/v1/rotation/reference-captures"

func (s *Service) CaptureRoutes(auth func(http.HandlerFunc) http.HandlerFunc) []rest.Route {
	handler := auth(s.captureHandler)
	return []rest.Route{
		{Method: http.MethodGet, Path: capturePath, Handler: handler},
		{Method: http.MethodPost, Path: capturePath, Handler: handler},
		{Method: http.MethodGet, Path: capturePath + "/:date", Handler: handler},
	}
}
func (s *Service) captureHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	send := func(status int, value any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
	fail := func(status int, message string) { send(status, map[string]string{"error": message}) }
	if r.URL.RawQuery != "" {
		fail(400, "不支持查询参数。")
		return
	}
	if r.URL.Path == capturePath {
		if r.Method == http.MethodGet {
			runs, err := s.captureList(r.Context())
			if err != nil {
				fail(503, "采集任务列表读取失败。")
				return
			}
			send(200, map[string]any{"runs": runs})
			return
		}
		if r.Method == http.MethodPost {
			var input struct {
				TradeDate string `json:"tradeDate"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
				fail(400, "请求格式不正确。")
				return
			}
			run, duplicate, err := s.submitCapture(r.Context(), input.TradeDate)
			var invalid *captureRequestError
			if errors.As(err, &invalid) {
				fail(invalid.status, invalid.message)
				return
			}
			if err != nil {
				fail(503, "采集任务接受情况未确认，请先查询记录。")
				return
			}
			status := 202
			if duplicate {
				status = 200
			}
			send(status, map[string]any{"run": run, "deduplicated": duplicate})
			return
		}
		w.Header().Set("Allow", "GET, POST")
		fail(405, "不支持此操作。")
		return
	}
	date := strings.TrimPrefix(r.URL.Path, capturePath+"/")
	if _, err := captureTarget(date); err != nil || date > s.today() {
		fail(400, "交易日无效。")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		fail(405, "仅支持读取。")
		return
	}
	run, err := s.captureRun(r.Context(), date)
	if errors.Is(err, sql.ErrNoRows) {
		fail(404, "该交易日尚无采集记录。")
		return
	}
	if err != nil {
		fail(503, "采集任务读取失败。")
		return
	}
	send(200, map[string]any{"run": run})
}
