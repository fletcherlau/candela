package rotation

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"time"
)

// Pointers preserve unknown values when reading older or incomplete publications.
// A range observes the continuous model; it never runs or initializes a strategy.
type RangeDay struct {
	Date       string     `json:"date"`
	NAV        *float64   `json:"nav"`
	Holding    *string    `json:"holding"`
	Weight     *float64   `json:"weight"`
	CashWeight *float64   `json:"cashWeight"`
	Cost       *float64   `json:"cost"`
	Turnover   *float64   `json:"turnover"`
	Benchmarks []*float64 `json:"benchmarks"`
	Suspended  []string   `json:"suspended,omitempty"`
}
type RangeResult struct {
	Version string     `json:"version"`
	Start   string     `json:"start"`
	End     string     `json:"end"`
	Codes   []string   `json:"codes"`
	Names   []string   `json:"names"`
	CostBPS *float64   `json:"costBps"`
	Days    []RangeDay `json:"days"`
}
type RangeWindow struct {
	Start       string       `json:"start"`
	End         string       `json:"end"`
	StartIndex  int          `json:"startIndex"`
	EndIndex    int          `json:"endIndex"`
	Returns     []*float64   `json:"returns"`
	DD          []*float64   `json:"dd"`
	Comparisons [][]*float64 `json:"comparisons"`
	Gain        *float64     `json:"gain"`
	MaxDD       *float64     `json:"maxdd"`
	Missing     bool         `json:"missing"`
}
type RangeView struct {
	Status        string       `json:"status"`
	Message       string       `json:"message"`
	UpdatedAt     string       `json:"updatedAt"`
	Revision      int64        `json:"revision"`
	Result        *RangeResult `json:"result"`
	Range         *RangeWindow `json:"range"`
	HoldingChange string       `json:"holdingChange"`
}

func (s *Service) RangeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "仅支持读取", 405)
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			http.Error(w, "查询参数无效", 400)
			return
		}
		for key, values := range query {
			if (key != "start" && key != "end") || len(values) != 1 || values[0] == "" {
				http.Error(w, "查询参数无效", 400)
				return
			}
		}
		start, end := query.Get("start"), query.Get("end")
		if (start == "") != (end == "") {
			http.Error(w, "请同时提供起止日期", 400)
			return
		}
		if start != "" {
			a, ea := time.Parse("20060102", start)
			b, eb := time.Parse("20060102", end)
			if ea != nil || eb != nil || a.After(b) || end > s.today() || b.After(a.AddDate(10, 0, 0)) {
				http.Error(w, "日期无效或观察区间超过十年", 400)
				return
			}
		}
		view := RangeView{Status: "unavailable", Message: "暂无已发布回测"}
		var payload []byte
		var updated time.Time
		err = s.DB.QueryRowContext(r.Context(), "SELECT status,message,payload,updated_at,revision FROM rotation_result WHERE id=1").Scan(&view.Status, &view.Message, &payload, &updated, &view.Revision)
		if err != nil && err != sql.ErrNoRows {
			http.Error(w, "回测服务暂不可用", 503)
			return
		}
		if !updated.IsZero() {
			view.UpdatedAt = updated.UTC().Format(time.RFC3339)
		}
		if len(payload) > 0 {
			if json.Unmarshal(payload, &view.Result) != nil {
				http.Error(w, "回测结果暂不可用", 503)
				return
			}
		}
		if view.Result != nil && len(view.Result.Days) > 0 {
			days := view.Result.Days
			for i, d := range days {
				if _, err := time.Parse("20060102", d.Date); err != nil || d.Date > s.today() || (i > 0 && days[i-1].Date >= d.Date) {
					http.Error(w, "回测日期序列无效", 503)
					return
				}
			}
			if start == "" {
				end = days[len(days)-1].Date
				start = day(end).AddDate(-1, 0, 0).Format("20060102")
			}
			lo := sort.Search(len(days), func(i int) bool { return days[i].Date >= start })
			hi := sort.Search(len(days), func(i int) bool { return days[i].Date > end })
			view.Range = buildRange(days[lo:hi], lo, len(view.Result.Codes))
			view.HoldingChange = rangeHoldingChange(view.Result)
			if view.Status == "ready" {
				now := s.now().In(time.FixedZone("Asia/Shanghai", 8*3600))
				if now.Hour() < 18 {
					now = now.AddDate(0, 0, -1)
				}
				cutoff := now.Format("20060102")
				var expected, coverage sql.NullString
				if err := s.DB.QueryRowContext(r.Context(), "SELECT MAX(CASE WHEN is_open=1 AND cal_date<=? THEN cal_date END),MAX(cal_date) FROM rotation_calendar", cutoff).Scan(&expected, &coverage); err != nil {
					http.Error(w, "交易日历暂不可用", 503)
					return
				}
				if !coverage.Valid || coverage.String < cutoff {
					view.Status = "stale"
					view.Message = "交易日历尚未更新，显示最近完整结果"
				} else if expected.Valid && days[len(days)-1].Date < expected.String {
					view.Status = "stale"
					view.Message = "行情尚未更新至最新交易日，显示最近完整结果"
				}
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(view)
		}
	})
}

