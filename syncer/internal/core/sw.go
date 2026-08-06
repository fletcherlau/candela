// 申万行业同步：字典（index_classify）、成分归属（index_member_all）与
// 行业指数日线（sw_daily）的编排逻辑。
// 与 sync.go 的 ETF 日线同步相互独立，共用同一套“窄接口 + 幂等 upsert + 增量起点”约定。
package core

import (
	"context"
	"fmt"
	"time"
)

// SWIndustry 是一条申万行业字典记录（index_classify，src=SW2021）。
type SWIndustry struct {
	IndexCode    string // 如 801010.SI
	IndustryName string
	Level        string // L1/L2/L3
	IndustryCode string
	ParentCode   string
	IsPub        string
	Src          string
}

// SWMember 是一条申万成分归属记录（index_member_all）。
// OutDate 为空串表示当前在册。
type SWMember struct {
	TsCode  string
	Name    string
	L1Code  string
	L1Name  string
	L2Code  string
	L2Name  string
	L3Code  string
	L3Name  string
	InDate  string // YYYYMMDD
	OutDate string // YYYYMMDD 或 ""
	IsNew   string // Y/N
}

// SWIndexBar 是一条申万行业指数日线（sw_daily）。
type SWIndexBar struct {
	TsCode    string
	TradeDate string // YYYYMMDD
	Open      float64
	High      float64
	Low       float64
	Close     float64
	ChangeAmt float64
	PctChange float64
	Vol       float64
	Amount    float64
}

// SWIndexStatus 是单个行业指数日线的同步状态快照。
type SWIndexStatus struct {
	TsCode          string `json:"tsCode"`
	Name            string `json:"name"`
	Level           string `json:"level"`
	LatestTradeDate string `json:"latestTradeDate"`
	DailyRows       int    `json:"dailyRows"`
}

// SWSource 是申万数据源（生产实现：go-tushare 通用 Query 适配）。
// 限频由数据源客户端内置保证，不在本层。
type SWSource interface {
	// FetchSWDictionary 拉取指定 src（如 SW2021）的全部行业字典记录。
	FetchSWDictionary(ctx context.Context, src string) ([]SWIndustry, error)
	// FetchSWMembership 拉取某 L1 行业下 is_new=Y/N 的成分归属记录。
	FetchSWMembership(ctx context.Context, l1Code, isNew string) ([]SWMember, error)
	// FetchSWIndexDaily 拉取 [startDate, endDate]（YYYYMMDD，闭区间）内的指数日线。
	FetchSWIndexDaily(ctx context.Context, tsCode, startDate, endDate string) ([]SWIndexBar, error)
}

// SWStore 是申万数据存储（生产实现：MySQL）。
type SWStore interface {
	// UpsertSWIndustries 按 index_code 主键 upsert 字典，返回写入行数。幂等。
	UpsertSWIndustries(ctx context.Context, items []SWIndustry) (int, error)
	// ListSWIndustries 返回字典记录；level 为 "L1"/"L2"/"L3" 时过滤，空串返回全部。
	ListSWIndustries(ctx context.Context, level string) ([]SWIndustry, error)
	// CountSWIndustries 返回字典行数。
	CountSWIndustries(ctx context.Context) (int, error)
	// UpsertSWMembers 按 (ts_code, l1_code, in_date) 主键 upsert 成分归属，返回写入行数。幂等。
	UpsertSWMembers(ctx context.Context, items []SWMember) (int, error)
	// CountSWMembers 返回成分归属行数。
	CountSWMembers(ctx context.Context) (int, error)
	// LatestSWIndexDailyDate 返回该指数已存储的最新日线交易日；无历史时返回 ""。
	LatestSWIndexDailyDate(ctx context.Context, tsCode string) (string, error)
	// UpsertSWIndexDaily 按 (ts_code, trade_date) 主键 upsert 指数日线，返回写入行数。幂等。
	UpsertSWIndexDaily(ctx context.Context, bars []SWIndexBar) (int, error)
	// SWIndexStatuses 返回全部行业指数的日线同步状态快照。
	SWIndexStatuses(ctx context.Context) ([]SWIndexStatus, error)
}

// SWIndustrySummary 是字典 + 成分归属一次同步的整体结果。
type SWIndustrySummary struct {
	DictFetched    int    `json:"dictFetched"`
	DictUpserted   int    `json:"dictUpserted"`
	MemberFetched  int    `json:"memberFetched"`
	MemberUpserted int    `json:"memberUpserted"`
	Message        string `json:"message"`
}

