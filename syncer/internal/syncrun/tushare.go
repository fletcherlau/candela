package syncrun

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tushare "github.com/fletcherlau/go-tushare"
)

const pageLimit = 1000
const dailyFields = "ts_code,trade_date,open,high,low,close,pre_close,change,pct_chg,vol,amount"

type TushareSource struct{ Client *tushare.Client }

func failure(code, message string) error { return &SourceError{code, message} }
func (s *TushareSource) query(ctx context.Context, api string, params map[string]interface{}, fields string) (*tushare.Response, error) {
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	params["limit"] = pageLimit
	resp, err := s.Client.QueryOne(api, params, fields, tushare.WithContext(bounded))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		// QueryOne turns exhausted 40203 retries into an untyped error. Only classify
		// that code; never persist upstream messages, tokens or request URLs.
		if strings.Contains(err.Error(), "code=40203") || strings.Contains(err.Error(), "status=429") {
			return nil, failure("rate_limited", "数据源调用频率受限，请稍后重新提交。")
		}
		return nil, failure("source_unavailable", "数据源请求失败或超时，请稍后重新提交。")
	}
	if resp == nil {
		return nil, failure("source_invalid", "数据源返回空响应。")
	}
	if !resp.IsSuccess() {
		if resp.Code == 40203 || strings.Contains(resp.Msg, "每分钟") || strings.Contains(resp.Msg, "频率") {
			return nil, failure("rate_limited", "数据源调用频率受限，请稍后重新提交。")
		}
		if strings.Contains(resp.Msg, "权限") || strings.Contains(resp.Msg, "积分") || strings.Contains(strings.ToLower(resp.Msg), "token") {
			return nil, failure("permission_denied", "数据源凭证或接口权限不足，请检查 Tushare 配置与指数日线权限。")
		}
		return nil, failure("source_rejected", "数据源拒绝查询，请检查接口配置。")
	}
	if resp.Data == nil {
		return nil, failure("source_invalid", "数据源返回缺少数据结构。")
	}
	// A success response without the requested shape must never look like an empty
	// pre-inception interval. Only source-optional daily metrics may be null;
	// Bar pointers preserve those nulls without silently storing zeros.
	fieldsSeen := map[string]bool{}
	for _, f := range resp.Data.Fields {
		fieldsSeen[f] = true
	}
	for _, f := range strings.Split(fields, ",") {
		if !fieldsSeen[f] {
			return nil, failure("source_invalid", "数据源返回字段不完整。")
		}
	}
	for _, row := range resp.Data.Items {
		if len(row) != len(resp.Data.Fields) {
			return nil, failure("source_invalid", "数据源返回记录不完整。")
		}
		for i, v := range row {
			field := resp.Data.Fields[i]
			if v == nil && !(api == "index_daily" && nullableDailyField(field)) {
				return nil, failure("source_invalid", fmt.Sprintf("数据源记录存在缺失值（%s.%s）。", api, field))
			}
		}
	}
	return resp, nil
}
func nullableDailyField(field string) bool {
	switch field {
	case "pre_close", "change", "pct_chg", "vol", "amount":
		return true
	}
	return false
}

