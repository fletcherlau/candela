// Package core 编排 ETF 日线行情的增量同步。
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

// Instrument 是纳入同步的标的。
type Instrument struct {
	TsCode string
	Name   string
}

// QuoteSource 是行情数据源（生产实现：Tushare 客户端）。
type QuoteSource interface {
	// FetchDaily 拉取 [startDate, endDate]（YYYYMMDD，闭区间）内的日线行情。
	FetchDaily(ctx context.Context, tsCode, startDate, endDate string) ([]Bar, error)
}

// Store 是存储（生产实现：MySQL）。
type Store interface {
	// ListSyncEnabled 返回全部启用同步的 Instrument。
	ListSyncEnabled(ctx context.Context) ([]Instrument, error)
	// LatestDailyDate 返回该标的已存储的最新交易日（YYYYMMDD）；无历史时返回 ""。
	LatestDailyDate(ctx context.Context, tsCode string) (string, error)
	// UpsertDaily 按 (ts_code, trade_date) 主键 upsert，返回影响行数。幂等。
	UpsertDaily(ctx context.Context, bars []Bar) (int, error)
}

// Result 是单个标的的同步结果摘要。
type Result struct {
	TsCode    string `json:"tsCode"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Fetched   int    `json:"fetched"`
	Upserted  int    `json:"upserted"`
	Message   string `json:"message"`
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

	// wait 在每次向数据源发起拉取前调用；生产接节流器，测试可观测调用次数。
	wait func(ctx context.Context) error
	// today 返回今天（YYYYMMDD），注入以便测试。
	today func() string
}

// NewSyncer 构造同步核心。wait 为 nil 时不节流；today 为 nil 时用本地当天。
func NewSyncer(source QuoteSource, store Store, chunkDays int, defaultStartDate string, wait func(context.Context) error, today func() string) *Syncer {
	if wait == nil {
		wait = func(context.Context) error { return nil }
	}
	if today == nil {
		today = func() string { return time.Now().Format("20060102") }
	}
	return &Syncer{
		source:           source,
		store:            store,
		chunkDays:        chunkDays,
		defaultStartDate: defaultStartDate,
		wait:             wait,
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
		if res.Message == "ok" || res.Message == "已是最新" {
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

	latest, err := s.store.LatestDailyDate(ctx, tsCode)
	if err != nil {
		res.Message = fmt.Sprintf("查询最新交易日失败: %v", err)
		return res
	}

	start := s.defaultStartDate
	if latest != "" {
		start = nextDay(latest)
	}
	res.StartDate = start
	res.EndDate = today

	if start > today {
		res.Message = "已是最新"
		return res
	}

	for chunkStart := start; chunkStart <= today; {
		chunkEnd := addDays(chunkStart, s.chunkDays-1)
		if chunkEnd > today {
			chunkEnd = today
		}

		if err := s.wait(ctx); err != nil {
			res.Message = fmt.Sprintf("节流等待中断: %v", err)
			return res
		}
		bars, err := s.source.FetchDaily(ctx, tsCode, chunkStart, chunkEnd)
		if err != nil {
			res.Message = fmt.Sprintf("拉取行情失败: %v", err)
			return res
		}
		res.Fetched += len(bars)

		n, err := s.store.UpsertDaily(ctx, bars)
		if err != nil {
			res.Message = fmt.Sprintf("写入存储失败: %v", err)
			return res
		}
		res.Upserted += n

		chunkStart = nextDay(chunkEnd)
	}

	res.Message = "ok"
	return res
}

func nextDay(date string) string { return addDays(date, 1) }

func addDays(date string, days int) string {
	t, err := time.Parse("20060102", date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, days).Format("20060102")
}
