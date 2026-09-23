package syncrun

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"math"
	"syncer/internal/core"
	"time"
)

func (s *ETFService) Serve(ctx context.Context) {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	worker := &Worker{Store: s.Store}
	for ctx.Err() == nil {
		run, err := s.claimETF(ctx)
		if err == nil {
			worker.executeWith(ctx, run, s.executeETF)
			continue
		}
		if err != sql.ErrNoRows && ctx.Err() == nil {
			log.Print("ETF queue unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
func (s *ETFService) claimETF(ctx context.Context) (Run, error) {
	rows, err := s.Store.db.QueryContext(ctx, `SELECT o.ts_code FROM etf_sync_object o WHERE active_run IS NOT NULL OR EXISTS (SELECT 1 FROM etf_sync_run r WHERE r.ts_code=o.ts_code AND state='queued') ORDER BY ts_code`)
	if err != nil {
		return Run{}, err
	}
	var codes []string
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			break
		}
		codes = append(codes, code)
	}
	scanErr := rows.Err()
	rows.Close()
	if err != nil {
		return Run{}, err
	}
	if scanErr != nil {
		return Run{}, scanErr
	}
	for _, code := range codes {
		r, err := s.Store.claimCode(ctx, code)
		if err == nil {
			return r, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return r, err
		}
	}
	return Run{}, sql.ErrNoRows
}
func (s *ETFService) executeETF(ctx context.Context, r Run) error {
	p, err := progress(s.Store.db.QueryRowContext(ctx, "SELECT "+etfProgressColumns+" FROM etf_sync_progress WHERE run_id=?", r.ID))
	if err != nil {
		return err
	}
	if p.ChunkDays < 1 || p.ChunkDays > 366 {
		return &SourceError{"invalid_plan", "保存的同步分段参数无效。"}
	}
	for _, stage := range []string{"daily", "adj"} {
		start, checkpoint := p.DailyStart, p.DailyCheckpoint
		if stage == "adj" {
			start, checkpoint = p.AdjStart, p.AdjCheckpoint
		}
		if checkpoint != "" {
			start = date(checkpoint).AddDate(0, 0, 1).Format("20060102")
		}
		for from := date(start); !from.After(date(r.EndDate)); {
			to := from.AddDate(0, 0, p.ChunkDays-1)
			if to.After(date(r.EndDate)) {
				to = date(r.EndDate)
			}
			if err := s.Store.owned(ctx, r, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "UPDATE etf_sync_run SET stage=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?", stage, r.ID)
				return err
			}); err != nil {
				return err
			}
			var bars []core.Bar
			var factors []core.AdjFactor
			if stage == "daily" {
				bars, err = s.Source.FetchDaily(ctx, r.Code, from.Format("20060102"), to.Format("20060102"))
			} else {
				factors, err = s.Source.FetchAdj(ctx, r.Code, from.Format("20060102"), to.Format("20060102"))
			}
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return &SourceError{"source_" + stage, "获取原始数据失败；已完成步骤保留，可按原范围重试。"}
			}
			if err = s.commitETF(ctx, r, stage, from.Format("20060102"), to.Format("20060102"), bars, factors); err != nil {
				return err
			}
			from = to.AddDate(0, 0, 1)
		}
	}
	return nil
}
func validETFNumber(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func (s *ETFService) commitETF(ctx context.Context, r Run, stage, from, to string, bars []core.Bar, factors []core.AdjFactor) error {
	seen := map[string]bool{}
	validDate := func(code, day string) bool {
		_, err := time.Parse("20060102", day)
		if err != nil || code != r.Code || day < from || day > to || seen[day] {
			return false
		}
		seen[day] = true
		return true
	}
	for _, b := range bars {
		if !validDate(b.TsCode, b.TradeDate) || b.Open <= 0 || b.High <= 0 || b.Low <= 0 || b.Close <= 0 || b.Low > b.High || b.High < math.Max(b.Open, b.Close) || b.Low > math.Min(b.Open, b.Close) {
			return &SourceError{"source_invalid", "行情日期、标的或价格无效，未提交该分段。"}
		}
		for _, n := range []float64{b.Open, b.High, b.Low, b.Close, b.PreClose, b.ChangeAmt, b.PctChg, b.Vol, b.Amount} {
			if !validETFNumber(n) {
				return &SourceError{"source_invalid", "行情包含无效数值，未提交该分段。"}
			}
		}
	}
	for _, f := range factors {
		if !validDate(f.TsCode, f.TradeDate) || f.AdjFactor <= 0 || !validETFNumber(f.AdjFactor) {
			return &SourceError{"source_invalid", "复权因子无效，未提交该分段。"}
		}
	}
	return s.Store.owned(ctx, r, func(tx *sql.Tx) error {
		p, err := progress(tx.QueryRowContext(ctx, "SELECT "+etfProgressColumns+" FROM etf_sync_progress WHERE run_id=?", r.ID))
		if err != nil {
			return err
		}
		start, checkpoint := p.DailyStart, p.DailyCheckpoint
		if stage == "adj" {
			start, checkpoint = p.AdjStart, p.AdjCheckpoint
		}
		next := start
		if checkpoint != "" {
			next = date(checkpoint).AddDate(0, 0, 1).Format("20060102")
		}
		if from != next || to < from || to > r.EndDate {
			return errors.New("non-contiguous ETF checkpoint")
		}
		for _, b := range bars {
			_, err = tx.ExecContext(ctx, `INSERT INTO etf_daily(ts_code,trade_date,open,high,low,close,pre_close,change_amt,pct_chg,vol,amount) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE open=VALUES(open),high=VALUES(high),low=VALUES(low),close=VALUES(close),pre_close=VALUES(pre_close),change_amt=VALUES(change_amt),pct_chg=VALUES(pct_chg),vol=VALUES(vol),amount=VALUES(amount)`, b.TsCode, b.TradeDate, b.Open, b.High, b.Low, b.Close, b.PreClose, b.ChangeAmt, b.PctChg, b.Vol, b.Amount)
			if err != nil {
				return err
			}
		}
		for _, f := range factors {
			if _, err = tx.ExecContext(ctx, `INSERT INTO etf_adj_factor(ts_code,trade_date,adj_factor) VALUES (?,?,?) ON DUPLICATE KEY UPDATE adj_factor=VALUES(adj_factor)`, f.TsCode, f.TradeDate, f.AdjFactor); err != nil {
				return err
			}
		}
		// Stage names are internal constants, never caller-provided SQL identifiers.
		if _, err = tx.ExecContext(ctx, "UPDATE etf_sync_progress SET "+stage+"_checkpoint=?,"+stage+"_rows="+stage+"_rows+? WHERE run_id=?", to, len(bars)+len(factors), r.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE etf_sync_run SET processed_rows=processed_rows+?,completed_segments=completed_segments+1,checkpoint=IF(completed_segments=total_segments,end_date,checkpoint),updated_at=UTC_TIMESTAMP(6) WHERE id=?`, len(bars)+len(factors), r.ID); err != nil {
			return err
		}
		if rotationCode(r.Code) {
			_, err = tx.ExecContext(ctx, "UPDATE rotation_result SET revision=revision+1,status='syncing',message='原始数据同步中，保留上一套完整结果' WHERE id=1")
		}
		return err
	})
}
