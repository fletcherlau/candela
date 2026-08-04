// Package schema 在服务启动时保证所需的 MySQL 表存在。
// 正式的 schema 迁移工具等表数量增长后再引入（见 issue #1 Out of Scope）。
package schema

import (
	"context"
	"database/sql"
)

var statements = []string{
	// Instrument：纳入每日同步的标的（CONTEXT.md 术语）。
	`CREATE TABLE IF NOT EXISTS instrument (
		ts_code      VARCHAR(20)  NOT NULL COMMENT 'Tushare 标的代码，如 510300.SH',
		name         VARCHAR(100) NOT NULL DEFAULT '' COMMENT '标的名称',
		sync_enabled TINYINT(1)   NOT NULL DEFAULT 1 COMMENT '是否纳入每日同步',
		created_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		PRIMARY KEY (ts_code)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='同步标的'`,

	// Raw Daily Bar：Tushare fund_daily 原样落库。
	`CREATE TABLE IF NOT EXISTS etf_daily (
		ts_code    VARCHAR(20)   NOT NULL,
		trade_date CHAR(8)       NOT NULL COMMENT '交易日，YYYYMMDD',
		open       DECIMAL(12,4) NULL,
		high       DECIMAL(12,4) NULL,
		low        DECIMAL(12,4) NULL,
		close      DECIMAL(12,4) NULL,
		pre_close  DECIMAL(12,4) NULL,
		` + "`change`" + `   DECIMAL(12,4) NULL,
		pct_chg    DECIMAL(12,4) NULL,
		vol        DECIMAL(20,4) NULL,
		amount     DECIMAL(20,4) NULL,
		PRIMARY KEY (ts_code, trade_date)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='ETF 日线行情（原始）'`,

	// Adjustment Factor：Tushare fund_adj 原样落库。
	`CREATE TABLE IF NOT EXISTS etf_adj_factor (
		ts_code    VARCHAR(20)   NOT NULL,
		trade_date CHAR(8)       NOT NULL COMMENT '交易日，YYYYMMDD',
		adj_factor DECIMAL(18,6) NULL,
		PRIMARY KEY (ts_code, trade_date)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='ETF 复权因子（原始）'`,
}

// Ensure 建齐所有表（CREATE TABLE IF NOT EXISTS），可重复调用。
func Ensure(ctx context.Context, db *sql.DB) error {
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
