# CONTEXT

candela 是一个金融数据平台。本文件是全仓库的统一语言（ubiquitous language）：issue、代码、文档中的领域术语以本文件为准。

## 术语

### Market Overview（市场总览，产品导航称“市场状态”）
以中证全指行情、MACD 及正式／暂算市场状态为内容的市场环境展示视图，供人工选择策略类型参考，与承担同步维护操作的 Data Management 分开呈现。指标和市场状态以修正后的完整历史为准，不保留修订前的市场状态版本。

### Index Series（指数序列）
用于观察市场或行业表现的指数，不是可交易的 Instrument。市场总览首个序列为中证全指（000985.CSI），与 ETF 及申万行业指数分别识别。

### Index Daily Bar（指数日线）
Index Series 在一个交易日的官方开高低收、涨跌及量额数据，不附加 ETF 复权因子或盘中快照。首次回填覆盖数据源可取得的全部历史。源端未提供的昨收、涨跌和量额保留为空值，不补零；交易日期与开高低收仍为必需数据。

### Monthly MACD Histogram（月度 MACD 柱）
基于完整历史月收盘价计算的 MACD（12、26、9）中 DIF 与 DEA 的差，不乘以二。EMA 以首个输入值初始化，前 33 个月只展示价格，自第 34 个月起提供指标和市场状态。

### Completed Monthly Bar（已完成月线）
该月交易日历中的最后一个交易日已收盘，且相应官方日线已同步的月线。不能仅依据自然月结束或当前数据库的最后一条记录判断完成。

### Monthly MACD Preview（月度 MACD 暂算）
以当月最新已同步的官方日线收盘价作为当月收盘输入计算的月度 MACD，标记未确认、所属月份和数据截止日。它不使用盘中报价，仍受历史长度与数据有效性约束。

### Market Regime（正式市场状态）
最新应完成月份在满足数据与指标条件后确认的月度市场状态：MACD 柱大于或等于零为牛市，小于零为熊市。切换行情图周期不会改变该状态的月度口径。

### Provisional Market Regime（暂算市场状态）
由当月 Monthly MACD Preview 推导的未确认牛熊状态，可与正式状态不同，但不能替代正式状态。

### Regime Pending Sync（正式状态待同步）
最新应完成月份缺少必要官方日线时，当前正式状态的不可用状态。暂停正式策略提示，历史结果仅按其真实月份保留展示。

### Preview Staleness（暂算数据滞后）
暂算结果使用的数据落后于当前应取得的官方收盘数据的状态，需同时展示截止日和滞后提示。月中更新失败不使已完整确认的正式月份自动失效。

### Strategy Guidance（策略类型提示）
依据月度市场状态提供的人工参考：牛市对应趋势跟踪，熊市对应反弹策略。正式与暂算提示分别展示，不自动切换策略或产生标的买卖指令。

### Published Market Result（已发布市场结果）
供市场总览读取的一套完整、一致的行情、指标及市场状态结果；更新期间可暂时保留上一套完整结果，并显式反映更新进度、缺数及滞后状态。新结果成功后整体替换，不形成旧市场状态的历史版本档案。

### Data Management（数据管理）
对中证全指、ETF 和申万数据进行增量同步、历史重同步等维护的独立能力，按数据类别组织并通过专门的数据管理页面使用。它与市场总览等数据展示能力分开呈现，位于独立 `/admin` 管理区域，前台导航不提供管理入口。

### Instrument（标的）
纳入平台管理的一只金融产品（当前仅 ETF）。以 Tushare 代码（`ts_code`，如 `510300.SH`）为唯一标识。每个 Instrument 有独立的 `sync_enabled` 开关：停用的标的保留历史数据但不参与每日同步，重新启用后恢复增量同步；仍属于某个 Strategy Universe 的标的不可停用同步。

### Instrument Enrollment（标的接入）
在 Data Management 中提交 ETF 代码并经校验，将该 ETF 纳入平台同步范围的过程。新接入的 Instrument 首次回填数据源可取得的全部历史，之后参与每日增量同步，不自动加入任何 Strategy Universe。

### Strategy Universe（策略候选名单）
某个策略明确选定参与计算的 Instrument 集合，与数据同步名单分别管理。Instrument 只有显式加入该集合后才参与对应策略的计算。

### Raw Daily Bar（原始日线行情）
某 Instrument 某交易日的日线行情（开高低收/昨收/涨跌额/涨跌幅/量额）。来自 Tushare `fund_daily`，**原样落库，不做任何加工**（ADR-0003）。存储于 `etf_daily` 表，主键 (ts_code, trade_date)。