// SWDailyResult 是单个行业指数的日线同步结果摘要。
type SWDailyResult struct {
	TsCode    string `json:"tsCode"`
	Name      string `json:"name"`
	Level     string `json:"level"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Fetched   int    `json:"fetched"`
	Upserted  int    `json:"upserted"`
	Message   string `json:"message"`

	success bool
}

// SWDailySummary 是指数日线一次同步触发的整体结果。
type SWDailySummary struct {
	Total   int            `json:"total"`
	Success int            `json:"success"`
	Results []SWDailyResult `json:"results"`
}

// SWSyncer 编排申万行业字典、成分归属与指数日线的同步。零值不可用，用 NewSWSyncer 构造。
type SWSyncer struct {
	source SWSource
	store  SWStore

	chunkDays        int
	defaultStartDate string
	src        string

	today func() string
}

// NewSWSyncer 构造申万同步核心。today 为 nil 时用本地当天。
func NewSWSyncer(source SWSource, store SWStore, chunkDays int, defaultStartDate, src string, today func() string) *SWSyncer {
	if today == nil {
		today = func() string { return time.Now().Format("20060102") }
	}
	if src == "" {
		src = "SW2021"
	}
	return &SWSyncer{
		source:           source,
		store:            store,
		chunkDays:        chunkDays,
		defaultStartDate: defaultStartDate,
		src:        src,
		today:            today,
	}
}

// RunIndustry 同步行业字典与成分归属：先全量刷新字典（upsert 幂等），
// 再按 L1 行业逐个拉取 is_new=Y/N 的成分归属历史。任一环节失败即中断并返回错误信息。
func (s *SWSyncer) RunIndustry(ctx context.Context) SWIndustrySummary {
	sum := SWIndustrySummary{}

	dict, err := s.source.FetchSWDictionary(ctx, s.src)
	if err != nil {
		sum.Message = fmt.Sprintf("拉取行业字典失败: %v", err)
		return sum
	}
	sum.DictFetched = len(dict)
	n, err := s.store.UpsertSWIndustries(ctx, dict)
	if err != nil {
		sum.Message = fmt.Sprintf("写入行业字典失败: %v", err)
		return sum
	}
	sum.DictUpserted = n

	// 成分归属按 L1 行业逐个拉取：一次调用即带齐该行业全部股票的 L1/L2/L3 归属。
	l1Seen := make(map[string]bool)
	var l1Codes []string
	for _, d := range dict {
		if d.Level == "L1" && !l1Seen[d.IndexCode] {
			l1Seen[d.IndexCode] = true
			l1Codes = append(l1Codes, d.IndexCode)
		}
	}
	for _, code := range l1Codes {
		for _, isNew := range []string{"Y", "N"} {
			members, err := s.source.FetchSWMembership(ctx, code, isNew)
			if err != nil {
				sum.Message = fmt.Sprintf("拉取成分归属失败(l1_code=%s is_new=%s): %v", code, isNew, err)
				return sum
			}
			sum.MemberFetched += len(members)
			n, err := s.store.UpsertSWMembers(ctx, members)
			if err != nil {
				sum.Message = fmt.Sprintf("写入成分归属失败(l1_code=%s is_new=%s): %v", code, isNew, err)
				return sum
			}
			sum.MemberUpserted += n
		}
	}

	sum.Message = "ok"
	return sum
}

// RunDaily 对指定行业指数（为空则字典内全部指数）执行日线增量同步。
// 单个指数失败不中断其余指数，失败信息记入该指数的 Result.Message。
func (s *SWSyncer) RunDaily(ctx context.Context, tsCodes []string) SWDailySummary {
	indexes, err := s.resolveIndexes(ctx, tsCodes)
	if err != nil {
		return SWDailySummary{Results: []SWDailyResult{{Message: err.Error()}}}
	}

	sum := SWDailySummary{Total: len(indexes)}
	for _, idx := range indexes {
		res := s.syncIndexDaily(ctx, idx)
		if res.success {
			sum.Success++
		}
		sum.Results = append(sum.Results, res)
	}
	return sum
}

func (s *SWSyncer) resolveIndexes(ctx context.Context, tsCodes []string) ([]SWIndustry, error) {
	if len(tsCodes) == 0 {
		return s.store.ListSWIndustries(ctx, "")
	}
	indexes := make([]SWIndustry, 0, len(tsCodes))
	for _, code := range tsCodes {
		indexes = append(indexes, SWIndustry{IndexCode: code})
	}
	return indexes, nil
}

func (s *SWSyncer) syncIndexDaily(ctx context.Context, idx SWIndustry) SWDailyResult {
	res := SWDailyResult{TsCode: idx.IndexCode, Name: idx.IndustryName, Level: idx.Level}
	today := s.today()
	if _, err := time.Parse("20060102", today); err != nil {
		res.Message = fmt.Sprintf("today 注入值格式非法: %q", today)
		return res
	}

	start, msg := s.dailyStart(ctx, idx.IndexCode)
	if msg != "" {
		res.Message = msg
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

		bars, err := s.source.FetchSWIndexDaily(ctx, idx.IndexCode, chunkStart, chunkEnd)
		if err != nil {
			res.Message = fmt.Sprintf("拉取指数日线失败: %v", err)
			return res
		}
		res.Fetched += len(bars)
		n, err := s.store.UpsertSWIndexDaily(ctx, bars)
		if err != nil {
			res.Message = fmt.Sprintf("写入指数日线失败: %v", err)
			return res
		}
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

// dailyStart 计算指数日线增量起点：有历史时取最新交易日次日，无历史则从默认起始日期全量回填。
func (s *SWSyncer) dailyStart(ctx context.Context, tsCode string) (start string, msg string) {
	latest, err := s.store.LatestSWIndexDailyDate(ctx, tsCode)
	if err != nil {
		return "", fmt.Sprintf("查询指数日线最新交易日失败: %v", err)
	}
	if latest == "" {
		return s.defaultStartDate, ""
	}
	if _, err := time.Parse("20060102", latest); err != nil {
		return "", fmt.Sprintf("存储中的日线最新交易日格式非法: %q", latest)
	}
	start, err = nextDay(latest)
	if err != nil {
		return "", fmt.Sprintf("存储中的最新日期格式非法: %q", latest)
	}
	return start, ""
}
