package rotation

import (
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
		v, err := s.dailyView(r.Context(), date)
		if err != nil {
			http.Error(w, "每日数据暂不可用", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(v)
		}
	})
}
