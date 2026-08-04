// Package core 编排 ETF 日线行情与复权因子的增量同步。
// 它只依赖 QuoteSource 与 Store 两个窄接口，不感知 Tushare HTTP 或 MySQL 细节，
// 是全仓库唯一的测试接缝（见 issue #1 Testing Decisions）。
package core

import (
	"context"
	"fmt"
	"time"
)

// Bar 是一条原始日线行情（Raw Daily Bar）。
type Bar struct {
	TsCode    string
	TradeDate string // YYYYMMDD
	Open      float64
	High      float64
	Low       float64
	Close     float64
	PreClose  float64
	ChangeAmt float64
	PctChg    float64
	Vol       float64
	Amount    float64
}

// AdjFactor 是一条原始复权因子（Adjustment Factor）。
type AdjFactor struct {
	TsCode    string
	TradeDate string // YYYYMMDD
	AdjFactor float64
}

// Instrument 是纳入同步的标的。
type Instrument struct {
	TsCode string
	Name   string
}

// QuoteSource 是行情数据源（生产实现：go-tushare 客户端的薄适配）。
// 限频由数据源客户端内置保证，不在本层。
type QuoteSource interface {
	// FetchDaily 拉取 [startDate, endDate]（YYYYMMDD，闭区间）内的日线行情。
	FetchDaily(ctx context.Context, tsCode, startDate, endDate string) ([]Bar, error)
	// FetchAdj 拉取 [startDate, endDate]（YYYYMMDD，闭区间）内的复权因子。
	FetchAdj(ctx context.Context, tsCode, startDate, endDate string) ([]AdjFactor, error)
}

// InstrumentStatus 是单个标的的同步状态快照（status 接口用）。
type InstrumentStatus struct {
	TsCode          string `json:"tsCode"`
	Name            string `json:"name"`
	SyncEnabled     bool   `json:"syncEnabled"`
	LatestTradeDate string `json:"latestTradeDate"` // 从未同步过时为 ""
	DailyRows       int    `json:"dailyRows"`
	AdjRows         int    `json:"adjRows"`
}

// Store 是存储（生产实现：MySQL）。
type Store interface {
	// ListSyncEnabled 返回全部启用同步的 Instrument。
	ListSyncEnabled(ctx context.Context) ([]Instrument, error)
	// Statuses 返回全部 Instrument（含已停用）的同步状态快照。
	Statuses(ctx context.Context) ([]InstrumentStatus, error)
	// LatestDailyDate 返回该标的已存储的最新日线交易日（YYYYMMDD）；无历史时返回 ""。
	LatestDailyDate(ctx context.Context, tsCode string) (string, error)
	// LatestAdjDate 返回该标的已存储的最新因子日期（YYYYMMDD）；无历史时返回 ""。
	LatestAdjDate(ctx context.Context, tsCode string) (string, error)
	// UpsertDaily 按 (ts_code, trade_date) 主键 upsert 日线，返回写入行数。幂等。
	UpsertDaily(ctx context.Context, bars []Bar) (int, error)
	// UpsertAdjFactors 按 (ts_code, trade_date) 主键 upsert 因子，返回写入行数。幂等。
	UpsertAdjFactors(ctx context.Context, factors []AdjFactor) (int, error)
}

