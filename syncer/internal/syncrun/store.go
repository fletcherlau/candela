package syncrun

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const columns = `id,ts_code,mode,start_date,end_date,effective_start,state,stage,processed_rows,completed_segments,total_segments,checkpoint,history_evidence,error_code,message,DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%sZ'),DATE_FORMAT(updated_at,'%Y-%m-%dT%H:%i:%sZ'),owner`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (r Run, err error) {
	err = row.Scan(&r.ID, &r.Code, &r.Mode, &r.StartDate, &r.EndDate, &r.EffectiveStart, &r.State, &r.Stage, &r.ProcessedRows, &r.CompletedSegments, &r.TotalSegments, &r.Checkpoint, &r.HistoryEvidence, &r.ErrorCode, &r.Message, &r.CreatedAt, &r.UpdatedAt, &r.Owner)
	return
}

// Submit locks the object before choosing the incremental boundary and looking
// for equivalent unfinished work. It never calls the source or starts execution.
func (s *Store) Submit(ctx context.Context, mode string, now time.Time) (Run, bool, error) {
	if mode != "backfill" && mode != "incremental" {
		return Run{}, false, ErrMode
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, false, err
	}
	defer tx.Rollback()
	var fence int64
	if err = tx.QueryRowContext(ctx, "SELECT fence FROM index_series WHERE ts_code=? FOR UPDATE", Code).Scan(&fence); err != nil {
		return Run{}, false, err
	}
	end, start := Cutoff(now), HistoryFloor
	// Same action and frozen cutoff remains the same request even as its worker
	// advances stored coverage. Do this before computing the incremental start.
	r, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM sync_run WHERE ts_code=? AND mode=? AND end_date=? AND state IN ('queued','running') ORDER BY sequence LIMIT 1", Code, mode, end))
	if err == nil {
		return r, true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, err
	}
	if mode == "incremental" {
		if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(trade_date),?) FROM index_daily WHERE ts_code=? AND trade_date<=?", HistoryFloor, Code, end).Scan(&start); err != nil {
			return Run{}, false, err
		}
	}
	buf := make([]byte, 16)
	if _, err = rand.Read(buf); err != nil {
		return Run{}, false, err
	}
	id := hex.EncodeToString(buf)
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_run(id,ts_code,mode,start_date,end_date,history_evidence,created_at,updated_at) VALUES (?,?,?,?,?,'',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, id, Code, mode, start, end)
	if err != nil {
		return Run{}, false, err
	}
	r, err = scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM sync_run WHERE id=?", id))
	if err != nil {
		return Run{}, false, err
	}
	return r, false, tx.Commit()
}
func (s *Store) Get(ctx context.Context, id string) (Run, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM sync_run WHERE id=?", id))
}
func (s *Store) List(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+" FROM sync_run ORDER BY sequence DESC LIMIT 50")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Claim serializes on the object row. Expired executions become explicit
// interrupted failures; checkpoint recovery and retry belong to T03.
func (s *Store) Claim(ctx context.Context) (Run, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()
	var active sql.NullString
	var fence int64
	if err = tx.QueryRowContext(ctx, "SELECT active_run,fence FROM index_series WHERE ts_code=? FOR UPDATE", Code).Scan(&active, &fence); err != nil {
		return Run{}, err
	}
	if active.Valid {
		var alive bool
		if err = tx.QueryRowContext(ctx, "SELECT state='running' AND lease_until>UTC_TIMESTAMP(6) FROM sync_run WHERE id=?", active.String).Scan(&alive); err != nil {
			return Run{}, err
		}
		if alive {
			return Run{}, sql.ErrNoRows
		}
		if _, err = tx.ExecContext(ctx, `UPDATE sync_run SET state='failed',stage='finished',error_code='interrupted',message='执行进程中断或执行权过期；已提交分段保留。',lease_until=NULL,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND state='running'`, active.String); err != nil {
			return Run{}, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE index_series SET active_run=NULL WHERE ts_code=?", Code); err != nil {
			return Run{}, err
		}
	}
	r, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM sync_run WHERE ts_code=? AND state='queued' ORDER BY sequence LIMIT 1 FOR UPDATE", Code))
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, commitEmpty(tx)
	}
	if err != nil {
		return Run{}, err
	}
	fence++
	if _, err = tx.ExecContext(ctx, `UPDATE index_series SET active_run=?,fence=? WHERE ts_code=?`, r.ID, fence, Code); err != nil {
		return Run{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sync_run SET state='running',stage='discovering',owner=?,lease_until=TIMESTAMPADD(SECOND,30,UTC_TIMESTAMP(6)),updated_at=UTC_TIMESTAMP(6) WHERE id=?`, fence, r.ID); err != nil {
		return Run{}, err
	}
	r.Owner = fence
	r.State = "running"
	r.Stage = "discovering"
	return r, tx.Commit()
}
func commitEmpty(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		return err
	}
	return sql.ErrNoRows
}

// Every checkpoint/terminal transition checks the database-time lease and fencing
// generation while holding the same object lock used by Claim.
func (s *Store) owned(ctx context.Context, r Run, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active sql.NullString
	var fence int64
	if err = tx.QueryRowContext(ctx, "SELECT active_run,fence FROM index_series WHERE ts_code=? FOR UPDATE", r.Code).Scan(&active, &fence); err != nil {
		return err
	}
	if !active.Valid || active.String != r.ID || fence != r.Owner {
		return ErrOwnership
	}
	var alive bool
	var deadline string
	if err = tx.QueryRowContext(ctx, "SELECT state='running' AND owner=? AND lease_until>UTC_TIMESTAMP(6),DATE_FORMAT(lease_until,'%Y-%m-%d %H:%i:%s.%f') FROM sync_run WHERE id=? FOR UPDATE", r.Owner, r.ID).Scan(&alive, &deadline); err != nil {
		return err
	}
	if !alive {
		return ErrOwnership
	}
	if err = fn(tx); err != nil {
		return err
	}
	// Recheck the original deadline even for terminal transitions or renewal:
	// a transaction that outlives its lease cannot publish or revive ownership.
	var valid bool
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)<CAST(? AS DATETIME(6))", deadline).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrOwnership
	}
	return tx.Commit()
}
func (s *Store) Heartbeat(ctx context.Context, r Run) error {
	return s.owned(ctx, r, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE sync_run SET lease_until=TIMESTAMPADD(SECOND,30,UTC_TIMESTAMP(6)) WHERE id=?", r.ID)
		return err
	})
}
func (s *Store) Plan(ctx context.Context, r Run, start, evidence string, total int) error {
	return s.owned(ctx, r, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE sync_run SET stage='fetching',effective_start=?,history_evidence=?,total_segments=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?`, start, evidence, total, r.ID)
		return err
	})
}
func (s *Store) CommitWindow(ctx context.Context, r Run, from, to string, bars []Bar) error {
	return s.owned(ctx, r, func(tx *sql.Tx) error {
		var checkpoint, effective string
		if err := tx.QueryRowContext(ctx, "SELECT checkpoint,effective_start FROM sync_run WHERE id=?", r.ID).Scan(&checkpoint, &effective); err != nil {
			return err
		}
		next := effective
		if checkpoint != "" {
			next = date(checkpoint).AddDate(0, 0, 1).Format("20060102")
		}
		if from != next || to < from || to > r.EndDate {
			return errors.New("non-contiguous checkpoint")
		}
		for _, b := range bars {
			if b.Code != r.Code || b.Date < from || b.Date > to {
				return errors.New("bar outside frozen range")
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO index_daily(ts_code,trade_date,open,high,low,close,pre_close,change_amt,pct_chg,vol,amount) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE open=VALUES(open),high=VALUES(high),low=VALUES(low),close=VALUES(close),pre_close=VALUES(pre_close),change_amt=VALUES(change_amt),pct_chg=VALUES(pct_chg),vol=VALUES(vol),amount=VALUES(amount)`, b.Code, b.Date, b.Open, b.High, b.Low, b.Close, b.PreClose, b.Change, b.PctChange, b.Volume, b.Amount)
			if err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE sync_run SET checkpoint=?,processed_rows=processed_rows+?,completed_segments=completed_segments+1,updated_at=UTC_TIMESTAMP(6) WHERE id=?`, to, len(bars), r.ID)
		return err
	})
}
func (s *Store) Finish(ctx context.Context, r Run, code, message string) error {
	return s.owned(ctx, r, func(tx *sql.Tx) error {
		state := "failed"
		if code == "" {
			var complete bool
			if err := tx.QueryRowContext(ctx, "SELECT checkpoint=end_date AND completed_segments=total_segments AND total_segments>0 FROM sync_run WHERE id=?", r.ID).Scan(&complete); err != nil {
				return err
			}
			if !complete {
				return errors.New("cannot complete unfinished windows")
			}
			state = "succeeded"
		}
		_, err := tx.ExecContext(ctx, `UPDATE sync_run SET state=?,stage='finished',error_code=?,message=?,lease_until=NULL,updated_at=UTC_TIMESTAMP(6) WHERE id=?`, state, code, message, r.ID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE index_series SET active_run=NULL WHERE ts_code=?", r.Code)
		return err
	})
}
