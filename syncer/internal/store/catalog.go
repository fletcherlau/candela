package store

import (
	"context"
	"syncer/internal/catalog"
)

// Catalog reports observed coverage; available means stored, not up to date.
func (s *MySQLStore) Catalog(ctx context.Context) catalog.Result {
	groups := []catalog.Group{
		{ID: "csi", Name: "中证全指", Status: "available", Items: []catalog.Item{}},
		{ID: "etf", Name: "ETF", Status: "available", Items: []catalog.Item{}},
		{ID: "sw", Name: "申万行业", Status: "available", Items: []catalog.Item{}},
	}
	var err error
	groups[0].Items, err = s.indexCoverage(ctx)
	if err != nil {
		groups[0].Status = "error"
		groups[0].Items = []catalog.Item{}
		groups[0].Message = "指数数据读取失败，请稍后重试。"
	}
	groups[1].Items, err = s.etfCoverage(ctx)
	if err != nil {
		groups[1].Status = "error"
		groups[1].Items = []catalog.Item{}
		groups[1].Message = "ETF 数据读取失败，请稍后重试。"
	}
	groups[2].Items, err = s.swCoverage(ctx)
	if err != nil {
		groups[2].Status = "error"
		groups[2].Items = []catalog.Item{}
		groups[2].Message = "申万数据读取失败，请稍后重试。"
	}
	return catalog.Result{Groups: groups}
}

func (s *MySQLStore) etfCoverage(ctx context.Context) ([]catalog.Item, error) {
	rows, err := s.db.QueryContext(ctx, `
 SELECT i.ts_code,i.name,i.sync_enabled,'daily',COUNT(d.trade_date),COALESCE(MIN(d.trade_date),''),COALESCE(MAX(d.trade_date),'')
 FROM instrument i LEFT JOIN etf_daily d ON d.ts_code=i.ts_code GROUP BY i.ts_code,i.name,i.sync_enabled
 UNION ALL
 SELECT i.ts_code,i.name,i.sync_enabled,'factor',COUNT(d.trade_date),COALESCE(MIN(d.trade_date),''),COALESCE(MAX(d.trade_date),'')
 FROM instrument i LEFT JOIN etf_adj_factor d ON d.ts_code=i.ts_code GROUP BY i.ts_code,i.name,i.sync_enabled
 ORDER BY 1,4`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []catalog.Item{}
	for rows.Next() {
		var item catalog.Item
		var enabled bool
		var kind string
		var count int64
		if err := rows.Scan(&item.Code, &item.Name, &enabled, &kind, &count, &item.StartDate, &item.EndDate); err != nil {
			return nil, err
		}
		item.ID = "etf-" + kind + ":" + item.Code
		item.SyncEnabled = &enabled
		item.Rows = &count
		item.Kind = "日线行情"
		if kind == "factor" {
			item.Kind = "复权因子"
		}
		item.Status = "available"
		if count == 0 {
			item.Status = "no_data"
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *MySQLStore) swCoverage(ctx context.Context) ([]catalog.Item, error) {
	out := []catalog.Item{}
	for _, d := range []struct{ id, name, query string }{
		{"sw-dictionary", "行业字典", "SELECT COUNT(*) FROM sw_industry"},
		{"sw-membership", "成分归属", "SELECT COUNT(*) FROM sw_industry_member"},
	} {
		var count int64
		if err := s.db.QueryRowContext(ctx, d.query).Scan(&count); err != nil {
			return nil, err
		}
		item := catalog.Item{ID: d.id, Name: d.name, Kind: "参考数据", Rows: &count, Status: "available", Note: "参考数据不适用行情覆盖日期；此处显示已有记录数。"}
		if count == 0 {
			item.Status = "no_data"
		}
		out = append(out, item)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT i.index_code,i.industry_name,i.level,COUNT(d.trade_date),COALESCE(MIN(d.trade_date),''),COALESCE(MAX(d.trade_date),'') FROM sw_industry i LEFT JOIN sw_index_daily d ON d.ts_code=i.index_code GROUP BY i.index_code,i.industry_name,i.level ORDER BY i.index_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item catalog.Item
		var level string
		var count int64
		if err := rows.Scan(&item.Code, &item.Name, &level, &count, &item.StartDate, &item.EndDate); err != nil {
			return nil, err
		}
		item.ID = "sw-daily:" + item.Code
		item.Kind = level + " 指数日线"
		item.Rows = &count
		item.Status = "available"
		if count == 0 {
			item.Status = "no_data"
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *MySQLStore) indexCoverage(ctx context.Context) ([]catalog.Item, error) {
	var count int64
	item := catalog.Item{ID: "csi:000985.CSI", Code: "000985.CSI", Name: "中证全指", Kind: "指数日线", Status: "available", Note: "Tushare index_daily 原始日线；不参与 ETF 复权或策略候选。"}
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(MIN(trade_date),''),COALESCE(MAX(trade_date),'') FROM index_daily WHERE ts_code=?", item.Code).Scan(&count, &item.StartDate, &item.EndDate)
	if err != nil {
		return nil, err
	}
	item.Rows = &count
	if count == 0 {
		item.Status = "no_data"
	}
	return []catalog.Item{item}, nil
}
