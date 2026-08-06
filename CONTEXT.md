# CONTEXT

candela 是一个金融数据平台。本文件是全仓库的统一语言（ubiquitous language）：issue、代码、文档中的领域术语以本文件为准。

## 术语

### Instrument（标的）
纳入平台管理的一只金融产品（当前仅 ETF）。以 Tushare 代码（`ts_code`，如 `510300.SH`）为唯一标识。每个 Instrument 有独立的 `sync_enabled` 开关：停用的标的保留历史数据但不参与每日同步。

### Raw Daily Bar（原始日线行情）
某 Instrument 某交易日的日线行情（开高低收/昨收/涨跌额/涨跌幅/量额）。来自 Tushare `fund_daily`，**原样落库，不做任何加工**（ADR-0003）。存储于 `etf_daily` 表，主键 (ts_code, trade_date)。

### Adjustment Factor（复权因子）
某 Instrument 某交易日的复权因子。来自 Tushare `fund_adj`，同样原样落库。存储于 `etf_adj_factor` 表，主键 (ts_code, trade_date)。注意：因子在停牌交易日也会补齐，因此因子表与行情表的日期**可能不对齐**，读取侧 join 时需容忍。

### Intraday Snapshot（盘中快照）
某 Instrument 某交易日的盘中（14:45）快照：实时 OHLC + 取数时刻最新价（**不是收盘价**），以及读取侧算出的后复权四点均值 (O+H+L+Latest)/4 × 最新复权因子。来自腾讯财经 qt.gtimg.cn，存储于 `intraday_snapshot` 表，主键 (ts_code, trade_date)，按主键 upsert **幂等**。

### Incremental Sync（增量同步）
syncer 的核心动作：从每个 Instrument 已存储的最新日期之后继续拉取数据直到今天。起点 = min(日线最新日期, 因子最新日期) 的次日；任一表无历史则退化为 **Full Backfill（全量回填）**——从配置的默认起始日期拉全量。起点已覆盖今天则短路（「已是最新」）。按主键 upsert，**幂等**：重复执行不产生重复行。

### Syncer（同步服务）
本仓库的服务进程（`syncer/`），负责把 Raw Daily Bar 与 Adjustment Factor 从 Tushare 同步到 MySQL。无状态（ADR-0001）：调度由系统 crontab 触发，服务本身不含定时逻辑；同一二进制支持 one-shot 模式（跑完即退）作为调试与兜底通道。

### Signal Card（信号卡片）
某 Instrument 基于盘中快照算出的轮动信号视图：ER 加权动量得分 score、YZ 年化波动率 σ_YZ、分位 q、节流权重 w(q) 与跨标的名次 rank，外加五情形交易建议（现金/持有各标的 → 买入/换入/持有 + 目标仓位，`core.ComputeAdvice` 纯函数按运行手册 §3/§4 推导：安全阀、δ=0.005 差距缓冲、5pp 微调死区、513100 溢价提示）。**无状态**：每次全量重算，建议仅由当日信号推导，不感知真实持仓。新鲜度守卫（stale）标记快照过期或日线历史滞后的标的。由 `POST /api/v1/rotation/signal` 计算（`persist=false` 时只读、Intraday Snapshot 不落库；非交易日回退最近交易日官方收盘口径 `basis=close`，卡片由 `ComputeCloseSignal` 重算），feishubot 渲染成卡片 JSON 2.0 原生表格（信号表 + 建议表）推送。

### Close Report（收盘日报）
每日 18:00 的端到端动作：syncer `POST /api/v1/rotation/close-report` 一条链完成——① Incremental Sync（幂等，已是最新则短路）② Slippage Diff ③ 以**官方收盘价**（Raw Daily Bar 的 close）作当日第 20 点重算信号（打分口径与 Signal Card 相同，仅当日点来源不同）。feishubot `POST /api/v1/push/close-report` 渲染成卡片推送；非交易日或无盘中快照时降级为纯同步摘要，**每天照推**（不同于 14:45 信号卡片的非交易日短路）。

### Slippage Diff（滑点差值）
某 Instrument 某交易日**官方日线**与 **Intraday Snapshot** 的差值：开/高/低/收（收 = 官方 close 对快照 latest）逐字段绝对差 + 相对 bps，外加四点均值差 (O+H+L+C)/4 vs (O+H+L+Latest)/4 的 bps。**bps 以快照为基准**：bps = (官方 − 快照) / 快照 × 10⁴，正值表示官方价高于 14:45 快照（尾盘继续走高）。运行手册 §7 的监控对象（月均 > 10bps 预警）。

### QuoteSource / RealtimeSource / Store（测试接缝）
同步与信号核心的窄接口：QuoteSource 是历史行情数据源（生产实现为 go-tushare 客户端的薄适配，限频由客户端内置），RealtimeSource 是盘中实时行情数据源（生产实现为腾讯财经 qt.gtimg.cn 的薄适配），Store 是存储（生产实现为 MySQL）。核心只依赖这三接口，是全仓库的测试接缝——单测用内存 fake 替换它们。

### Feishubot（机器人服务）
本仓库的服务进程（`feishubot/`），飞书自建应用机器人：持飞书长连接收聊天命令（`status`/`signal`/`sync`/`help`），经 syncer 的 HTTP API 读写数据，是 syncer 之上的界面层。不连 MySQL，内部无调度逻辑（ADR-0004）。

### Daily Report Push（日报推送）
feishubot 的推送动作：系统 crontab curl 触发 feishubot 推送端点（X-Api-Key 鉴权），feishubot 组成卡片，经飞书消息 API 下发到配置的会话（`FEISHU_PUSH_CHAT_ID`）。现有端点：`/api/v1/push/daily-report`（同步状态）、`/api/v1/push/signal-card`（Signal Card，14:45）、`/api/v1/push/close-report`（Close Report，18:00）。调度配置在运维层 crontab，不进代码库。

## 避免使用的说法

- 不说「股票」「基金」泛指——说 **Instrument**（当前同步域只覆盖 ETF）。
- 不说「复权价」——平台只存原料（Raw Daily Bar + Adjustment Factor），复权价是读取侧的**计算结果**，不是存储概念。
- 不说「定时任务」「调度器」——说 **cron 触发**；syncer 内部没有调度器。
