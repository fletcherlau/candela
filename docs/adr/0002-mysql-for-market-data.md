# ADR-0002: MySQL 作为行情存储

- 状态：已接受
- 日期：2026-08-03（issue #1 决策，2026-08-04 补录）

## 背景

行情数据（Raw Daily Bar、Adjustment Factor）需要一份本地持久化存储，供后续回测、持仓管理等功能消费。候选：MySQL、PostgreSQL、SQLite、时序数据库（InfluxDB 等）。

## 决策

使用 **MySQL 8**（开发库跑 Docker 容器，数据卷持久化）。表结构当前由服务启动时 `CREATE TABLE IF NOT EXISTS` 保证；正式的 schema 迁移工具等表数量增长后再引入。

关键表设计：

- `instrument`：ts_code 主键、name、sync_enabled、时间戳。
- `etf_daily` / `etf_adj_factor`：均为 (ts_code, trade_date) 复合主键，按主键 upsert 保证幂等。
- 字符集 utf8mb4；`trade_date` 用 CHAR(8) 存 YYYYMMDD 原始形态。

## 后果

- 团队熟悉度高，运维成本低；复合主键天然支撑「(Instrument, 交易日) 幂等 upsert」的同步语义。
- 后续回测等读取侧可直接 SQL 查询，无需额外数据搬运。
- 代价：行情是典型时序数据，MySQL 在超大规模下不如专用时序库；当前 ETF 数量级（每标的每年约 250 行）远未触及瓶颈，未来如需迁移以 Store 接口为接缝。
