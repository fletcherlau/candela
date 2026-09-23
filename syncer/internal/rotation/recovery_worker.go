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
	run.Stage = "accepted"
	run.Message = "恢复请求已接受，正在核对原任务进度"
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
				result, err := s.DB.ExecContext(beat, "UPDATE rotation_recovery_run SET lease_until=TIMESTAMPADD(SECOND,30,UTC_TIMESTAMP(6)) WHERE id=? AND owner=? AND state='running' AND lease_until>UTC_TIMESTAMP(6)", run.ID, run.Owner)
				if err == nil {
					var n int64
					n, err = result.RowsAffected()
					if err == nil && n != 1 {
						err = errRecoveryOwnership
					}
				}
				stop()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	var err error
	if run.Basis == "close" {
		err = s.recoverClose(task, run)
	} else {
		err = s.recoveryUpdate(task, run, "running", "computing", "原始数据已保存，正在按冻结依据计算参考")
		if err == nil {
			err = s.publishReferenceRecovery(task, run.TradeDate, &run)
		}
	}
	state, stage, message := "succeeded", "published", "固定参考已发布，原始输入保持不变"
	if err == nil {
		var published bool
		historical := false
		if run.Basis == "close" && s.ETFSync != nil {
			origin, e := s.ETFSync.Get(task, run.OriginID)
			err = e
			if e == nil && origin.Mode == "historical" {
				historical = true
				published, err = s.historicalRecoveryPublished(task, s.DB, origin.StartDate)
				message = "受影响留存日与连续回测已发布，固定参考保持不变"
				if !published {
					message = "受影响收盘或连续回测尚未完成当前数据发布，可按原任务重试"
				}
			}
		}
		if err == nil && !historical {
			err = s.DB.QueryRowContext(task, `SELECT d.status='ready' AND d.payload IS NOT NULL AND (d.basis='reference_1445' OR d.revision=r.revision),
 CASE WHEN d.basis='close' AND d.revision<>r.revision THEN '行情在计算期间发生变化，尚未完成当前数据发布，可按原任务重试' ELSE d.message END
 FROM rotation_daily d JOIN rotation_result r ON r.id=1 WHERE d.trade_date=? AND d.basis=?`, run.TradeDate, run.Basis).Scan(&published, &message)
		}

		if err == nil && !published {
			state, stage = "failed", "calculation_failed"
		}
	}
	interrupted := task.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, errRecoveryOwnership)
	cancel()
	<-done
	if interrupted {
		state, stage, message = "queued", "recovering", "恢复执行中断，等待按原交易日继续"
	} else if errors.Is(err, errRecoverySyncIncomplete) {
		state, stage, message = "failed", "synchronization_failed", "原范围同步未完成，已完成步骤保留；请查看恢复同步批次中的失败对象后重试"
	} else if err != nil {
		state, stage, message = "failed", "calculation_failed", "计算或发布失败，已完成步骤保留，可按原任务重试"
	}
	finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := s.recoveryUpdate(finish, run, state, stage, message); err != nil && !errors.Is(err, errRecoveryOwnership) {
		log.Print("rotation recovery status remains pending")
	}
}

// A publication and its execution-right check commit atomically. A slow
// calculation cannot publish after another worker acquired or finished the run.
func commitRecoveryPublication(ctx context.Context, tx *sql.Tx, run *RecoveryRun) error {
	if run != nil {
		var owner int64
		var state string
		var live bool
		err := tx.QueryRowContext(ctx, "SELECT owner,state,COALESCE(lease_until>UTC_TIMESTAMP(6),FALSE) FROM rotation_recovery_run WHERE id=? FOR UPDATE", run.ID).Scan(&owner, &state, &live)
		if errors.Is(err, sql.ErrNoRows) {
			return errRecoveryOwnership
		}
		if err != nil {
			return err
		}
		if owner != run.Owner || state != "running" || !live {
			return errRecoveryOwnership
		}
	}
	return tx.Commit()
}
