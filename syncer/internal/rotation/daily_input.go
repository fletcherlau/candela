package rotation

import (
	"context"
	"database/sql"
	"fmt"
	"syncer/internal/core"
	"time"
)

// Inputs are bounded before limiting, so old dates retain their complete window.
func (s *Service) closeInputs(ctx context.Context, tx *sql.Tx, date string) (map[string][]core.DailyBarAdj, map[string]float64, map[string]string, error) {
	panels := map[string][]core.DailyBarAdj{}
	prices := map[string]float64{}
	reasons := map[string]string{}
	for _, code := range core.RotationCodes {
		var through string
		err := tx.QueryRowContext(ctx, "SELECT through_date FROM rotation_coverage WHERE ts_code=?", code).Scan(&through)
		if err == sql.ErrNoRows || (err == nil && through < date) {
			reasons[code] = "日线与因子同步覆盖尚未完整"
			continue
		}
		if err != nil {
			return nil, nil, nil, err
		}
		rows, err := tx.QueryContext(ctx, `SELECT d.trade_date,d.open,d.high,d.low,d.close,
 (SELECT a.adj_factor FROM etf_adj_factor a WHERE a.ts_code=d.ts_code AND a.trade_date<=d.trade_date ORDER BY a.trade_date DESC LIMIT 1)
 FROM (SELECT * FROM etf_daily WHERE ts_code=? AND trade_date<=? ORDER BY trade_date DESC LIMIT ?) d ORDER BY d.trade_date`, code, date, s.quantileWindow()+20)
		if err != nil {
			return nil, nil, nil, err
		}
		var series []core.DailyBarAdj
		for rows.Next() {
			var d string
			var o, h, l, c, f sql.NullFloat64
			if err = rows.Scan(&d, &o, &h, &l, &c, &f); err != nil {
				break
			}
			if _, dateErr := time.Parse("20060102", d); dateErr != nil {
				reasons[code] = "历史行情日期无效，无法计算"
				continue
			}
			valid := o.Valid && h.Valid && l.Valid && c.Valid && f.Valid && o.Float64 > 0 && c.Float64 > 0 && l.Float64 > 0 && f.Float64 > 0 && h.Float64 >= o.Float64 && h.Float64 >= c.Float64 && l.Float64 <= o.Float64 && l.Float64 <= c.Float64
			if !valid {
				reasons[code] = "日线或复权因子缺失／无效"
				continue
			}
			if d == date {
				prices[code] = c.Float64
			}
			factor := f.Float64
			series = append(series, core.DailyBarAdj{TradeDate: d, Open: o.Float64 * factor, High: h.Float64 * factor, Low: l.Float64 * factor, Close: c.Float64 * factor, AdjFactor: 1})
		}
		if rows.Err() != nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, nil, nil, err
		}
		if _, ok := prices[code]; !ok {
			reasons[code] = "该交易日官方行情未到齐"
		}
		if reasons[code] == "" && len(series) > 0 {
			if err := s.ensureCalendar(ctx, series[0].TradeDate, date); err != nil {
				return nil, nil, nil, err
			}
			calendarRows, err := s.DB.QueryContext(ctx, "SELECT cal_date,is_open FROM rotation_calendar WHERE cal_date BETWEEN ? AND ?", series[0].TradeDate, date)
			if err != nil {
				return nil, nil, nil, err
			}
			seen := make(map[string]bool, len(series))
			for _, bar := range series {
				seen[bar.TradeDate] = true
			}
			for calendarRows.Next() {
				var d string
				var open bool
				if err = calendarRows.Scan(&d, &open); err != nil {
					break
				}
				if open && !seen[d] && !verifiedSuspension(code, d) {
					reasons[code] = fmt.Sprintf("%s 历史行情缺失，无法计算", d)
				}
				if !open && seen[d] {
					reasons[code] = "历史行情包含非交易日，无法计算"
				}
			}
			if calendarRows.Err() != nil {
				err = calendarRows.Err()
			}
			calendarRows.Close()
			if err != nil {
				return nil, nil, nil, err
			}
			if reasons[code] == "" {
				panels[code] = series
			}
		}
	}
	return panels, prices, reasons, nil
}