### Adjustment Factor（复权因子）
某 Instrument 某交易日的复权因子。来自 Tushare `fund_adj`，同样原样落库。存储于 `etf_adj_factor` 表，主键 (ts_code, trade_date)。注意：因子在停牌交易日也会补齐，因此因子表与行情表的日期**可能不对齐**，读取侧 join 时需容忍。

### Intraday Snapshot（盘中快照）
某 Instrument 某交易日的盘中（14:45）快照：实时 OHLC + 取数时刻最新价（**不是收盘价**），以及读取侧算出的后复权四点均值 (O+H+L+Latest)/4 × 最新复权因子。来自腾讯财经 qt.gtimg.cn，存储于 `intraday_snapshot` 表，主键 (ts_code, trade_date)，按主键 upsert **幂等**。

### Incremental Sync（增量同步）
追补最新行情的同步动作。Index Series 从提交时已存储的最后一个交易日（含该日）增量获取，按北京时间 18:00 的边界固定本次截止日；无历史则回填源端全部可取得历史。按主键 upsert，重复执行不产生重复行。

现有 ETF 路径（后续迁入持久执行）：从每个 Instrument 已存储的最新日期之后继续拉取数据直到今天。起点 = min(日线最新日期, 因子最新日期) 的次日；任一表无历史则退化为 **Full Backfill（全量回填）**——从配置的默认起始日期拉全量。起点已覆盖今天则短路（「已是最新」）。按主键 upsert，**幂等**：重复执行不产生重复行。

### Historical Resync（历史重同步）
对选定日线数据的指定起止日期区间重新从数据源获取数据，补齐缺失记录并更新已有记录的维护操作。区间外数据保持不变，与追补最新数据的 Incremental Sync 区分。

### Sync Run（同步执行）
一次可独立跟踪的数据同步过程，不因页面切换或浏览器关闭而终止，其进度、结果及失败原因作为执行记录保留并可在 Data Management 查询。同一数据对象的 Sync Run 依次执行，新请求排队，完全相同且尚未结束的请求复用已有执行记录。中证全指已实现持久接受、固定日期边界、对象互斥和分段事务检查点；已处理记录数包含幂等重写，不表示实际改动行数。

### Sync Cancellation（同步取消）
由用户主动终止排队中或执行中的 Sync Run 的操作，停止后续处理并保留已写入的数据。取消后的执行记录标记为“已取消”，不参与 Sync Recovery。

### Sync Retry（同步重试）
针对某次批量同步中的失败数据对象重新发起同步的维护操作。成功对象的已写入数据和成功结果保留，不参与这次重试。

### Sync Recovery（同步恢复）
未取消的 Sync Run 因服务重启或机器中断而停止后，在服务恢复时沿用原数据对象和日期范围自动继续未完成部分的行为。已完成部分保留，中断与恢复记入该次同步的执行记录。该能力由 T03 实现；T02 暂将执行权过期记录显式标记为中断失败，不自动续跑。

### Syncer（同步服务）
负责维护平台行情数据并承接 Sync Run 的同步服务。每日业务同步由外部 cron 触发，已提交工作的排队、取消及中断恢复属于同步执行职责。

### Four-ETF Rotation Page（四标的轮动页）
面向已了解该策略的持续跟踪者，供其查看每日计算数据、按历史交易日复盘，并观察策略长期历史模拟表现的页面；操作由用户自行判断，教学内容由独立教程承载。

### Pre-close Reference（收盘前参考）
某交易日固定的 14:45 价格快照及据此形成的一组策略计算数据，供用户自行判断，并作为与该日收盘后结果比较的基准；不随之后的盘中行情更新，不包含操作建议。

### Post-close Result（收盘后结果）
基于某交易日当前有效的完整四标的收盘行情形成的一组策略计算数据，用于查看当日收盘状态，并与该日收盘前参考比较；随收盘行情更正而更新，不保留该日旧版收盘结果，不包含操作建议。

### Momentum Score（动量得分）
四标的轮动策略使用的 20 日 ER 加权动量值。取含当日在内的最近 20 个日度数据点，以复权开、高、低、收的均价计算对数价格净变化，再按趋势效率加权；收盘前参考的当日收价使用 14:45 快照价。收盘前参考与收盘后结果遵循同一公式，标的按该值从高到低排名。

### Volatility-based Position Ratio（波动率分位计算仓位）
按每个标的的波动率分位和既定公式计算出的仓位比例，是供用户自行判断操作的策略计算指标。

