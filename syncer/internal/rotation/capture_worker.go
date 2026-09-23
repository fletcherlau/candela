package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"math"
	"syncer/internal/core"
	"time"
)

// ServeCaptures drains already accepted targets only. The daily 14:45 trigger
// remains external cron, independent of this recovery loop and browser polling.
func (s *Service) ServeCaptures(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		run, err := s.claimCapture(ctx)
		if err == nil {
			s.executeCapture(ctx, run)
		} else if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
			log.Print("reference capture queue unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) executeCapture(ctx context.Context, run CaptureRun) {
	task, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-task.Done():
				return
			case <-ticker.C:
				beat, stop := context.WithTimeout(task, 5*time.Second)
				err := s.captureOwned(beat, run, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(beat, "UPDATE rotation_capture_run SET lease_until=TIMESTAMPADD(SECOND,30,UTC_TIMESTAMP(6)) WHERE trade_date=?", run.TradeDate)
					return err
				})
				stop()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	err := s.captureObjects(task, run)
	cancel()
	<-done
	if err != nil {
		finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		// A failed/expired owner cannot requeue over a newer owner. A current owner
		// preserves every saved input, and recovery uses the same target next time.
		_ = s.resumeCapture(finish, run)
	}
}
func (s *Service) captureObjects(ctx context.Context, run CaptureRun) error {
	target, err := time.Parse(time.RFC3339, run.TargetAt)
	if err != nil {
		return err
	}
	deadline, err := time.Parse(time.RFC3339, run.DeadlineAt)
	if err != nil {
		return err
	}
	for _, code := range core.RotationCodes {
		saved := false
		if err = s.captureOwned(ctx, run, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, "SELECT payload IS NOT NULL FROM rotation_reference_input WHERE trade_date=? AND ts_code=?", run.TradeDate, code).Scan(&saved)
		}); err != nil {
			return err
		}
		if saved {
			continue
		}
		requested := s.now()
		received := requested
		reason := ""
		var quote core.RealtimeQuote
		if requested.Before(target) || !requested.Before(deadline) {
			reason = "已错过原 14:45 采集窗口，当前接入无法补取原时点数据"
		} else if s.Realtime == nil {
			reason = "实时行情源未配置，原时点数据未留存"
		} else {
			timeout := 10 * time.Second
			if remaining := deadline.Sub(requested); remaining < timeout {
				timeout = remaining
			}
			call, stop := context.WithTimeout(ctx, timeout)
			quotes, fetchErr := s.Realtime.FetchRealtime(call, []string{code})
			stop()
			received = s.now()
			if fetchErr != nil {
				reason = "原时点行情获取失败，未留存；当前接入不支持历史时点补取"
			} else if len(quotes) != 1 || quotes[0].TsCode != code {
				reason = "行情返回对象不完整或与原任务不一致"
			} else {
				quote = quotes[0]
				reason = captureQuoteReason(run, quote, requested, received)
			}
		}
		if err = s.captureOwned(ctx, run, func(tx *sql.Tx) error {
			var payload []byte
			state := "missing"
			if reason == "" {
				input, err := s.freezeCaptureInput(ctx, tx, run, quote, requested, received)
				if err != nil {
					return err
				}
				payload, err = json.Marshal(input)
				if err != nil {
					return err
				}
				state = "captured"
			}
			if _, err := tx.ExecContext(ctx, "UPDATE rotation_reference_input SET state=?,reason=?,payload=? WHERE trade_date=? AND ts_code=? AND payload IS NULL", state, reason, payload, run.TradeDate, code); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE rotation_capture_run SET available=(SELECT COUNT(*) FROM rotation_reference_input WHERE trade_date=? AND payload IS NOT NULL),updated_at=UTC_TIMESTAMP(6) WHERE trade_date=?`, run.TradeDate, run.TradeDate)
			return err
		}); err != nil {
			return err
		}
	}
	return s.captureOwned(ctx, run, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT available FROM rotation_capture_run WHERE trade_date=?", run.TradeDate).Scan(&count); err != nil {
			return err
		}
		state, stage, message := "missing", "inputs_missing", "原 14:45 数据未留存，当前接入无法补取"
		if count == 4 {
			state, stage, message = "captured", "awaiting_calculation", "四标的原始数据采集完成，参考待计算"
		} else if count > 0 {
			state, stage, message = "partial", "inputs_incomplete", "仅部分原始数据已留存，不能发布整组参考"
		}
		_, err := tx.ExecContext(ctx, "UPDATE rotation_capture_run SET state=?,stage=?,message=?,lease_until=NULL,updated_at=UTC_TIMESTAMP(6) WHERE trade_date=?", state, stage, message, run.TradeDate)
		return err
	})
}
func captureQuoteReason(run CaptureRun, q core.RealtimeQuote, requested, received time.Time) string {
	target, _ := time.Parse(time.RFC3339, run.TargetAt)
	deadline, _ := time.Parse(time.RFC3339, run.DeadlineAt)
	if requested.Before(target) || !requested.Before(deadline) || received.Before(requested) || !received.Before(deadline) {
		return "实际采集未在原 14:45 窗口内完成，不能冒充固定参考"
	}
	if q.Source == "" || q.SourceTime.IsZero() || q.TradeDate != run.TradeDate || q.SourceTime.In(shanghai).Format("20060102") != run.TradeDate || q.SourceTime.Before(target) || !q.SourceTime.Before(deadline) || q.SourceTime.After(received) {
		return "来源完整时点缺失、错日或不属于原 14:45 采集窗口"
	}
	for _, v := range []float64{q.Open, q.High, q.Low, q.Latest} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			return "原始行情含无效价格，不能留存为有效参考"
		}
	}
	if q.High < q.Open || q.High < q.Latest || q.Low > q.Open || q.Low > q.Latest {
		return "原始行情高低价不一致，不能留存为有效参考"
	}
	return ""
}
