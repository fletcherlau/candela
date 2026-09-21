package rotation

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"
)

var errRecoveryOwnership = errors.New("recovery execution ownership expired")

func (s *Service) claimRecovery(ctx context.Context) (RecoveryRun, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return RecoveryRun{}, err
	}
	defer tx.Rollback()
	run, err := scanRecovery(tx.QueryRowContext(ctx, "SELECT "+recoveryColumns+" FROM rotation_recovery_run WHERE state='queued' OR (state='running' AND lease_until<=UTC_TIMESTAMP(6)) ORDER BY sequence LIMIT 1 FOR UPDATE SKIP LOCKED"))
	if err != nil {
		return run, err
	}
	if run.State == "running" || run.Stage == "recovering" {
		run.Recoveries++
	}
	run.Owner++
	run.State = "running"
	run.Stage = "computing"
	run.Message = "原始数据已保存，正在按冻结依据计算参考"
	_, err = tx.ExecContext(ctx, "UPDATE rotation_recovery_run SET owner=?,state=?,stage=?,message=?,recoveries=?,lease_until=TIMESTAMPADD(SECOND,30,UTC_TIMESTAMP(6)),updated_at=UTC_TIMESTAMP(6) WHERE id=?", run.Owner, run.State, run.Stage, run.Message, run.Recoveries, run.ID)
	if err != nil {
		return run, err
	}
	return run, tx.Commit()
}
func (s *Service) recoveryUpdate(ctx context.Context, run RecoveryRun, state, stage, message string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE rotation_recovery_run SET state=?,stage=?,message=?,lease_until=IF(?='running',TIMESTAMPADD(SECOND,30,UTC_TIMESTAMP(6)),NULL),updated_at=UTC_TIMESTAMP(6) WHERE id=? AND owner=? AND state='running' AND lease_until>UTC_TIMESTAMP(6)`, state, stage, message, state, run.ID, run.Owner)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errRecoveryOwnership
	}
	return nil
}
func (s *Service) ServeRecoveries(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		run, err := s.claimRecovery(ctx)
		if err == nil {
			s.executeRecovery(ctx, run)
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
			log.Print("rotation recovery queue unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) executeRecovery(ctx context.Context, run RecoveryRun) {
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
				err := s.recoveryUpdate(beat, run, "running", run.Stage, run.Message)
				stop()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	err := s.publishReference(task, run.TradeDate)
	state, stage, message := "succeeded", "published", "固定参考已发布，原始输入保持不变"
	if err == nil {
		var published bool
		err = s.DB.QueryRowContext(task, "SELECT status='ready' AND payload IS NOT NULL,message FROM rotation_daily WHERE trade_date=? AND basis='reference_1445'", run.TradeDate).Scan(&published, &message)
		if err == nil && !published {
			state, stage = "failed", "calculation_failed"
		}
	}
	interrupted := task.Err() != nil || errors.Is(err, context.Canceled)
	cancel()
	<-done
	if interrupted {
		state, stage, message = "queued", "recovering", "恢复执行中断，等待按原交易日继续"
	} else if err != nil {
		state, stage, message = "failed", "calculation_failed", "参考计算或发布失败，原始输入保留，可按原任务重试"
	}
	finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := s.recoveryUpdate(finish, run, state, stage, message); err != nil && !errors.Is(err, errRecoveryOwnership) {
		log.Print("rotation recovery status remains pending")
	}
}
