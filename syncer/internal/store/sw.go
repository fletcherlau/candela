package store

import (
	"context"
	"database/sql"
	"strings"

	"syncer/internal/core"
)

// UpsertSWIndustries 按 index_code 主键 upsert 行业字典，重复执行幂等。返回写入行数。
func (s *MySQLStore) UpsertSWIndustries(ctx context.Context, items []core.SWIndustry) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString(`INSERT INTO sw_industry (index_code, industry_name, level, industry_code, parent_code, is_pub, src) VALUES `)
	args := make([]interface{}, 0, len(items)*7)
	for i, it := range items {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`(?,?,?,?,?,?,?)`)
		args = append(args, it.IndexCode, it.IndustryName, it.Level, it.IndustryCode, it.ParentCode, it.IsPub, it.Src)
	}
	sb.WriteString(` ON DUPLICATE KEY UPDATE
		industry_name=VALUES(industry_name), level=VALUES(level), industry_code=VALUES(industry_code),
		parent_code=VALUES(parent_code), is_pub=VALUES(is_pub), src=VALUES(src)`)

	if _, err := s.db.ExecContext(ctx, sb.String(), args...); err != nil {
		return 0, err
	}
	return len(items), nil
}

// ListSWIndustries 返回字典记录（按 index_code 升序）；level 非空时按级别过滤。
func (s *MySQLStore) ListSWIndustries(ctx context.Context, level string) ([]core.SWIndustry, error) {
	query := `SELECT index_code, industry_name, level, industry_code, parent_code, is_pub, src FROM sw_industry`
	args := []interface{}{}
	if level != "" {
		query += ` WHERE level = ?`
		args = append(args, level)
	}
	query += ` ORDER BY index_code`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.SWIndustry
	for rows.Next() {
		var it core.SWIndustry
		if err := rows.Scan(&it.IndexCode, &it.IndustryName, &it.Level, &it.IndustryCode, &it.ParentCode, &it.IsPub, &it.Src); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// CountSWIndustries 返回行业字典行数。
func (s *MySQLStore) CountSWIndustries(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sw_industry`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// UpsertSWMembers 按 (ts_code, l1_code, in_date) 主键 upsert 成分归属，重复执行幂等。返回写入行数。
// out_date 空串落库为 NULL（在册）。
func (s *MySQLStore) UpsertSWMembers(ctx context.Context, items []core.SWMember) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString(`INSERT INTO sw_industry_member
		(ts_code, name, l1_code, l1_name, l2_code, l2_name, l3_code, l3_name, in_date, out_date, is_new) VALUES `)
	args := make([]interface{}, 0, len(items)*11)
	for i, it := range items {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`(?,?,?,?,?,?,?,?,?,?,?)`)
		var outDate interface{}
		if it.OutDate != "" {
			outDate = it.OutDate
		}
		args = append(args, it.TsCode, it.Name, it.L1Code, it.L1Name, it.L2Code, it.L2Name,
			it.L3Code, it.L3Name, it.InDate, outDate, it.IsNew)
	}
	sb.WriteString(` ON DUPLICATE KEY UPDATE
		name=VALUES(name), l1_name=VALUES(l1_name), l2_code=VALUES(l2_code), l2_name=VALUES(l2_name),
		l3_code=VALUES(l3_code), l3_name=VALUES(l3_name), out_date=VALUES(out_date), is_new=VALUES(is_new)`)

	if _, err := s.db.ExecContext(ctx, sb.String(), args...); err != nil {
		return 0, err
	}
	return len(items), nil
}

// CountSWMembers 返回成分归属行数。
func (s *MySQLStore) CountSWMembers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sw_industry_member`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// LatestSWIndexDailyDate 返回该指数已存储的最新日线交易日（YYYYMMDD）；无历史时返回 ""。
func (s *MySQLStore) LatestSWIndexDailyDate(ctx context.Context, tsCode string) (string, error) {
	var latest sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(trade_date) FROM sw_index_daily WHERE ts_code = ?`, tsCode).Scan(&latest); err != nil {
		return "", err
	}
	return latest.String, nil
}

// UpsertSWIndexDaily 按 (ts_code, trade_date) 主键 upsert 指数日线，重复执行幂等。返回写入行数。
func (s *MySQLStore) UpsertSWIndexDaily(ctx context.Context, bars []core.SWIndexBar) (int, error) {
	if len(bars) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString(`INSERT INTO sw_index_daily (ts_code, trade_date, open, high, low, close, change_amt, pct_change, vol, amount) VALUES `)
	args := make([]interface{}, 0, len(bars)*10)
	for i, b := range bars {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`(?,?,?,?,?,?,?,?,?,?)`)
		args = append(args, b.TsCode, b.TradeDate, b.Open, b.High, b.Low, b.Close,
			b.ChangeAmt, b.PctChange, b.Vol, b.Amount)
	}
	sb.WriteString(` ON DUPLICATE KEY UPDATE
		open=VALUES(open), high=VALUES(high), low=VALUES(low), close=VALUES(close),
		change_amt=VALUES(change_amt), pct_change=VALUES(pct_change), vol=VALUES(vol), amount=VALUES(amount)`)

	if _, err := s.db.ExecContext(ctx, sb.String(), args...); err != nil {
		return 0, err
	}
	return len(bars), nil
}

// SWIndexStatuses 返回全部行业指数的日线同步状态快照（含从未同步的指数）。
func (s *MySQLStore) SWIndexStatuses(ctx context.Context) ([]core.SWIndexStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.index_code, i.industry_name, i.level,
		       COALESCE(d.latest, ''), COALESCE(d.cnt, 0)
		FROM sw_industry i
		LEFT JOIN (
			SELECT ts_code, MAX(trade_date) AS latest, COUNT(*) AS cnt
			FROM sw_index_daily GROUP BY ts_code
		) d ON d.ts_code = i.index_code
		ORDER BY i.level, i.index_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.SWIndexStatus
	for rows.Next() {
		var st core.SWIndexStatus
		if err := rows.Scan(&st.TsCode, &st.Name, &st.Level, &st.LatestTradeDate, &st.DailyRows); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
