package bot

import (
	"strings"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

func signalFixture() *SignalResp {
	return &SignalResp{
		TradeDate:    "20260805",
		SnapshotDate: "20260805",
		TradingDay:   true,
		Cards: []SignalCardItem{
			{TsCode: "518880.SH", Name: "华安易富黄金ETF", Score: f64(-0.023456), YZVol: f64(0.15234), Quantile: f64(61.25), Weight: 1, Rank: 3},
			{TsCode: "510880.SH", Name: "华泰柏瑞上证红利ETF", Score: f64(0.123456), YZVol: f64(0.08123), Quantile: f64(45.5), Weight: 1, Rank: 1},
			{TsCode: "513100.SH", Name: "国泰纳斯达克100ETF(QDII)", Score: f64(0.056789), YZVol: f64(0.21567), Quantile: f64(82.4), Weight: 0.752, Rank: 2},
			{TsCode: "159915.SZ", Name: "易方达创业板ETF", Score: nil, YZVol: nil, Quantile: nil, Weight: 1, Rank: 0, Message: "历史不足 20 点，无法打分"},
		},
	}
}

func TestRenderSignalCard(t *testing.T) {
	now := time.Date(2026, 8, 5, 14, 45, 0, 0, time.Local)
	md := RenderSignalCard(signalFixture(), now)

	for _, want := range []string{
		"信号卡片",
		"数据时间：2026-08-05",
		"生成时间：2026-08-05 14:45:00",
		// 数值格式：score 4+ 位小数，σ_YZ / q 百分号，w(q)
		"score 0.1235", "σ_YZ 8.12%", "q 45.50%", "w(q) 1.00",
		"score -0.0235", "σ_YZ 15.23%",
		"w(q) 0.75",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("card missing %q:\n%s", want, md)
		}
	}

	// 按名次排序：rank 1 → 2 → 3，无法打分（rank 0）垫底。
	i1 := strings.Index(md, "华泰柏瑞上证红利ETF")
	i2 := strings.Index(md, "国泰纳斯达克100ETF")
	i3 := strings.Index(md, "华安易富黄金ETF")
	i0 := strings.Index(md, "易方达创业板ETF")
	if !(i1 >= 0 && i1 < i2 && i2 < i3 && i3 < i0) {
		t.Errorf("cards not sorted by rank (1→2→3→0):\n%s", md)
	}

	// 第一名高亮（粗体），其余名次不粗体。
	if !strings.Contains(md, "**1｜华泰柏瑞上证红利ETF（510880.SH）**") {
		t.Errorf("rank-1 instrument should be bold:\n%s", md)
	}
	if strings.Contains(md, "**2｜") || strings.Contains(md, "**3｜") {
		t.Errorf("non-rank-1 instruments should not be bold:\n%s", md)
	}

	// 无 stale 标的时不出现告警。
	if strings.Contains(md, "新鲜度告警") {
		t.Errorf("no stale card, should not warn:\n%s", md)
	}

	// 无状态：不含换仓结论。
	if strings.Contains(md, "换") {
		t.Errorf("signal card must be stateless:\n%s", md)
	}
}

func TestRenderSignalCardStaleWarning(t *testing.T) {
	resp := signalFixture()
	resp.Cards[0].Stale = true
	resp.Cards[2].Stale = true
	md := RenderSignalCard(resp, time.Now())

	warn := strings.Index(md, "新鲜度告警")
	if warn < 0 {
		t.Fatalf("stale cards should trigger warning:\n%s", md)
	}
	// 告警置顶：在第一名之前出现。
	if warn > strings.Index(md, "华泰柏瑞上证红利ETF") {
		t.Errorf("warning should be above instrument list:\n%s", md)
	}
	for _, code := range []string{"518880.SH", "513100.SH"} {
		if !strings.Contains(md[:strings.Index(md, "1｜")], code) {
			t.Errorf("warning should name stale instrument %s:\n%s", code, md)
		}
	}
}

func TestRenderSignalCardNA(t *testing.T) {
	md := RenderSignalCard(signalFixture(), time.Now())

	// 无法打分的标的：score / σ_YZ / q 渲染为 -，附原因。
	if !strings.Contains(md, "score - · σ_YZ - · q -") {
		t.Errorf("absent values should render as -:\n%s", md)
	}
	if !strings.Contains(md, "历史不足 20 点，无法打分") {
		t.Errorf("card message should be rendered:\n%s", md)
	}
}

func TestRenderSignalCardNil(t *testing.T) {
	if !strings.Contains(RenderSignalCard(nil, time.Now()), "未返回结果") {
		t.Error("nil SignalResp should render 未返回结果")
	}
}
