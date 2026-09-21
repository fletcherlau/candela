package syncrun

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/zeromicro/go-zero/rest"
	"io"
	"net/http"
	"strings"
)

const etfPath = "/api/v1/data/etf-syncs"

func (s *ETFService) Routes(auth func(http.HandlerFunc) http.HandlerFunc) []rest.Route {
	handler := auth(s.etfHandler)
	return []rest.Route{
		{Method: "GET", Path: etfPath, Handler: handler}, {Method: "POST", Path: etfPath, Handler: handler},
		{Method: "GET", Path: etfPath + "/:id", Handler: handler},
		{Method: "POST", Path: etfPath + "/:id/retry", Handler: handler}, {Method: "POST", Path: etfPath + "/:id/cancel", Handler: handler},
	}
}
func (s *ETFService) etfHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	send := func(status int, value any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
	fail := func(err error) {
		var invalid *etfRequestError
		switch {
		case errors.As(err, &invalid):
			send(invalid.status, map[string]string{"error": invalid.message})
		case errors.Is(err, sql.ErrNoRows):
			send(404, map[string]string{"error": "批次不存在。"})
		default:
			send(503, map[string]string{"error": "执行结果未确认，请查询批次后重试。"})
		}
	}
	if r.URL.RawQuery != "" {
		send(400, map[string]string{"error": "不支持查询参数。"})
		return
	}
	if r.URL.Path == etfPath {
		if r.Method == "GET" {
			b, err := s.List(r.Context())
			if err != nil {
				fail(err)
				return
			}
			send(200, map[string]any{"batches": b})
			return
		}
		if r.Method != "POST" {
			send(405, map[string]string{"error": "不支持此操作。"})
			return
		}
		var input ETFRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			send(400, map[string]string{"error": "请求格式不正确。"})
			return
		}
		b, duplicate, err := s.SubmitRequest(r.Context(), input)
		if err != nil {
			fail(err)
			return
		}
		status := 202
		if duplicate {
			status = 200
		}
		send(status, map[string]any{"batch": b, "deduplicated": duplicate})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, etfPath+"/"), "/")
	if len(parts) > 2 || !ValidID(parts[0]) {
		send(404, map[string]string{"error": "批次不存在。"})
		return
	}
	if len(parts) == 1 && r.Method == "GET" {
		b, err := s.Get(r.Context(), parts[0])
		if err != nil {
			fail(err)
			return
		}
		send(200, map[string]any{"batch": b})
		return
	}
	if len(parts) != 2 || r.Method != "POST" {
		send(405, map[string]string{"error": "不支持此操作。"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if err != nil || len(body) != 0 {
		send(400, map[string]string{"error": "恢复／取消只接受原任务标识。"})
		return
	}
	switch parts[1] {
	case "retry":
		b, duplicate, err := s.Retry(r.Context(), parts[0])
		if err != nil {
			fail(err)
			return
		}
		status := 202
		if duplicate {
			status = 200
		}
		send(status, map[string]any{"batch": b, "deduplicated": duplicate})
	case "cancel":
		b, err := s.Cancel(r.Context(), parts[0])
		if err != nil {
			fail(err)
			return
		}
		send(200, map[string]any{"batch": b})
	default:
		send(404, map[string]string{"error": "批次不存在。"})
	}
}
