package core

import (
	"context"
	"math"
	"testing"
)

// closeTo 是 1e-6 容差的近似比较（手算十进制期望值与 float64 运算末位有差）。
func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// --- bps 口径：以快照为基准，(官方 − 快照) / 快照 × 10⁴ ---

func TestBpsOfSignAndBase(t *testing.T) {
	// issue #12 示例：快照四点均值 3.2280 vs 官方 3.2300
	// → (3.23 − 3.228) / 3.228 × 10⁴ ≈ +6.20 bps（正 = 官方高于快照）。
	if got := bpsOf(3.2300, 3.2280); !closeTo(got, 6.195787) {
		t.Fatalf("bpsOf = %v, want ≈ 6.1958", got)
	}
	// 反向：官方低于快照为负。
	if got := bpsOf(3.2280, 3.2300); !closeTo(got, -6.191950) {
		t.Fatalf("bpsOf = %v, want ≈ -6.1920", got)
	}
	// base 为 0 防御性返回 0。
	if got := bpsOf(1, 0); got != 0 {
		t.Fatalf("bpsOf(1, 0) = %v, want 0", got)
	}
}

// --- BuildSlippageDiffs ---

func TestBuildSlippageDiffs(t *testing.T) {
	// 手算期望：快照 O=3.20 H=3.25 L=3.18 Latest=3.22（均值 3.2125），
	// 官方 O=3.21 H=3.26 L=3.19 C=3.23（均值 3.2225）。
	// 各字段绝对差均 +0.01；开 bps = 0.01/3.20×10⁴ = 31.25，
	// 收 bps = 0.01/3.22×10⁴ ≈ 31.0559，均值差 bps = 0.01/3.2125×10⁴ ≈ 31.1284。
	snaps := map[string]IntradaySnapshot{
		"518880.SH": {TsCode: "518880.SH", TradeDate: "20260805", Open: 3.20, High: 3.25, Low: 3.18, Latest: 3.22},
	}
	bars := map[string]DailyBarAdj{
		"518880.SH": {TradeDate: "20260805", Open: 3.21, High: 3.26, Low: 3.19, Close: 3.23, AdjFactor: 1},
	}

	diffs := BuildSlippageDiffs([]string{"518880.SH"}, bars, snaps)
	if len(diffs) != 1 {
		t.Fatalf("diffs len = %d, want 1", len(diffs))
	}
	d := diffs[0]
	if !d.Available {
		t.Fatalf("数据齐全应 Available: %+v", d)
	}
	if !closeTo(d.Open.Abs, 0.01) || !closeTo(d.Open.Bps, 31.25) {
		t.Fatalf("open diff = %+v, want abs 0.01 / bps 31.25", d.Open)
	}
	if !closeTo(d.High.Abs, 0.01) || !closeTo(d.High.Bps, 30.769231) {
		t.Fatalf("high diff = %+v", d.High)
	}
	if !closeTo(d.Low.Abs, 0.01) || !closeTo(d.Low.Bps, 31.446541) {
		t.Fatalf("low diff = %+v", d.Low)
	}
	// 收 = 官方 close vs 快照 latest。
	if !closeTo(d.Close.Abs, 0.01) || !closeTo(d.Close.Bps, 31.055901) {
		t.Fatalf("close diff = %+v", d.Close)
	}
	if !closeTo(d.MeanBps, 31.128405) {
		t.Fatalf("MeanBps = %v, want ≈ 31.1284", d.MeanBps)
	}
}

func TestBuildSlippageDiffsMissing(t *testing.T) {
	// A 缺快照、B 缺当日官方日线：各自标注原因，不中断，也不影响齐全的 C。
	snaps := map[string]IntradaySnapshot{
		"B": {TsCode: "B", TradeDate: "20260805", Open: 1, High: 1, Low: 1, Latest: 1},
		"C": {TsCode: "C", TradeDate: "20260805", Open: 1, High: 1, Low: 1, Latest: 1},
	}
	bars := map[string]DailyBarAdj{
		"A": {TradeDate: "20260805", Open: 1, High: 1, Low: 1, Close: 1, AdjFactor: 1},
		"C": {TradeDate: "20260805", Open: 1, High: 1, Low: 1, Close: 1, AdjFactor: 1},
	}

	diffs := BuildSlippageDiffs([]string{"A", "B", "C"}, bars, snaps)
	if len(diffs) != 3 {
		t.Fatalf("diffs len = %d, want 3", len(diffs))
	}
	byCode := map[string]InstrumentDiff{}
	for _, d := range diffs {
		byCode[d.TsCode] = d
	}
	if byCode["A"].Available || byCode["A"].Message == "" {
		t.Fatalf("A 缺快照应 Available=false 且记原因: %+v", byCode["A"])
	}
	if byCode["B"].Available || byCode["B"].Message == "" {
		t.Fatalf("B 缺日线应 Available=false 且记原因: %+v", byCode["B"])
	}
	if !byCode["C"].Available {
		t.Fatalf("C 数据齐全应 Available: %+v", byCode["C"])
	}
}