func (s *TushareSource) daily(ctx context.Context, from, to string) ([]Bar, bool, error) {
	params := map[string]interface{}{"ts_code": Code, "end_date": to}
	if from != "" {
		params["start_date"] = from
	}
	resp, err := s.query(ctx, "index_daily", params, dailyFields)
	if err != nil {
		return nil, false, err
	}
	var bars []Bar
	if err := resp.ToStruct(&bars); err != nil {
		return nil, false, failure("source_invalid", "指数行情格式不正确。")
	}
	seen := map[string]bool{}
	for _, b := range bars {
		_, err := time.Parse("20060102", b.Date)
		if err != nil || b.Code != Code || b.Date < HistoryFloor || b.Date < from || b.Date > to || seen[b.Date] || b.Close <= 0 || b.Open <= 0 || b.High < b.Low || (b.Volume != nil && *b.Volume < 0) || (b.Amount != nil && *b.Amount < 0) {
			return nil, false, failure("source_invalid", "指数行情包含异常日期、重复记录或无效数值。")
		}
		seen[b.Date] = true
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
	return bars, resp.Data.HasMore || len(bars) >= pageLimit, nil
}

// Walk backward with a strict decreasing date boundary, independent of response
// ordering. Probe again even after a short page: only a successful empty response
// proves no earlier history. This avoids guessing inception from publication or
// base dates, and avoids offset pagination silently losing truncated history.
func (s *TushareSource) Earliest(ctx context.Context, end string) (string, string, error) {
	cutoff, earliest, pages := end, "", 0
	for {
		bars, full, err := s.daily(ctx, "", cutoff)
		if err != nil {
			return "", "", err
		}
		pages++
		if len(bars) == 0 {
			if full {
				return "", "", failure("source_truncated", "历史探测返回截断的空页，无法确认覆盖范围。")
			}
			if earliest == "" {
				return "", "", failure("source_empty", "截止日之前没有可用指数行情，不能将空结果视为回填成功。")
			}
			return earliest, fmt.Sprintf("index_daily 历史探测 %d 页；源端最早记录 %s；不设起点、截止 %s 的探测成功返回空集；本次截止 %s。", pages, earliest, cutoff, end), nil
		}
		earliest = bars[0].Date
		if earliest == HistoryFloor {
			return earliest, "源端记录到达完整日历范围起点 00010101。", nil
		}
		cutoff = date(earliest).AddDate(0, 0, -1).Format("20060102")
	}
}

// Split a capped response by date until completeness can be checked. Each public
// window is at most 366 calendar days, safely below the requested page limit.
func (s *TushareSource) Window(ctx context.Context, from, to string) ([]Bar, error) {
	bars, full, err := s.daily(ctx, from, to)
	if err != nil {
		return nil, err
	}
	if full {
		if from == to {
			return nil, failure("source_truncated", "单日行情仍被截断，任务已停止。")
		}
		mid := date(from).AddDate(0, 0, int(date(to).Sub(date(from)).Hours()/24)/2)
		left, err := s.Window(ctx, from, mid.Format("20060102"))
		if err != nil {
			return nil, err
		}
		right, err := s.Window(ctx, mid.AddDate(0, 0, 1).Format("20060102"), to)
		if err != nil {
			return nil, err
		}
		return append(left, right...), nil
	}
	resp, err := s.query(ctx, "trade_cal", map[string]interface{}{"exchange": "SSE", "start_date": from, "end_date": to}, "cal_date,is_open")
	if err != nil {
		return nil, err
	}
	if resp.Data.HasMore || len(resp.Data.Items) >= pageLimit {
		return nil, failure("source_truncated", "交易日历被截断，无法验证行情完整性。")
	}
	var days []struct {
		Date string `json:"cal_date"`
		Open int    `json:"is_open"`
	}
	if err = resp.ToStruct(&days); err != nil {
		return nil, failure("source_invalid", "交易日历格式不正确。")
	}
	calendar := map[string]int{}
	for _, d := range days {
		if _, ok := calendar[d.Date]; ok || d.Date < from || d.Date > to || (d.Open != 0 && d.Open != 1) {
			return nil, failure("source_invalid", "交易日历包含异常记录。")
		}
		calendar[d.Date] = d.Open
	}
	actual := map[string]bool{}
	for _, b := range bars {
		actual[b.Date] = true
	}
	for day := date(from); !day.After(date(to)); day = day.AddDate(0, 0, 1) {
		key := day.Format("20060102")
		open, ok := calendar[key]
		if !ok {
			return nil, failure("calendar_incomplete", "交易日历缺失，无法确认行情是否完整。")
		}
		if (open == 1) != actual[key] {
			return nil, failure("source_gap", "行情与交易日历不一致；存在缺失交易日或异常记录，该分段未提交。")
		}
	}
	return bars, nil
}
