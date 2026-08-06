// Package schema 在服务启动时保证所需的 MySQL 表存在。
// 正式的 schema 迁移工具等表数量增长后再引入（见 issue #1 Out of Scope）。
package schema

import (
	"context"
	"database/sql"
	"fmt"
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
	// 注意：change_amt 对应 Tushare 字段 change（MySQL 保留字，列名避开）。
	`CREATE TABLE IF NOT EXISTS etf_daily (
		ts_code    VARCHAR(20)   NOT NULL,
		trade_date CHAR(8)       NOT NULL COMMENT '交易日，YYYYMMDD',
		open       DECIMAL(12,4) NULL,
		high       DECIMAL(12,4) NULL,
		low        DECIMAL(12,4) NULL,
		close      DECIMAL(12,4) NULL,
		pre_close  DECIMAL(12,4) NULL,
		change_amt DECIMAL(12,4) NULL,
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

	// Intraday Snapshot：14:45 盘中快照（CONTEXT.md 术语），latest 为取数时刻最新价（非收盘）。
	`CREATE TABLE IF NOT EXISTS intraday_snapshot (
		ts_code    VARCHAR(20)   NOT NULL,
		trade_date CHAR(8)       NOT NULL COMMENT '交易日，YYYYMMDD',
		open       DECIMAL(12,4) NULL,
		high       DECIMAL(12,4) NULL,
		low        DECIMAL(12,4) NULL,
		latest     DECIMAL(12,4) NULL COMMENT '取数时刻最新价（非收盘价）',
		adj_mean   DECIMAL(18,6) NULL COMMENT '后复权四点均值 (O+H+L+Latest)/4 × 最新复权因子',
		PRIMARY KEY (ts_code, trade_date)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='盘中快照'`,

	// SW Industry Dictionary：Tushare index_classify（src=SW2021）原样落库。
	`CREATE TABLE IF NOT EXISTS sw_industry (
		index_code    VARCHAR(20)  NOT NULL COMMENT '申万指数代码，如 801010.SI',
		industry_name VARCHAR(100) NOT NULL DEFAULT '' COMMENT '行业名称',
		level         VARCHAR(10)  NOT NULL DEFAULT '' COMMENT 'L1/L2/L3',
		industry_code VARCHAR(20)  NOT NULL DEFAULT '' COMMENT '行业代码（申万内部口径）',
		parent_code   VARCHAR(20)  NOT NULL DEFAULT '' COMMENT '父级行业代码',
		is_pub        VARCHAR(4)   NOT NULL DEFAULT '' COMMENT '是否发布',
		src           VARCHAR(20)  NOT NULL DEFAULT 'SW2021' COMMENT '行业分类来源',
		updated_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		PRIMARY KEY (index_code)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='申万行业字典'`,

	// SW Industry Membership：Tushare index_member_all 成分归属历史（含进出日期）。
	// 一只股票在同一 L1 行业内可能多次进出，(ts_code, l1_code, in_date) 唯一标识一段归属。
	`CREATE TABLE IF NOT EXISTS sw_industry_member (
		ts_code    VARCHAR(20)  NOT NULL COMMENT '股票代码',
		name       VARCHAR(100) NOT NULL DEFAULT '' COMMENT '股票名称',
		l1_code    VARCHAR(20)  NOT NULL,
		l1_name    VARCHAR(100) NOT NULL DEFAULT '',
		l2_code    VARCHAR(20)  NOT NULL DEFAULT '',
		l2_name    VARCHAR(100) NOT NULL DEFAULT '',
		l3_code    VARCHAR(20)  NOT NULL DEFAULT '',
		l3_name    VARCHAR(100) NOT NULL DEFAULT '',
		in_date    CHAR(8)      NOT NULL COMMENT '纳入日期，YYYYMMDD',
		out_date   CHAR(8)      NULL COMMENT '剔除日期，YYYYMMDD；在册为 NULL',
		is_new     CHAR(1)      NOT NULL DEFAULT '' COMMENT 'Y=最新在册 N=历史',
		updated_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		PRIMARY KEY (ts_code, l1_code, in_date),
		KEY idx_l1_code (l1_code),
		KEY idx_l2_code (l2_code)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='申万行业成分归属历史'`,

	// SW Index Daily：Tushare sw_daily 申万行业指数日线原样落库。
	// 注意：change_amt 对应 Tushare 字段 change（MySQL 保留字，列名避开）。
	`CREATE TABLE IF NOT EXISTS sw_index_daily (
		ts_code    VARCHAR(20)   NOT NULL,
		trade_date CHAR(8)       NOT NULL COMMENT '交易日，YYYYMMDD',
		open       DECIMAL(12,4) NULL,
		high       DECIMAL(12,4) NULL,
		low        DECIMAL(12,4) NULL,
		close      DECIMAL(12,4) NULL,
		change_amt DECIMAL(12,4) NULL,
		pct_change DECIMAL(12,4) NULL,
		vol        DECIMAL(20,4) NULL,
		amount     DECIMAL(20,4) NULL,
		PRIMARY KEY (ts_code, trade_date)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='申万行业指数日线（原始）'`,
}

// Ensure 建齐所有表（CREATE TABLE IF NOT EXISTS），可重复调用。
func Ensure(ctx context.Context, db *sql.DB) error {
	for i, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("schema statement %d: %w", i, err)
		}
	}
	return nil
}