// Result 是单个标的的同步结果摘要。
type Result struct {
	TsCode    string `json:"tsCode"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	// Fetched/Upserted 是日线 + 因子的合计；分项见 Daily/Adj 前缀字段。
	Fetched       int    `json:"fetched"`
	Upserted      int    `json:"upserted"`
	DailyFetched  int    `json:"dailyFetched"`
	AdjFetched    int    `json:"adjFetched"`
	DailyUpserted int    `json:"dailyUpserted"`
	AdjUpserted   int    `json:"adjUpserted"`
	Message       string `json:"message"`

	success bool // 供 Summary 计数，不随 JSON 暴露
}

// Summary 是一次同步触发的整体结果。
type Summary struct {
	Total   int      `json:"total"`
	Success int      `json:"success"`
	Results []Result `json:"results"`
}

// Syncer 编排增量同步。零值不可用，用 NewSyncer 构造。
type Syncer struct {
	source QuoteSource
	store  Store

	chunkDays        int
	defaultStartDate string

	// today 返回今天（YYYYMMDD），注入以便测试。
	today func() string
}

// NewSyncer 构造同步核心。today 为 nil 时用本地当天。
func NewSyncer(source QuoteSource, store Store, chunkDays int, defaultStartDate string, today func() string) *Syncer {
	if today == nil {
		today = func() string { return time.Now().Format("20060102") }
	}
	return &Syncer{
		source:           source,
		store:            store,
		chunkDays:        chunkDays,
		defaultStartDate: defaultStartDate,
		today:            today,
	}
}

// Run 对指定标的（为空则全部启用同步的标的）执行增量同步。
// 单个标的失败不中断其余标的，失败信息记入该标的的 Result.Message。
func (s *Syncer) Run(ctx context.Context, tsCodes []string) Summary {
	instruments, err := s.resolveInstruments(ctx, tsCodes)
	if err != nil {
		return Summary{Results: []Result{{Message: err.Error()}}}
	}

	sum := Summary{Total: len(instruments)}
	for _, inst := range instruments {
		res := s.syncOne(ctx, inst.TsCode)
		if res.success {
			sum.Success++
		}
		sum.Results = append(sum.Results, res)
	}
	return sum
}

func (s *Syncer) resolveInstruments(ctx context.Context, tsCodes []string) ([]Instrument, error) {
	if len(tsCodes) == 0 {
		return s.store.ListSyncEnabled(ctx)
	}
	instruments := make([]Instrument, 0, len(tsCodes))
	for _, code := range tsCodes {
		instruments = append(instruments, Instrument{TsCode: code})
	}
	return instruments, nil
}

func (s *Syncer) syncOne(ctx context.Context, tsCode string) Result {
	res := Result{TsCode: tsCode}
	today := s.today()
	if _, err := time.Parse("20060102", today); err != nil {
		res.Message = fmt.Sprintf("today 注入值格式非法: %q", today)
		return res
	}

	start, msg := s.syncStart(ctx, tsCode)
	if msg != "" {
		res.Message = msg
		return res
	}
	if _, err := time.Parse("20060102", start); err != nil {
		res.Message = fmt.Sprintf("默认起始日期格式非法: %q", start)
		return res
	}
	res.StartDate = start
	res.EndDate = today

	if start > today {
		res.Message = "已是最新"
		res.success = true
		return res
	}

	for chunkStart := start; chunkStart <= today; {
		chunkEnd, err := addDays(chunkStart, s.chunkDays-1)
		if err != nil {
			res.Message = fmt.Sprintf("分片日期计算失败: %v", err)
			return res
		}
		if chunkEnd > today {
			chunkEnd = today
		}

		bars, err := s.source.FetchDaily(ctx, tsCode, chunkStart, chunkEnd)
		if err != nil {
			res.Message = fmt.Sprintf("拉取行情失败: %v", err)
			return res
		}
		res.DailyFetched += len(bars)
		res.Fetched += len(bars)
		n, err := s.store.UpsertDaily(ctx, bars)
		if err != nil {
			res.Message = fmt.Sprintf("写入行情失败: %v", err)
			return res
		}
		res.DailyUpserted += n
		res.Upserted += n

		// 注意顺序：日线先于因子写入。若因子拉取失败，本分片日线已落库而因子滞后，
		// 下一轮 min 起点会由因子侧驱动回补，配合 upsert 幂等保证恢复安全。
		factors, err := s.source.FetchAdj(ctx, tsCode, chunkStart, chunkEnd)
		if err != nil {
			res.Message = fmt.Sprintf("拉取复权因子失败: %v", err)
			return res
		}
		res.AdjFetched += len(factors)
		res.Fetched += len(factors)
		n, err = s.store.UpsertAdjFactors(ctx, factors)
		if err != nil {
			res.Message = fmt.Sprintf("写入复权因子失败: %v", err)
			return res
		}
		res.AdjUpserted += n
		res.Upserted += n

		chunkStart, err = nextDay(chunkEnd)
		if err != nil {
			res.Message = fmt.Sprintf("分片日期计算失败: %v", err)
			return res
		}
	}

	res.Message = "ok"
	res.success = true
	return res
}

// syncStart 计算增量起点：日线与因子皆有历史时取两者较旧者的次日；
// 任一缺失（或皆空）则从默认起始日期全量回填，确保两张表不长期错位。
// 返回空 msg 表示正常。
func (s *Syncer) syncStart(ctx context.Context, tsCode string) (start string, msg string) {
	latestDaily, err := s.store.LatestDailyDate(ctx, tsCode)
	if err != nil {
		return "", fmt.Sprintf("查询日线最新交易日失败: %v", err)
	}
	latestAdj, err := s.store.LatestAdjDate(ctx, tsCode)
	if err != nil {
		return "", fmt.Sprintf("查询因子最新日期失败: %v", err)
	}

	if latestDaily == "" || latestAdj == "" {
		return s.defaultStartDate, ""
	}

	// 两侧都校验后再比较：单边脏值不能被字符串比较静默掩盖。
	if _, err := time.Parse("20060102", latestDaily); err != nil {
		return "", fmt.Sprintf("存储中的日线最新交易日格式非法: %q", latestDaily)
	}
	if _, err := time.Parse("20060102", latestAdj); err != nil {
		return "", fmt.Sprintf("存储中的因子最新日期格式非法: %q", latestAdj)
	}

	older := latestDaily
	if latestAdj < older {
		older = latestAdj
	}
	start, err = nextDay(older)
	if err != nil {
		return "", fmt.Sprintf("存储中的最新日期格式非法: %q", older)
	}
	return start, ""
}

func nextDay(date string) (string, error) { return addDays(date, 1) }

func addDays(date string, days int) (string, error) {
	t, err := time.Parse("20060102", date)
	if err != nil {
		return "", fmt.Errorf("非法日期 %q（期望 YYYYMMDD）", date)
	}
	return t.AddDate(0, 0, days).Format("20060102"), nil
}
