package rotation

import (
	"context"
	"database/sql"
	"syncer/internal/core"
	"time"
)

func nullableFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return finiteNumber(v.Float64)
}

// Freeze raw values and their exact as-of factor/calendar evidence in the same
// repeatable-read transaction as the first successful quote. No source calls,
// current-date LIMIT-then-filter, or future factors are involved.
func (s *Service) freezeCaptureInput(ctx context.Context, tx *sql.Tx, run CaptureRun, q core.RealtimeQuote, requested, received time.Time) (CaptureInput, error) {
	input := CaptureInput{Quote: q, RequestedAt: requested, CapturedAt: received, Params: run.Params, History: []FrozenBar{}, Calendar: []FrozenCalendarDay{}}
	var coverage sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT MAX(through_date) FROM rotation_coverage WHERE ts_code=?", q.TsCode).Scan(&coverage); err != nil {
		return input, err
	}
	input.CoverageThrough = coverage.String
	var factor sql.NullFloat64
	var factorDate sql.NullString
	err := tx.QueryRowContext(ctx, "SELECT adj_factor,trade_date FROM etf_adj_factor WHERE ts_code=? AND trade_date<=? ORDER BY trade_date DESC LIMIT 1", q.TsCode, run.TradeDate).Scan(&factor, &factorDate)
	if err != nil && err != sql.ErrNoRows {
		return input, err
	}
	input.Factor = nullableFloat(factor)
	input.FactorDate = factorDate.String
	rows, err := tx.QueryContext(ctx, `SELECT d.trade_date,d.open,d.high,d.low,d.close,a.adj_factor,a.trade_date
 FROM (SELECT * FROM etf_daily WHERE ts_code=? AND trade_date<? ORDER BY trade_date DESC LIMIT ?) d
 LEFT JOIN etf_adj_factor a ON a.ts_code=d.ts_code AND a.trade_date=(SELECT MAX(f.trade_date) FROM etf_adj_factor f WHERE f.ts_code=d.ts_code AND f.trade_date<=d.trade_date)
 ORDER BY d.trade_date`, q.TsCode, run.TradeDate, run.Params.QuantileWindow+19)
	if err != nil {
		return input, err
	}
	for rows.Next() {
		var bar FrozenBar
		var o, h, l, c, f sql.NullFloat64
		var fd sql.NullString
		if err = rows.Scan(&bar.Date, &o, &h, &l, &c, &f, &fd); err != nil {
			break
		}
		bar.Open = nullableFloat(o)
		bar.High = nullableFloat(h)
		bar.Low = nullableFloat(l)
		bar.Close = nullableFloat(c)
		bar.Factor = nullableFloat(f)
		bar.FactorDate = fd.String
		input.History = append(input.History, bar)
	}
	if rows.Err() != nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return input, err
	}
	from := run.TradeDate
	if len(input.History) > 0 {
		from = input.History[0].Date
	}
	rows, err = tx.QueryContext(ctx, "SELECT cal_date,is_open FROM rotation_calendar WHERE cal_date BETWEEN ? AND ? ORDER BY cal_date", from, run.TradeDate)
	if err != nil {
		return input, err
	}
	defer rows.Close()
	for rows.Next() {
		var day FrozenCalendarDay
		if err = rows.Scan(&day.Date, &day.Open); err != nil {
			return input, err
		}
		day.Suspended = day.Open && verifiedSuspension(q.TsCode, day.Date)
		input.Calendar = append(input.Calendar, day)
	}
	return input, rows.Err()
}
