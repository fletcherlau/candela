package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"syncer/internal/core"
	"time"
)

// ServeReferences handles saved captures independently of history backtesting.
// It never starts a daily collection or requests live quotes.
func (s *Service) ServeReferences(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := s.refreshReferences(ctx); err != nil && ctx.Err() == nil {
			log.Print("reference publication pending")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Publication uses only immutable saved inputs; page reads never invoke this.
func (s *Service) refreshReferences(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.trade_date FROM rotation_capture_run c
 WHERE c.state='captured' AND NOT EXISTS (SELECT 1 FROM rotation_daily d WHERE d.trade_date=c.trade_date AND d.basis='reference_1445') ORDER BY c.trade_date`)
	if err != nil {
		return err
	}
	var dates []string
	for rows.Next() {
		var date string
		if err = rows.Scan(&date); err != nil {
			break
		}
		dates = append(dates, date)
	}
	if rows.Err() != nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, date := range dates {
		if err = s.publishReference(ctx, date); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) publishReference(ctx context.Context, date string) error {
	return s.publishReferenceRecovery(ctx, date, nil)
}
func (s *Service) publishReferenceRecovery(ctx context.Context, date string, recovery *RecoveryRun) error {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, "SELECT state FROM rotation_capture_run WHERE trade_date=? FOR UPDATE", date).Scan(&state); err != nil {
		return err
	}
	if state != "captured" {
		return nil
	}
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM rotation_daily WHERE trade_date=? AND basis='reference_1445' AND payload IS NOT NULL", date).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	// Failed calculations have no published payload. The capture row lock
	// serializes recovery with the publisher; a first published reference is
	// immutable and was checked above. Replacing a failure never changes input.
	if _, err = tx.ExecContext(ctx, "DELETE FROM rotation_daily WHERE trade_date=? AND basis='reference_1445' AND payload IS NULL", date); err != nil {
		return err
	}
	run, err := readCaptureRun(ctx, tx, date)
	if err != nil {
		return err
	}
	if run.Params.Version != indicatorVersion || run.Params.QuantileWindow <= 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO rotation_daily(trade_date,basis,revision,status,available,message) VALUES (?,'reference_1445',0,'failed',?,'冻结参数版本不受当前计算器支持，参考未发布')`, date, run.Available)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE rotation_capture_run SET stage='reference_failed',message='原始数据已保存，冻结参数版本不受当前计算器支持，参考未发布',updated_at=UTC_TIMESTAMP(6) WHERE trade_date=?", date); err != nil {
			return err
		}
		return commitRecoveryPublication(ctx, tx, recovery)
	}
	panels := map[string][]core.DailyBarAdj{}
	prices := map[string]float64{}
	reasons := map[string]string{}
	for _, item := range run.Items {
		if item.Input == nil {
			return fmt.Errorf("captured input missing")
		}
		input := item.Input
		if input.Params != run.Params {
			return fmt.Errorf("frozen parameter identity mismatch")
		}
		prices[item.Code] = input.Quote.Latest
		panels[item.Code], reasons[item.Code] = referencePanel(date, input)
	}
	result := s.dailyResult(date, panels, prices, reasons, run.Params)
	result.Basis = "reference_1445"
	sources := []string{}
	seenSources := map[string]bool{}
	for _, item := range run.Items {
		source := item.Input.Quote.Source
		if !seenSources[source] {
			sources = append(sources, source)
			seenSources[source] = true
		}
	}
	result.Source = strings.Join(sources, "、") + " 固定 14:45 行情及已冻结历史依据"
	for i := range result.Cards {
		result.Cards[i].SourceTime = run.Items[i].Input.Quote.SourceTime.Format(time.RFC3339Nano)
		result.Cards[i].CapturedAt = run.Items[i].Input.CapturedAt.Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO rotation_daily(trade_date,basis,revision,status,available,message,payload,published_at) VALUES (?,'reference_1445',0,'ready',4,'固定 14:45 参考已发布',?,?)`, date, payload, s.now().UTC())
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE rotation_capture_run SET stage='reference_published',message='原始数据已保存，固定参考已发布',updated_at=UTC_TIMESTAMP(6) WHERE trade_date=?", date); err != nil {
		return err
	}
	return commitRecoveryPublication(ctx, tx, recovery)
}

// An invalid frozen history produces unknown indicators, never invented bars or
// a shorter percentile window. Raw reference prices retain their own evidence.
func referencePanel(date string, input *CaptureInput) ([]core.DailyBarAdj, string) {
	valid := func(v *float64) bool { return v != nil && finiteNumber(*v) != nil && *v > 0 }
	if !valid(input.Factor) || input.FactorDate > date {
		return nil, "冻结的当日复权因子缺失／无效"
	}
	calendar := map[string]bool{}
	suspended := map[string]bool{}
	for _, d := range input.Calendar {
		calendar[d.Date] = d.Open
		suspended[d.Date] = d.Suspended
	}
	previous := ""
	for d, open := range calendar {
		if open && d < date && d > previous {
			previous = d
		}
	}
	if previous == "" || input.CoverageThrough < previous {
		return nil, "冻结的历史同步覆盖不足，无法确认计算依据"
	}
	var series []core.DailyBarAdj
	seen := map[string]bool{}
	for _, bar := range input.History {
		if bar.Date >= date || seen[bar.Date] || (len(series) > 0 && bar.Date <= series[len(series)-1].TradeDate) {
			return nil, "冻结的历史日期不符合截至日顺序"
		}
		if !valid(bar.Open) || !valid(bar.High) || !valid(bar.Low) || !valid(bar.Close) || !valid(bar.Factor) || bar.FactorDate > bar.Date || *bar.High < *bar.Open || *bar.High < *bar.Close || *bar.Low > *bar.Open || *bar.Low > *bar.Close {
			return nil, "冻结的历史行情或因子缺失／无效"
		}
		seen[bar.Date] = true
		f := *bar.Factor
		series = append(series, core.DailyBarAdj{TradeDate: bar.Date, Open: *bar.Open * f, High: *bar.High * f, Low: *bar.Low * f, Close: *bar.Close * f, AdjFactor: 1})
	}
	if len(series) == 0 {
		return nil, "冻结的历史不足，无法计算"
	}
	for d := day(series[0].TradeDate); !d.After(day(date)); d = d.AddDate(0, 0, 1) {
		key := d.Format("20060102")
		open, ok := calendar[key]
		if !ok {
			return nil, "冻结的交易日历不完整，无法计算"
		}
		if key == date {
			if !open {
				return nil, "冻结的目标日期不是交易日"
			}
			continue
		}
		if open && !seen[key] && !suspended[key] {
			return nil, key + " 冻结的历史行情缺失，无法计算"
		}
		if !open && seen[key] {
			return nil, "冻结的历史包含非交易日行情"
		}
	}
	q, f := input.Quote, *input.Factor
	series = append(series, core.DailyBarAdj{TradeDate: date, Open: q.Open * f, High: q.High * f, Low: q.Low * f, Close: q.Latest * f, AdjFactor: 1})
	return series, ""
}
