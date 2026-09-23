package rotation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var errRecoverySyncIncomplete = errors.New("original scope synchronization incomplete")

// The sync batch, not a browser date or a new submission, defines recovery scope.
func (s *Service) acceptCloseRecovery(ctx context.Context, origin, parent string) (RecoveryRun, bool, error) {
	if s.ETFSync == nil {
		return RecoveryRun{}, false, &captureRequestError{503, "收盘同步服务暂不可用。"}
	}
	batch, err := s.ETFSync.Get(ctx, origin)
	if err != nil {
		return RecoveryRun{}, false, err
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return RecoveryRun{}, false, err
	}
	defer tx.Rollback()
	// Immutable batch identity serializes root submissions without another ETF queue.
	var originalID string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM etf_sync_batch WHERE id=? FOR UPDATE", origin).Scan(&originalID); err != nil {
		return RecoveryRun{}, false, err
	}
	keyInput := "close/" + origin
	var previous RecoveryRun
	if parent != "" {
		previous, err = scanRecovery(tx.QueryRowContext(ctx, "SELECT "+recoveryColumns+" FROM rotation_recovery_run WHERE id=? FOR UPDATE", parent))
		if err != nil {
			return RecoveryRun{}, false, err
		}
		if previous.Basis != "close" || previous.OriginID != origin {
			return RecoveryRun{}, false, &captureRequestError{400, "原恢复阶段不匹配。"}
		}
		keyInput = "retry/" + parent
	}
	digest := sha256.Sum256([]byte(keyInput))
	key := hex.EncodeToString(digest[:])
	prior, err := scanRecovery(tx.QueryRowContext(ctx, "SELECT "+recoveryColumns+" FROM rotation_recovery_run WHERE request_key=?", key))
	if err == nil {
		return prior, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RecoveryRun{}, false, err
	}
	if parent != "" && previous.State != "failed" {
		return RecoveryRun{}, false, &captureRequestError{409, "原恢复任务尚未失败，无需再次重试。"}
	}
	if batch.State != "failed" && batch.State != "partial" && batch.State != "succeeded" {
		return RecoveryRun{}, false, &captureRequestError{409, "同步仍在执行或已取消，不能发起失败恢复。"}
	}
	relevantBatch := false
	for _, item := range batch.Items {
		relevantBatch = relevantBatch || relevant(item.Code)
	}
	if !relevantBatch {
		return RecoveryRun{}, false, &captureRequestError{400, "原批次不包含轮动标的。"}
	}
	date, err := time.ParseInLocation("20060102", batch.EndDate, time.FixedZone("Asia/Shanghai", 8*3600))
	if err != nil {
		return RecoveryRun{}, false, err
	}
	target := date.Add(15 * time.Hour)
	if target.After(s.now()) {
		return RecoveryRun{}, false, &captureRequestError{409, "原交易日尚未收盘。"}
	}
	if err = s.ensureCalendar(ctx, batch.EndDate, batch.EndDate); err != nil {
		return RecoveryRun{}, false, err
	}
	var open bool
	if err = tx.QueryRowContext(ctx, "SELECT is_open FROM rotation_calendar WHERE cal_date=?", batch.EndDate).Scan(&open); err != nil {
		return RecoveryRun{}, false, err
	}
	if !open {
		return RecoveryRun{}, false, &captureRequestError{400, "原同步截止日不是交易日，不能发布收盘结果。"}
	}
	var published int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM rotation_daily d JOIN rotation_result r ON r.id=1 AND r.revision=d.revision WHERE d.trade_date=? AND d.basis='close' AND d.status='ready' AND d.payload IS NOT NULL`, batch.EndDate).Scan(&published); err != nil {
		return RecoveryRun{}, false, err
	}
	if published > 0 {
		return RecoveryRun{}, false, &captureRequestError{409, "收盘结果已发布，无需恢复。"}
	}
	raw := make([]byte, 16)
	if _, err = rand.Read(raw); err != nil {
		return RecoveryRun{}, false, err
	}
	id := hex.EncodeToString(raw)
	_, err = tx.ExecContext(ctx, `INSERT INTO rotation_recovery_run(id,request_key,parent_id,origin_id,basis,trade_date,target_at,state,stage,message,created_at,updated_at) VALUES (?,?,?,?,'close',?,?,'queued','accepted','恢复请求已保存，沿用原批次日期和失败步骤',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, id, key, parent, origin, batch.EndDate, target.Format(time.RFC3339))
	if err != nil {
		return RecoveryRun{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return RecoveryRun{}, false, err
	}
	run, err := s.recoveryRun(ctx, id)
	return run, false, err
}

func (s *Service) recoverClose(ctx context.Context, run RecoveryRun) error {
	if s.ETFSync == nil {
		return fmt.Errorf("close synchronization unavailable")
	}
	if run.SyncBatchID == "" {
		sourceID := run.OriginID
		if run.ParentID != "" {
			parent, err := s.recoveryRun(ctx, run.ParentID)
			if err != nil {
				return err
			}
			if parent.SyncBatchID != "" {
				sourceID = parent.SyncBatchID
			}
		}
		batch, err := s.ETFSync.Get(ctx, sourceID)
		if err != nil {
			return err
		}
		if batch.State == "failed" || batch.State == "partial" {
			batch, _, err = s.ETFSync.Retry(ctx, sourceID)
			if err != nil {
				return err
			}
		}
		result, err := s.DB.ExecContext(ctx, `UPDATE rotation_recovery_run SET sync_batch_id=?,stage='syncing',message='沿用原范围恢复未完成同步步骤',updated_at=UTC_TIMESTAMP(6) WHERE id=? AND owner=? AND state='running' AND lease_until>UTC_TIMESTAMP(6)`, batch.ID, run.ID, run.Owner)
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
		run.SyncBatchID = batch.ID
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		batch, err := s.ETFSync.Get(ctx, run.SyncBatchID)
		if err != nil {
			return err
		}
		if batch.State == "succeeded" {
			break
		}
		if batch.State == "failed" || batch.State == "partial" || batch.State == "cancelled" {
			return errRecoverySyncIncomplete
		}
		if err = s.recoveryUpdate(ctx, run, "running", "syncing", fmt.Sprintf("原批次恢复中，已完成 %d/%d 个对象", batch.Success, batch.Total)); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	if err := s.recoveryUpdate(ctx, run, "running", "sync_complete", "原范围同步完成，等待整组收盘计算"); err != nil {
		return err
	}
	if err := s.recoveryUpdate(ctx, run, "running", "computing", "原范围同步完成，正在重算四标的收盘结果"); err != nil {
		return err
	}
	return s.publishCloseRecovery(ctx, run.TradeDate, &run)
}
