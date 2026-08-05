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

// Statuses 返回全部 Instrument（含已停用、从未同步）的同步状态快照。
func (s *MySQLStore) Statuses(ctx context.Context) ([]core.InstrumentStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.ts_code, i.name, i.sync_enabled,
		       COALESCE(d.latest, ''), COALESCE(a.latest, ''),
		       COALESCE(d.cnt, 0), COALESCE(a.cnt, 0)
		FROM instrument i
		LEFT JOIN (
			SELECT ts_code, MAX(trade_date) AS latest, COUNT(*) AS cnt
			FROM etf_daily GROUP BY ts_code
		) d ON d.ts_code = i.ts_code
		LEFT JOIN (
			SELECT ts_code, MAX(trade_date) AS latest, COUNT(*) AS cnt
			FROM etf_adj_factor GROUP BY ts_code
		) a ON a.ts_code = i.ts_code
		ORDER BY i.ts_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.InstrumentStatus
	for rows.Next() {
		var st core.InstrumentStatus
		if err := rows.Scan(&st.TsCode, &st.Name, &st.SyncEnabled,
			&st.LatestTradeDate, &st.LatestAdjDate, &st.DailyRows, &st.AdjRows); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
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

// RecentDaily 返回该标的最近 limit 条日线及对齐后的复权因子（按交易日升序）。
// 因子取 trade_date <= 当日的最近一条（因子表补齐停牌日，两表日期可能不对齐，见 CONTEXT.md）；
// 无因子记录时回退 1.0（与 rotation7.py 的 fillna(1.0) 一致）。
func (s *MySQLStore) RecentDaily(ctx context.Context, tsCode string, limit int) ([]core.DailyBarAdj, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.trade_date, d.open, d.high, d.low, d.close,
		       COALESCE((SELECT a.adj_factor FROM etf_adj_factor a
		                 WHERE a.ts_code = d.ts_code AND a.trade_date <= d.trade_date
		                 ORDER BY a.trade_date DESC LIMIT 1), 1.0)
		FROM (
			SELECT ts_code, trade_date, open, high, low, close
			FROM etf_daily WHERE ts_code = ?
			ORDER BY trade_date DESC LIMIT ?
		) d
		ORDER BY d.trade_date`, tsCode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.DailyBarAdj
	for rows.Next() {
		var b core.DailyBarAdj
		var open, high, low, close sql.NullFloat64
		if err := rows.Scan(&b.TradeDate, &open, &high, &low, &close, &b.AdjFactor); err != nil {
			return nil, err
		}
		b.Open, b.High, b.Low, b.Close = open.Float64, high.Float64, low.Float64, close.Float64
		out = append(out, b)
	}
	return out, rows.Err()
}

// UpsertIntradaySnapshots 按 (ts_code, trade_date) 主键 upsert 盘中快照，重复执行幂等。返回写入行数。
func (s *MySQLStore) UpsertIntradaySnapshots(ctx context.Context, snaps []core.IntradaySnapshot) (int, error) {
	if len(snaps) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString(`INSERT INTO intraday_snapshot (ts_code, trade_date, open, high, low, latest, adj_mean) VALUES `)
	args := make([]interface{}, 0, len(snaps)*7)
	for i, snap := range snaps {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`(?,?,?,?,?,?,?)`)
		args = append(args, snap.TsCode, snap.TradeDate, snap.Open, snap.High, snap.Low, snap.Latest, snap.AdjMean)
	}
	sb.WriteString(` ON DUPLICATE KEY UPDATE
		open=VALUES(open), high=VALUES(high), low=VALUES(low),
		latest=VALUES(latest), adj_mean=VALUES(adj_mean)`)

	if _, err := s.db.ExecContext(ctx, sb.String(), args...); err != nil {
		return 0, err
	}
	return len(snaps), nil
}
