package main

import (
	"context"
	"fmt"
	tushare "github.com/fletcherlau/go-tushare"
	"time"
)

type rotationCalendar struct{ client *tushare.Client }

func (s *rotationCalendar) Calendar(ctx context.Context, from, to string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	resp, err := s.client.QueryOne("trade_cal", map[string]interface{}{"exchange": "SSE", "start_date": from, "end_date": to}, "cal_date,is_open", tushare.WithContext(ctx))
	if err != nil || resp == nil || !resp.IsSuccess() || resp.Data == nil || resp.Data.HasMore {
		return nil, fmt.Errorf("交易日历源不可用")
	}
	var rows []struct {
		Date string `json:"cal_date"`
		Open int    `json:"is_open"`
	}
	if resp.ToStruct(&rows) != nil {
		return nil, fmt.Errorf("交易日历无效")
	}
	out := map[string]bool{}
	for _, r := range rows {
		if _, ok := out[r.Date]; ok || r.Date < from || r.Date > to || (r.Open != 0 && r.Open != 1) {
			return nil, fmt.Errorf("交易日历无效")
		}
		out[r.Date] = r.Open == 1
	}
	return out, nil
}