### Rotation Backtest（轮动历史回测）
固定四标的（红利、黄金、创业板、纳指）按定稿 v1.4 规则连续计算的单位净值、回撤与持仓历史。采用收盘价近似成交、单边 10bps、5 个百分点调仓门槛、零现金收益，无安全阀或吊灯；不是实际账户收益。默认观察一年，缩放只改变显示区间，不重新初始化持仓。正式公告确认的整日停牌不参与交易／指标采样，只延续最近有效收盘估值；未知行情缺口阻止后续发布。

### Published Rotation Result（已发布轮动结果）
一套完整的轮动历史回测及四标的对照序列，由已完成的 ETF 日线／因子同步触发重算，持久保存当前结果和更新状态。行情修订以版本条件保护完整发布，更新中／失败保留旧结果与真实截止日；查询不触发行情拉取。

### Signal Card（信号卡片）
某 Instrument 基于盘中快照算出的轮动信号视图：ER 加权动量得分 score、YZ 年化波动率 σ_YZ、分位 q、节流权重 w(q) 与跨标的名次 rank，外加五情形交易建议（现金/持有各标的 → 买入/换入/持有 + 目标仓位，`core.ComputeAdvice` 纯函数按运行手册 §3/§4 推导：安全阀、δ=0.005 差距缓冲、5pp 微调死区、513100 溢价提示）。**无状态**：每次全量重算，建议仅由当日信号推导，不感知真实持仓。新鲜度守卫（stale）标记快照过期或日线历史滞后的标的。由 `POST /api/v1/rotation/signal` 计算（`persist=false` 时只读、Intraday Snapshot 不落库；非交易日回退最近交易日官方收盘口径 `basis=close`，卡片由 `ComputeCloseSignal` 重算），feishubot 渲染成卡片 JSON 2.0 原生表格（信号表 + 建议表）推送。

### Close Report（收盘日报）
每日 18:00 的端到端动作：syncer `POST /api/v1/rotation/close-report` 一条链完成——① Incremental Sync（幂等，已是最新则短路）② Slippage Diff ③ 以**官方收盘价**（Raw Daily Bar 的 close）作当日第 20 点重算信号（打分口径与 Signal Card 相同，仅当日点来源不同）。feishubot `POST /api/v1/push/close-report` 渲染成卡片推送；非交易日或无盘中快照时降级为纯同步摘要，**每天照推**（不同于 14:45 信号卡片的非交易日短路）。

### Slippage Diff（滑点差值）
某 Instrument 某交易日**官方日线**与 **Intraday Snapshot** 的差值：开/高/低/收（收 = 官方 close 对快照 latest）逐字段绝对差 + 相对 bps，外加四点均值差 (O+H+L+C)/4 vs (O+H+L+Latest)/4 的 bps。**bps 以快照为基准**：bps = (官方 − 快照) / 快照 × 10⁴，正值表示官方价高于 14:45 快照（尾盘继续走高）。运行手册 §7 的监控对象（月均 > 10bps 预警）。

### QuoteSource / RealtimeSource / Store（测试接缝）
同步与信号核心的窄接口：QuoteSource 是历史行情数据源（生产实现为 go-tushare 客户端的薄适配，限频由客户端内置），RealtimeSource 是盘中实时行情数据源（生产实现为腾讯财经 qt.gtimg.cn 的薄适配），Store 是存储（生产实现为 MySQL）。现有 ETF／申万核心以这些接口为测试接缝。持久指数同步位于 `internal/syncrun`，以 Source 接口替换行情源，并使用隔离真实 MySQL 验证执行权和事务行为。

### Feishubot（机器人服务）
本仓库的服务进程（`feishubot/`），飞书自建应用机器人：持飞书长连接收聊天命令（`status`/`signal`/`sync`/`help`），经 syncer 的 HTTP API 读写数据，是 syncer 之上的界面层。不连 MySQL，内部无调度逻辑（ADR-0004）。

### Daily Report Push（日报推送）
feishubot 的推送动作：系统 crontab curl 触发 feishubot 推送端点（X-Api-Key 鉴权），feishubot 组成卡片，经飞书消息 API 下发到配置的会话（`FEISHU_PUSH_CHAT_ID`）。现有端点：`/api/v1/push/daily-report`（同步状态）、`/api/v1/push/signal-card`（Signal Card，14:45）、`/api/v1/push/close-report`（Close Report，18:00）。调度配置在运维层 crontab，不进代码库。

## 避免使用的说法

- 不说「股票」「基金」泛指——说 **Instrument**（当前同步域只覆盖 ETF）。
- 不说「复权价」——平台只存原料（Raw Daily Bar + Adjustment Factor），复权价是读取侧的**计算结果**，不是存储概念。
- 不把每日业务触发与同步执行协调混称为「调度器」——每日时间安排说 **cron 触发**，已提交工作的执行与恢复说 **Sync Run / Sync Recovery**。
