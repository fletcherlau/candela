// Package store 实现 core.Store，将原始行情与复权因子按主键 upsert 到 MySQL。
package store

import (
	"context"
	"database/sql"
	"strings"

	"syncer/internal/core"
)

// MySQLStore 是 core.Store 的 MySQL 实现。
type MySQLStore struct {
	db *sql.DB
}

// NewMySQLStore 构造 MySQL 存储。
func NewMySQLStore(db *sql.DB) *MySQLStore {
	return &MySQLStore{db: db}
}

// ListSyncEnabled 返回全部启用同步的 Instrument。
func (s *MySQLStore) ListSyncEnabled(ctx context.Context) ([]core.Instrument, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts_code, name FROM instrument WHERE sync_enabled = 1 ORDER BY ts_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.Instrument
	for rows.Next() {
		var inst core.Instrument
		if err := rows.Scan(&inst.TsCode, &inst.Name); err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// LatestDailyDate 返回该标的已存储的最新日线交易日（YYYYMMDD）；无历史时返回 ""。
func (s *MySQLStore) LatestDailyDate(ctx context.Context, tsCode string) (string, error) {
	return s.latestDate(ctx, `SELECT MAX(trade_date) FROM etf_daily WHERE ts_code = ?`, tsCode)
}

// LatestAdjDate 返回该标的已存储的最新因子日期（YYYYMMDD）；无历史时返回 ""。
func (s *MySQLStore) LatestAdjDate(ctx context.Context, tsCode string) (string, error) {
	return s.latestDate(ctx, `SELECT MAX(trade_date) FROM etf_adj_factor WHERE ts_code = ?`, tsCode)
}

func (s *MySQLStore) latestDate(ctx context.Context, query, tsCode string) (string, error) {
	var latest sql.NullString
	if err := s.db.QueryRowContext(ctx, query, tsCode).Scan(&latest); err != nil {
		return "", err
	}
	return latest.String, nil
}

// UpsertDaily 按 (ts_code, trade_date) 主键 upsert 日线，重复执行幂等。返回写入行数。
func (s *MySQLStore) UpsertDaily(ctx context.Context, bars []core.Bar) (int, error) {
	if len(bars) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString(`INSERT INTO etf_daily (ts_code, trade_date, open, high, low, close, pre_close, change_amt, pct_chg, vol, amount) VALUES `)
	args := make([]interface{}, 0, len(bars)*11)
	for i, b := range bars {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`(?,?,?,?,?,?,?,?,?,?,?)`)
		args = append(args, b.TsCode, b.TradeDate, b.Open, b.High, b.Low, b.Close,
			b.PreClose, b.ChangeAmt, b.PctChg, b.Vol, b.Amount)
	}
	sb.WriteString(` ON DUPLICATE KEY UPDATE
		open=VALUES(open), high=VALUES(high), low=VALUES(low), close=VALUES(close),
		pre_close=VALUES(pre_close), change_amt=VALUES(change_amt), pct_chg=VALUES(pct_chg),
		vol=VALUES(vol), amount=VALUES(amount)`)

	if _, err := s.db.ExecContext(ctx, sb.String(), args...); err != nil {
		return 0, err
	}
	return len(bars), nil
}

// UpsertAdjFactors 按 (ts_code, trade_date) 主键 upsert 因子，重复执行幂等。返回写入行数。
func (s *MySQLStore) UpsertAdjFactors(ctx context.Context, factors []core.AdjFactor) (int, error) {
	if len(factors) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString(`INSERT INTO etf_adj_factor (ts_code, trade_date, adj_factor) VALUES `)
	args := make([]interface{}, 0, len(factors)*3)
	for i, a := range factors {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`(?,?,?)`)
		args = append(args, a.TsCode, a.TradeDate, a.AdjFactor)
	}
	sb.WriteString(` ON DUPLICATE KEY UPDATE adj_factor=VALUES(adj_factor)`)

	if _, err := s.db.ExecContext(ctx, sb.String(), args...); err != nil {
		return 0, err
	}
	return len(factors), nil
}
