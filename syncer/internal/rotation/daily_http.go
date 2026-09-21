package rotation

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// DailyHandler reads saved publications only. It never fetches or computes data.
func (s *Service) DailyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "仅支持读取", 405)
			return
		}
		query, queryErr := url.ParseQuery(r.URL.RawQuery)
		if queryErr != nil {
			http.Error(w, "查询参数无效", 400)
			return
		}
		date := query.Get("tradeDate")
		for key, values := range query {
			if key != "tradeDate" || len(values) != 1 || values[0] == "" {
				http.Error(w, "查询参数无效", 400)
				return
			}
		}
		if date != "" {
			if _, err := time.Parse("20060102", date); err != nil || date > s.today() {
				http.Error(w, "交易日无效", 400)
				return
			}
		}
		if date == "" {
			var latest sql.NullString
			if err := s.DB.QueryRowContext(r.Context(), "SELECT MAX(trade_date) FROM rotation_daily WHERE basis='close' AND trade_date<=?", s.today()).Scan(&latest); err != nil {
				http.Error(w, "每日数据暂不可用", 503)
				return
			}
			date = latest.String
		}
		v := DailyView{TradeDate: date, Status: "unavailable", Message: "该交易日尚无已发布数据", ReferenceStatus: "missing"}
		var payload []byte
		var revision, current int64
		err := s.DB.QueryRowContext(r.Context(), `SELECT d.status,d.available,d.message,d.payload,d.revision,r.revision FROM rotation_daily d CROSS JOIN rotation_result r
 WHERE d.trade_date=? AND d.basis='close' AND r.id=1`, date).Scan(&v.Status, &v.Available, &v.Message, &payload, &revision, &current)
		if err != nil && err != sql.ErrNoRows {
			http.Error(w, "每日数据暂不可用", 503)
			return
		}
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &v.Close); err != nil {
				http.Error(w, "每日结果暂不可用", 503)
				return
			}
		}
		if err == nil && current != revision {
			v.Status = "updating"
			v.Message = "数据正在更新，等待整组计算完成"
			if v.Close != nil {
				v.Message = "数据正在更新，保留已发布完整结果"
			}
		}
		var reference []byte
		refErr := s.DB.QueryRowContext(r.Context(), "SELECT status,payload FROM rotation_daily WHERE trade_date=? AND basis='reference_1445'", date).Scan(&v.ReferenceStatus, &reference)
		if refErr != nil && refErr != sql.ErrNoRows {
			http.Error(w, "参考数据暂不可用", 503)
			return
		}
		if len(reference) > 0 {
			if json.Unmarshal(reference, &v.Reference) != nil {
				http.Error(w, "参考结果暂不可用", 503)
				return
			}
			if v.Close == nil {
				v.Status = "ready"
				v.Message = "固定 14:45 参考已发布"
				v.Available = v.Reference.Available
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(v)
		}
	})
}
