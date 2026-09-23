package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"syncer/internal/core"
	"time"
)

const captureColumns = `trade_date,target_at,deadline_at,basis,state,stage,available,message,params,recoveries,DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%sZ'),DATE_FORMAT(updated_at,'%Y-%m-%dT%H:%i:%sZ'),owner`

type captureScanner interface{ Scan(...any) error }

func scanCapture(row captureScanner) (r CaptureRun, err error) {
	var params []byte
	err = row.Scan(&r.TradeDate, &r.TargetAt, &r.DeadlineAt, &r.Basis, &r.State, &r.Stage, &r.Available, &r.Message, &params, &r.Recoveries, &r.CreatedAt, &r.UpdatedAt, &r.Owner)
	if err == nil {
		err = json.Unmarshal(params, &r.Params)
	}
	return
}

type captureRequestError struct {
	status  int
	message string
}

func (e *captureRequestError) Error() string { return e.message }
func (s *Service) submitCapture(ctx context.Context, date string) (CaptureRun, bool, error) {
	target, err := captureTarget(date)
	if err != nil || date > s.today() {
		return CaptureRun{}, false, &captureRequestError{400, "交易日无效或位于未来。"}
	}
	if s.now().Before(target) {
		return CaptureRun{}, false, &captureRequestError{409, "尚未到该交易日 14:45，不能提前采集。"}
	}
	if saved, err := s.captureRun(ctx, date); err == nil {
		return saved, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return CaptureRun{}, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return CaptureRun{}, false, err
	}
	defer tx.Rollback()
	var open bool
	err = tx.QueryRowContext(ctx, "SELECT is_open FROM rotation_calendar WHERE cal_date=?", date).Scan(&open)
	if errors.Is(err, sql.ErrNoRows) {
		return CaptureRun{}, false, &captureRequestError{409, "交易日历尚未缓存，无法确认采集日期。"}
	}
	if err != nil {
		return CaptureRun{}, false, err
	}
	if !open {
		return CaptureRun{}, false, &captureRequestError{409, "该日期不是交易日，不采集参考。"}
	}
	params, _ := json.Marshal(CaptureParams{Version: indicatorVersion, QuantileWindow: s.quantileWindow()})
	result, err := tx.ExecContext(ctx, `INSERT IGNORE INTO rotation_capture_run(trade_date,target_at,deadline_at,params,message,created_at,updated_at) VALUES (?,?,?,?,'等待固定 14:45 采集',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, date, target.Format(time.RFC3339), target.Add(time.Minute).Format(time.RFC3339), params)
	if err != nil {
		return CaptureRun{}, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return CaptureRun{}, false, err
	}
	if n == 1 {
		for _, code := range core.RotationCodes {
			if _, err = tx.ExecContext(ctx, "INSERT INTO rotation_reference_input(trade_date,ts_code) VALUES (?,?)", date, code); err != nil {
				return CaptureRun{}, false, err
			}
		}
	}
	// INSERT IGNORE obtains a shared lock for a duplicate. Do not upgrade that
	// lock: concurrent duplicate callers would deadlock. Commit acceptance,
	// then read the saved record in its own consistent read transaction.
	if err = tx.Commit(); err != nil {
		return CaptureRun{}, false, err
	}
	run, err := s.captureRun(ctx, date)
	return run, n == 0, err
}
func (s *Service) captureRun(ctx context.Context, date string) (CaptureRun, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return CaptureRun{}, err
	}
	defer tx.Rollback()
	run, err := scanCapture(tx.QueryRowContext(ctx, "SELECT "+captureColumns+" FROM rotation_capture_run WHERE trade_date=?", date))
	if err != nil {
		return CaptureRun{}, err
	}
	for i, code := range core.RotationCodes {
		item := CaptureItem{Code: code, Name: core.RotationNames[i]}
		var data []byte
		if err = tx.QueryRowContext(ctx, "SELECT state,reason,payload FROM rotation_reference_input WHERE trade_date=? AND ts_code=?", date, code).Scan(&item.State, &item.Reason, &data); err != nil {
			return CaptureRun{}, err
		}
		if len(data) > 0 {
			if err = json.Unmarshal(data, &item.Input); err != nil {
				return CaptureRun{}, err
			}
		}
		run.Items = append(run.Items, item)
	}
	return run, tx.Commit()
}
func (s *Service) captureList(ctx context.Context) ([]CaptureRun, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+captureColumns+" FROM rotation_capture_run ORDER BY trade_date DESC LIMIT 50")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []CaptureRun{}
	for rows.Next() {
		run, err := scanCapture(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}
