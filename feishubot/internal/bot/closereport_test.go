package bot

import (
	"strings"
	"testing"
	"time"
)

func closeReportFixture() *CloseReportResp {
	return &CloseReportResp{
		TradeDate:   "20260805",
		TradingDay:  true,
		HasSnapshot: true,
		Sync: SyncResp{
			Total:   2,
			Success: 2,
			Results: []SyncResultItem{
				{TsCode: "510880.SH", Message: "已是最新"},
				{TsCode: "518880.SH", Upserted: 2, DailyUpserted: 1, AdjUpserted: 1, Message: "ok"},
			},
		},
		Diffs: []SlippageDiffItem{
			{
				TsCode: "518880.SH", Name: "华安易富黄金ETF", Available: true,
				// 快照 O=3.20 H=3.25 L=3.18 Latest=3.22（均值 3.2125），
				// 官方 O=3.21 H=3.26 L=3.19 C=3.23（均值 3.2225）：
				// 收差 = (3.23−3.22)/3.22×10⁴ ≈ +31.06bps；均值差 = 0.01/3.2125×10⁴ ≈ +31.13bps（快照为基准）。
				Open:    &DiffField{Abs: 0.01, Bps: 31.25},
				High:    &DiffField{Abs: 0.01, Bps: 30.769231},
				Low:     &DiffField{Abs: 0.01, Bps: 31.446541},
				Close:   &DiffField{Abs: 0.01, Bps: 31.055901},
				MeanBps: f64(31.128405),
			},
			{
				TsCode: "510880.SH", Name: "华泰柏瑞上证红利ETF", Available: false,
				Message: "无当日盘中快照（14:45 未取数或非交易日），无滑点差值",
			},
		},
		Cards: []SignalCardItem{
			{TsCode: "518880.SH", Name: "华安易富黄金ETF", Score: f64(-0.023456), YZVol: f64(0.15234), Quantile: f64(61.25), Weight: 1, Rank: 2},
			{TsCode: "510880.SH", Name: "华泰柏瑞上证红利ETF", Score: f64(0.123456), YZVol: f64(0.08123), Quantile: f64(45.5), Weight: 1, Rank: 1},
		},
	}
}

func TestRenderCloseReportFull(t *testing.T) {
	now := time.Date(2026, 8, 5, 18, 0, 0, 0, time.Local)
	md := RenderCloseReport(closeReportFixture(), now)

	for _, want := range []string{
		"收盘日报",
		"交易日：2026-08-05",
		// ① 同步结果摘要
		"合计 2 只，成功 2 只", "已是最新",
		// ② 滑点差值：逐字段带号绝对差 + bps，四点均值差 bps
		"滑点差值",
		"开 +0.0100（+31.25bps）", "高 +0.0100（+30.77bps）",
		"低 +0.0100（+31.45bps）", "收 +0.0100（+31.06bps）",
		"四点均值差 +31.13bps",
		// 无差值标的标注原因
		"无当日盘中快照",
		// ③ 收盘重算：与信号卡片同一渲染约定
		"收盘重算",
		"**1｜华泰柏瑞上证红利ETF（510880.SH）**",
		"score 0.1235 · σ_YZ 8.12% · q 45.50% · w(q) 1.00",
		"score -0.0235",
		"生成时间：2026-08-05 18:00:00",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("close report missing %q:\n%s", want, md)
		}
	}

	// 三段顺序：同步结果 → 滑点差值 → 收盘重算。
	iSync := strings.Index(md, "同步结果")
	iDiff := strings.Index(md, "滑点差值")
	iRe := strings.Index(md, "收盘重算")
	if !(iSync >= 0 && iSync < iDiff && iDiff < iRe) {
		t.Errorf("sections out of order:\n%s", md)
	}
}

func TestRenderCloseReportDegraded(t *testing.T) {
	// 非交易日：降级为纯同步摘要，无差值/重算段落，但仍渲染（每天照推）。
	resp := closeReportFixture()
	resp.TradingDay = false
	resp.HasSnapshot = false
	md := RenderCloseReport(resp, time.Now())

	for _, want := range []string{"收盘日报", "同步结果", "非交易日或无盘中快照"} {
		if !strings.Contains(md, want) {
			t.Errorf("degraded report missing %q:\n%s", want, md)
		}
	}
	for _, notWant := range []string{"**滑点差值**", "**收盘重算**", "四点均值差"} {
		if strings.Contains(md, notWant) {
			t.Errorf("degraded report should not contain %q:\n%s", notWant, md)
		}
	}

	// 有快照但当日官方日线未就位（tradingDay=false）同样降级。
	resp2 := closeReportFixture()
	resp2.TradingDay = false
	md2 := RenderCloseReport(resp2, time.Now())
	if strings.Contains(md2, "四点均值差") {
		t.Errorf("tradingDay=false should degrade:\n%s", md2)
	}
}

func TestRenderCloseReportNil(t *testing.T) {
	if !strings.Contains(RenderCloseReport(nil, time.Now()), "未返回结果") {
		t.Error("nil CloseReportResp should render 未返回结果")
	}
}
