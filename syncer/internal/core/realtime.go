package core

import "context"

// RealtimeQuote 是一条盘中实时行情，Intraday Snapshot（盘中快照）的输入。
// Latest 是取数时刻的最新价，不是收盘价。
type RealtimeQuote struct {
	TsCode    string
	TradeDate string // YYYYMMDD，取自行情时间戳
	Open      float64
	High      float64
	Low       float64
	Latest    float64
}

// RealtimeSource 是实时行情数据源（生产实现：腾讯财经 qt.gtimg.cn 的薄适配）。
// 与 QuoteSource 并列的窄接口，同属全仓库唯一测试接缝。
type RealtimeSource interface {
	// FetchRealtime 拉取指定标的当时的盘中行情。
	FetchRealtime(ctx context.Context, tsCodes []string) ([]RealtimeQuote, error)
}
