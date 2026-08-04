package main

import (
	"context"

	"syncer/internal/core"

	tushare "github.com/fletcherlau/go-tushare"
	"github.com/fletcherlau/go-tushare/fund/market"
)

// fundDailySource 是 go-tushare 客户端到 core.QuoteSource 的薄适配。
// 限频由 go-tushare 客户端内置（WithMinInterval），不在本层。
type fundDailySource struct {
	client *tushare.Client
}

func (s *fundDailySource) FetchDaily(ctx context.Context, tsCode, startDate, endDate string) ([]core.Bar, error) {
	items, err := market.FundDaily(s.client, &market.FundDailyParams{
		TSCode:    tsCode,
		StartDate: startDate,
		EndDate:   endDate,
	}, tushare.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	bars := make([]core.Bar, 0, len(items))
	for _, it := range items {
		bars = append(bars, core.Bar{
			TsCode:    it.TSCode,
			TradeDate: it.TradeDate,
			Open:      it.Open,
			High:      it.High,
			Low:       it.Low,
			Close:     it.Close,
			PreClose:  it.PreClose,
			ChangeAmt: it.Change,
			PctChg:    it.PctChg,
			Vol:       it.Vol,
			Amount:    it.Amount,
		})
	}
	return bars, nil
}

func (s *fundDailySource) FetchAdj(ctx context.Context, tsCode, startDate, endDate string) ([]core.AdjFactor, error) {
	items, err := market.FundAdj(s.client, &market.FundAdjParams{
		TSCode:    tsCode,
		StartDate: startDate,
		EndDate:   endDate,
	}, tushare.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	factors := make([]core.AdjFactor, 0, len(items))
	for _, it := range items {
		factors = append(factors, core.AdjFactor{
			TsCode:    it.TSCode,
			TradeDate: it.TradeDate,
			AdjFactor: it.AdjFactor,
		})
	}
	return factors, nil
}
