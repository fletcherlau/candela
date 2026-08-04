# CONTEXT

candela 是一个金融数据平台。本文件是全仓库的统一语言（ubiquitous language）：issue、代码、文档中的领域术语以本文件为准。

## 术语

### Instrument（标的）
纳入平台管理的一只金融产品（当前仅 ETF）。以 Tushare 代码（`ts_code`，如 `510300.SH`）为唯一标识。每个 Instrument 有独立的 `sync_enabled` 开关：停用的标的保留历史数据但不参与每日同步。

### Raw Daily Bar（原始日线行情）
某 Instrument 某交易日的日线行情（开高低收/昨收/涨跌额/涨跌幅/量额）。来自 Tushare `fund_daily`，**原样落库，不做任何加工**（ADR-0003）。存储于 `etf_daily` 表，主键 (ts_code, trade_date)。

### Adjustment Factor（复权因子）
某 Instrument 某交易日的复权因子。来自 Tushare `fund_adj`，同样原样落库。存储于 `etf_adj_factor` 表，主键 (ts_code, trade_date)。注意：因子在停牌交易日也会补齐，因此因子表与行情表的日期**可能不对齐**，读取侧 join 时需容忍。

### Incremental Sync（增量同步）
syncer 的核心动作：从每个 Instrument 已存储的最新日期之后继续拉取数据直到今天。起点 = min(日线最新日期, 因子最新日期) 的次日；任一表无历史则退化为 **Full Backfill（全量回填）**——从配置的默认起始日期拉全量。起点已覆盖今天则短路（「已是最新」）。按主键 upsert，**幂等**：重复执行不产生重复行。

### Syncer（同步服务）
本仓库的服务进程（`syncer/`），负责把 Raw Daily Bar 与 Adjustment Factor 从 Tushare 同步到 MySQL。无状态（ADR-0001）：调度由系统 crontab 触发，服务本身不含定时逻辑；同一二进制支持 one-shot 模式（跑完即退）作为调试与兜底通道。

### QuoteSource / Store（测试接缝）
同步核心的两个窄接口：QuoteSource 是行情数据源（生产实现为 go-tushare 客户端的薄适配，限频由客户端内置），Store 是存储（生产实现为 MySQL）。同步核心只依赖这两个接口，是全仓库唯一的测试接缝——单测用内存 fake 替换二者。

### Feishubot（机器人服务）
本仓库的服务进程（`feishubot/`），飞书自建应用机器人：持飞书长连接收聊天命令（`status`/`sync`/`help`），经 syncer 的 HTTP API 读写数据，是 syncer 之上的界面层。不连 MySQL，内部无调度逻辑（ADR-0004）。

### Daily Report Push（日报推送）
feishubot 的推送动作：系统 crontab curl 触发 `POST /api/v1/push/daily-report`（X-Api-Key 鉴权），feishubot 拉取同步状态组成卡片，经飞书消息 API 下发到配置的会话（`FEISHU_PUSH_CHAT_ID`）。每日 14:45 / 18:00 各一次，调度配置在运维层 crontab，不进代码库。

## 避免使用的说法

- 不说「股票」「基金」泛指——说 **Instrument**（当前同步域只覆盖 ETF）。
- 不说「复权价」——平台只存原料（Raw Daily Bar + Adjustment Factor），复权价是读取侧的**计算结果**，不是存储概念。
- 不说「定时任务」「调度器」——说 **cron 触发**；syncer 内部没有调度器。