// --- ComputeCloseSignal：当日第 20 点取官方收盘 ---

func TestComputeCloseSignalUsesOfficialClose(t *testing.T) {
	// 与 TestTodayPointUsesLatestTimesLatestAdjFactor 同构，但当日点来自库内官方日线：
	// 19 根历史四点同价 1、因子 2 → 后复权 M = 2，L = ln2；
	// 当日官方日线 O=H=L=1、C=3、因子 2 → M = (1+1+1+3)/4×2 = 3，L = ln3。
	// D = ln3 − ln2 = ln1.5，全部位移集中在最后一步 → ER = 1，score = ln1.5。
	// 若错用 open 或漏乘因子，期望值不成立。
	st := newFakeStore()
	hist := mkFlatHistory("518880.SH", 19, "20240119", 1, 2)
	hist = append(hist, DailyBarAdj{TradeDate: "20240120", Open: 1, High: 1, Low: 1, Close: 3, AdjFactor: 2})
	st.recentDaily["518880.SH"] = hist

	c := NewSignalComputer(&fakeRealtimeSource{}, st, 5, fixedToday("20240120"))
	report, err := c.ComputeCloseSignal(context.Background(), []string{"518880.SH"}, "20240120")
	if err != nil {
		t.Fatalf("ComputeCloseSignal err: %v", err)
	}
	card := report.Cards[0]
	if want := math.Log(1.5); !almostEqual(card.Score, want) {
		t.Fatalf("Score = %v, want ln(1.5) = %v", card.Score, want)
	}
	if card.Stale {
		t.Fatalf("当日官方日线齐时不应 stale: %+v", card)
	}
	if card.Rank != 1 {
		t.Fatalf("单标的名次应为 1, got %d", card.Rank)
	}
	// 收盘重算无实时行情依赖、无快照落库副作用。
	if len(st.snapshots) != 0 {
		t.Fatalf("ComputeCloseSignal 不应落库快照: %v", st.snapshots)
	}
}

func TestComputeCloseSignalMissingTodayBar(t *testing.T) {
	// 库内最新日线停在 T-1（当日官方日线未同步）：该标的跳过打分，不中断其余标的。
	st := newFakeStore()
	st.recentDaily["518880.SH"] = mkFlatHistory("518880.SH", 20, "20240119", 1, 1)
	hist := mkFlatHistory("510880.SH", 19, "20240119", 1, 2)
	hist = append(hist, DailyBarAdj{TradeDate: "20240120", Open: 1, High: 1, Low: 1, Close: 3, AdjFactor: 2})
	st.recentDaily["510880.SH"] = hist

	c := NewSignalComputer(&fakeRealtimeSource{}, st, 5, fixedToday("20240120"))
	report, err := c.ComputeCloseSignal(context.Background(), []string{"518880.SH", "510880.SH"}, "20240120")
	if err != nil {
		t.Fatalf("ComputeCloseSignal err: %v", err)
	}
	missing, ok := report.Cards[0], report.Cards[1]
	if !missing.Stale || missing.Message == "" {
		t.Fatalf("缺当日日线应 Stale 且记原因: %+v", missing)
	}
	if !math.IsNaN(missing.Score) || missing.Rank != 0 {
		t.Fatalf("缺当日日线不应打分/排名: %+v", missing)
	}
	if ok.Stale || math.IsNaN(ok.Score) || ok.Rank != 1 {
		t.Fatalf("数据齐全标的应正常打分: %+v", ok)
	}
}

func TestComputeCloseSignalBadTradeDate(t *testing.T) {
	c := NewSignalComputer(&fakeRealtimeSource{}, newFakeStore(), 5, nil)
	if _, err := c.ComputeCloseSignal(context.Background(), []string{"A"}, "2026-08-05"); err == nil {
		t.Fatal("非法 tradeDate 应报错")
	}
}
