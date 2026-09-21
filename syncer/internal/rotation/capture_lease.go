package rotation

import (
	"context"
	"database/sql"
	"errors"
)

var errCaptureOwnership = errors.New("capture execution ownership expired")

// A target row is the execution lock. As in Sync Run, database-time leases and
// increasing owners fence every checkpoint, including an external call returning late.
func (s *Service) claimCapture(ctx context.Context) (CaptureRun, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return CaptureRun{}, err
	}
	defer tx.Rollback()
	run, err := scanCapture(tx.QueryRowContext(ctx, "SELECT "+captureColumns+" FROM rotation_capture_run WHERE state='queued' OR (state='running' AND lease_until<=UTC_TIMESTAMP(6)) ORDER BY trade_date LIMIT 1 FOR UPDATE SKIP LOCKED"))
	if err != nil {
		return CaptureRun{}, err
	}
	recovered := run.State == "running" || run.Stage == "recovering"
	if recovered {
		run.Recoveries++
	}
	run.Owner++
	run.State = "running"
	run.Stage = "capturing"
	run.Message = "正在采集固定 14:45 原始数据"
	if recovered {
		run.Message = "从原目标恢复采集，已保存输入保持不变"
	}
	_, err = tx.ExecContext(ctx, `UPDATE rotation_capture_run SET owner=?,state='running',stage='capturing',message=?,recoveries=?,lease_until=TIMESTAMPADD(SECOND,30,UTC_TIMESTAMP(6)),updated_at=UTC_TIMESTAMP(6) WHERE trade_date=?`, run.Owner, run.Message, run.Recoveries, run.TradeDate)
	if err != nil {
		return CaptureRun{}, err
	}
	return run, tx.Commit()
}
func (s *Service) captureOwned(ctx context.Context, run CaptureRun, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner int64
	var alive bool
	var deadline sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT owner,state='running' AND lease_until>UTC_TIMESTAMP(6),DATE_FORMAT(lease_until,'%Y-%m-%d %H:%i:%s.%f') FROM rotation_capture_run WHERE trade_date=? FOR UPDATE`, run.TradeDate).Scan(&owner, &alive, &deadline)
	if err != nil {
		return err
	}
	if !alive || owner != run.Owner {
		return errCaptureOwnership
	}
	if err = fn(tx); err != nil {
		return err
	}
	var valid bool
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)<CAST(? AS DATETIME(6))", deadline).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return errCaptureOwnership
	}
	return tx.Commit()
}
func (s *Service) resumeCapture(ctx context.Context, run CaptureRun) error {
	return s.captureOwned(ctx, run, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE rotation_capture_run SET state='queued',stage='recovering',message='采集执行中断，等待按原目标恢复',lease_until=NULL,updated_at=UTC_TIMESTAMP(6) WHERE trade_date=?`, run.TradeDate)
		return err
	})
}