func knownNumber(v *float64) bool { return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) }
func positive(v *float64) bool    { return knownNumber(v) && *v > 0 }
func buildRange(days []RangeDay, lo, count int) *RangeWindow {
	result := &RangeWindow{StartIndex: lo, EndIndex: lo + len(days) - 1, Returns: []*float64{}, DD: []*float64{}, Comparisons: make([][]*float64, count)}
	for j := range result.Comparisons {
		result.Comparisons[j] = []*float64{}
	}
	if len(days) == 0 {
		return result
	}
	result.Start, result.End = days[0].Date, days[len(days)-1].Date
	base := days[0].NAV
	if lo == 0 {
		one := 1.0
		base = &one
	}
	peak := base
	complete := positive(base)
	maxdd := 0.0
	for _, d := range days {
		var gain, drawdown *float64
		if positive(base) && positive(d.NAV) {
			gain = finiteNumber(*d.NAV / *base - 1)
		}
		if !positive(d.NAV) {
			complete = false
		}
		if complete {
			nextPeak := math.Max(*peak, *d.NAV)
			peak = &nextPeak
			drawdown = finiteNumber(*d.NAV / *peak - 1)
			maxdd = math.Min(maxdd, *drawdown)
		}
		result.Returns = append(result.Returns, gain)
		result.DD = append(result.DD, drawdown)
		if !positive(d.NAV) || !knownNumber(d.Weight) || !knownNumber(d.CashWeight) || d.Holding == nil {
			result.Missing = true
		}
		for j := 0; j < count; j++ {
			var comparison *float64
			if j < len(days[0].Benchmarks) && j < len(d.Benchmarks) && positive(days[0].Benchmarks[j]) && positive(d.Benchmarks[j]) {
				comparison = finiteNumber(*d.Benchmarks[j] / *days[0].Benchmarks[j] - 1)
			}
			result.Comparisons[j] = append(result.Comparisons[j], comparison)
			if j >= len(d.Benchmarks) || !knownNumber(d.Benchmarks[j]) {
				result.Missing = true
			}
		}
	}
	if len(days) > 1 {
		result.Gain = result.Returns[len(days)-1]
		if complete {
			result.MaxDD = &maxdd
		}
	}
	return result
}
func rangeHoldingChange(result *RangeResult) string {
	days := result.Days
	if len(days) < 2 {
		return "前后交易日字段不足，无法比较"
	}
	previous, current := days[len(days)-2], days[len(days)-1]
	for _, d := range []RangeDay{previous, current} {
		if d.Holding == nil || !knownNumber(d.Weight) || !knownNumber(d.CashWeight) {
			return "前后交易日字段不足，无法比较"
		}
	}
	if *previous.Holding != *current.Holding {
		return rangeHoldingName(previous, result) + " → " + rangeHoldingName(current, result)
	}
	delta := *current.Weight - *previous.Weight
	if math.Abs(delta) < 1e-10 && math.Abs(*current.CashWeight-*previous.CashWeight) < 1e-10 {
		return "标的与收盘权重相同"
	}
	return fmt.Sprintf("标的相同；权重变化 %+.2f 个百分点", delta*100)
}
func rangeHoldingName(d RangeDay, result *RangeResult) string {
	if d.Holding == nil {
		return "—"
	}
	if *d.Holding == "" {
		if knownNumber(d.Weight) && knownNumber(d.CashWeight) && *d.Weight == 0 && *d.CashWeight == 1 {
			return "现金"
		}
		return "—"
	}
	for i, code := range result.Codes {
		if code == *d.Holding && i < len(result.Names) && result.Names[i] != "" {
			return result.Names[i]
		}
	}
	return *d.Holding
}
