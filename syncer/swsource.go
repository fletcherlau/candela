package main

import (
	"context"
	"fmt"

	"syncer/internal/core"

	tushare "github.com/fletcherlau/go-tushare"
)

// swSource 是 go-tushare 通用 Query 到 core.SWSource 的薄适配。
// go-tushare 尚未封装申万接口，这里直接用客户端的通用 Query 调用：
// index_classify（行业字典）、index_member_all（成分归属）、sw_daily（指数日线）。
// 限频由 go-tushare 客户端内置（WithMinInterval），不在本层。
type swSource struct {
	client *tushare.Client
}

type swDictionaryItem struct {
	IndexCode    string `json:"index_code"`
	IndustryName string `json:"industry_name"`
	Level        string `json:"level"`
	IndustryCode string `json:"industry_code"`
	ParentCode   string `json:"parent_code"`
	IsPub        string `json:"is_pub"`
	Src          string `json:"src"`
}

func (s *swSource) FetchSWDictionary(ctx context.Context, src string) ([]core.SWIndustry, error) {
	resp, err := s.client.Query("index_classify", map[string]interface{}{"src": src}, "", tushare.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("index_classify 返回错误: code=%d msg=%s", resp.Code, resp.Msg)
	}

	var items []swDictionaryItem
	if err := resp.ToStruct(&items); err != nil {
		return nil, fmt.Errorf("解析 index_classify 响应失败: %w", err)
	}

	out := make([]core.SWIndustry, 0, len(items))
	for _, it := range items {
		out = append(out, core.SWIndustry{
			IndexCode:    it.IndexCode,
			IndustryName: it.IndustryName,
			Level:        it.Level,
			IndustryCode: it.IndustryCode,
			ParentCode:   it.ParentCode,
			IsPub:        it.IsPub,
			Src:          it.Src,
		})
	}
	return out, nil
}

type swMembershipItem struct {
	L1Code  string `json:"l1_code"`
	L1Name  string `json:"l1_name"`
	L2Code  string `json:"l2_code"`
	L2Name  string `json:"l2_name"`
	L3Code  string `json:"l3_code"`
	L3Name  string `json:"l3_name"`
	TSCode  string `json:"ts_code"`
	Name    string `json:"name"`
	InDate  string `json:"in_date"`
	OutDate string `json:"out_date"`
	IsNew   string `json:"is_new"`
}

func (s *swSource) FetchSWMembership(ctx context.Context, l1Code, isNew string) ([]core.SWMember, error) {
	resp, err := s.client.Query("index_member_all", map[string]interface{}{
		"l1_code": l1Code,
		"is_new":  isNew,
	}, "", tushare.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("index_member_all 返回错误: code=%d msg=%s", resp.Code, resp.Msg)
	}

	var items []swMembershipItem
	if err := resp.ToStruct(&items); err != nil {
		return nil, fmt.Errorf("解析 index_member_all 响应失败: %w", err)
	}

	out := make([]core.SWMember, 0, len(items))
	for _, it := range items {
		out = append(out, core.SWMember{
			TsCode:  it.TSCode,
			Name:    it.Name,
			L1Code:  it.L1Code,
			L1Name:  it.L1Name,
			L2Code:  it.L2Code,
			L2Name:  it.L2Name,
			L3Code:  it.L3Code,
			L3Name:  it.L3Name,
			InDate:  it.InDate,
			OutDate: it.OutDate,
			IsNew:   it.IsNew,
		})
	}
	return out, nil
}

type swDailyItem struct {
	TSCode    string  `json:"ts_code"`
	TradeDate string  `json:"trade_date"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Change    float64 `json:"change"`
	PctChange float64 `json:"pct_change"`
	Vol       float64 `json:"vol"`
	Amount    float64 `json:"amount"`
}

func (s *swSource) FetchSWIndexDaily(ctx context.Context, tsCode, startDate, endDate string) ([]core.SWIndexBar, error) {
	resp, err := s.client.Query("sw_daily", map[string]interface{}{
		"ts_code":    tsCode,
		"start_date": startDate,
		"end_date":   endDate,
	}, "", tushare.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("sw_daily 返回错误: code=%d msg=%s", resp.Code, resp.Msg)
	}

	var items []swDailyItem
	if err := resp.ToStruct(&items); err != nil {
		return nil, fmt.Errorf("解析 sw_daily 响应失败: %w", err)
	}

	out := make([]core.SWIndexBar, 0, len(items))
	for _, it := range items {
		out = append(out, core.SWIndexBar{
			TsCode:    it.TSCode,
			TradeDate: it.TradeDate,
			Open:      it.Open,
			High:      it.High,
			Low:       it.Low,
			Close:     it.Close,
			ChangeAmt: it.Change,
			PctChange: it.PctChange,
			Vol:       it.Vol,
			Amount:    it.Amount,
		})
	}
	return out, nil
}
