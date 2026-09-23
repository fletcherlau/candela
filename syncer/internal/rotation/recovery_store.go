package rotation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
)

type RecoveryRun struct {
	ID          string `json:"id"`
	ParentID    string `json:"parentId"`
	OriginID    string `json:"originId"`
	Basis       string `json:"basis"`
	TradeDate   string `json:"tradeDate"`
	TargetAt    string `json:"targetAt"`
	State       string `json:"state"`
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	SyncBatchID string `json:"syncBatchId"`
	Recoveries  int    `json:"recoveries"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	Owner       int64  `json:"-"`
}

const recoveryColumns = `id,parent_id,origin_id,basis,trade_date,target_at,state,stage,message,sync_batch_id,recoveries,DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%sZ'),DATE_FORMAT(updated_at,'%Y-%m-%dT%H:%i:%sZ'),owner`

func scanRecovery(row captureScanner) (r RecoveryRun, err error) {
	err = row.Scan(&r.ID, &r.ParentID, &r.OriginID, &r.Basis, &r.TradeDate, &r.TargetAt, &r.State, &r.Stage, &r.Message, &r.SyncBatchID, &r.Recoveries, &r.CreatedAt, &r.UpdatedAt, &r.Owner)
	return
}
func (s *Service) recoveryRun(ctx context.Context, id string) (RecoveryRun, error) {
	return scanRecovery(s.DB.QueryRowContext(ctx, "SELECT "+recoveryColumns+" FROM rotation_recovery_run WHERE id=?", id))
}
func (s *Service) recoveryList(ctx context.Context, basis, origin string) ([]RecoveryRun, error) {
	query := "SELECT " + recoveryColumns + " FROM rotation_recovery_run"
	var args []any
	if basis != "" {
		query += " WHERE basis=? AND origin_id=?"
		args = []any{basis, origin}
	}
	rows, err := s.DB.QueryContext(ctx, query+" ORDER BY sequence DESC LIMIT 50", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecoveryRun{}
	for rows.Next() {
		r, e := scanRecovery(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// The browser supplies a saved origin or parent attempt only. All target fields
// come from that durable origin; no new source quote is part of acceptance.
func (s *Service) acceptReferenceRecovery(ctx context.Context, origin, parent string) (RecoveryRun, bool, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return RecoveryRun{}, false, err
	}
	defer tx.Rollback()
	keyInput := "reference/" + origin
	if parent != "" {
		previous, e := scanRecovery(tx.QueryRowContext(ctx, "SELECT "+recoveryColumns+" FROM rotation_recovery_run WHERE id=? FOR UPDATE", parent))
		if e != nil {
			return RecoveryRun{}, false, e
		}
		if previous.Basis != "reference_1445" {
			return RecoveryRun{}, false, &captureRequestError{400, "不支持此恢复阶段。"}
		}
		// Reject active attempts before acquiring the capture lock. The
		// publisher holds that lock before checking its recovery ownership.
		if previous.State != "failed" && previous.State != "unavailable" {
			return RecoveryRun{}, false, &captureRequestError{409, "原恢复任务尚未失败，无需再次重试。"}
		}
		origin = previous.OriginID
		keyInput = "retry/" + parent
	}
	digest := sha256.Sum256([]byte(keyInput))
	key := hex.EncodeToString(digest[:])
	capture, err := scanCapture(tx.QueryRowContext(ctx, "SELECT "+captureColumns+" FROM rotation_capture_run WHERE trade_date=? FOR UPDATE", origin))
	if err != nil {
		return RecoveryRun{}, false, err
	}
	prior, err := scanRecovery(tx.QueryRowContext(ctx, "SELECT "+recoveryColumns+" FROM rotation_recovery_run WHERE request_key=?", key))
	if err == nil {
		return prior, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RecoveryRun{}, false, err
	}
	var published int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM rotation_daily WHERE trade_date=? AND basis='reference_1445' AND payload IS NOT NULL", origin).Scan(&published); err != nil {
		return RecoveryRun{}, false, err
	}
	if published > 0 {
		return RecoveryRun{}, false, &captureRequestError{409, "固定参考已发布，无需重试，也不能覆盖原始参考。"}
	}
	if capture.State == "queued" || capture.State == "running" {
		return RecoveryRun{}, false, &captureRequestError{409, "原始采集仍在执行，请等待结果。"}
	}
	state, stage, message := "queued", "accepted", "恢复请求已保存，等待使用原时点冻结输入计算"
	if capture.State != "captured" || capture.Available != 4 {
		state, stage, message = "unavailable", "inputs_missing", "原时点数据无法补取；保留已保存输入，不请求当前价格"
	}
	raw := make([]byte, 16)
	if _, err = rand.Read(raw); err != nil {
		return RecoveryRun{}, false, err
	}
	id := hex.EncodeToString(raw)
	_, err = tx.ExecContext(ctx, `INSERT INTO rotation_recovery_run(id,request_key,parent_id,origin_id,basis,trade_date,target_at,state,stage,message,created_at,updated_at) VALUES (?,?,?,?,'reference_1445',?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, id, key, parent, origin, capture.TradeDate, capture.TargetAt, state, stage, message)
	if err != nil {
		return RecoveryRun{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return RecoveryRun{}, false, err
	}
	run, err := s.recoveryRun(ctx, id)
	return run, false, err
}
