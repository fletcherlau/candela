package core

import (
	"math"
	"strings"
	"testing"
)

// adviceFixture 手工构造的信号卡片（对齐 2026-08-05 实盘口径）：
// 510880 第一名（score 0.0195, w 0.80）；513100 第二名（0.0088，差距 0.0107 > 0.005 → 换仓）；
// 518880 得分 -0.01（安全阀）；159915 得分 0.0160（差距 0.0035 ≤ 0.005 → 差距缓冲不换）。
func adviceFixture() []SignalCard {
	return []SignalCard{
		{TsCode: "510880.SH", Score: 0.0195, YZVol: 0.08, Quantile: 55, Weight: 0.80, Rank: 1},
		{TsCode: "513100.SH", Score: 0.0088, YZVol: 0.21, Quantile: 82, Weight: 0.60, Rank: 2},
		{TsCode: "518880.SH", Score: -0.0100, YZVol: 0.15, Quantile: 61, Weight: 1.00, Rank: 3},
		{TsCode: "159915.SZ", Score: 0.0160, YZVol: 0.25, Quantile: 75, Weight: 0.90, Rank: 4},
	}
}

// findAdvice 按情形名取建议行；找不到则测试失败。
func findAdvice(t *testing.T, items []AdviceItem, scenario string) AdviceItem {
	t.Helper()
	for _, it := range items {
		if it.Scenario == scenario {
			return it
		}
	}
	t.Fatalf("缺少情形 %s 的建议行: %+v", scenario, items)
	return AdviceItem{}
}

func wantWeight(t *testing.T, it AdviceItem, want float64) {
	t.Helper()
	if it.TargetWeight == nil {
		t.Fatalf("情形 %s 目标仓位应为 %.2f，实际为 null", it.Scenario, want)
	}
	if !almostEqual(*it.TargetWeight, want) {
		t.Errorf("情形 %s 目标仓位 = %.4f，期望 %.2f", it.Scenario, *it.TargetWeight, want)
	}
}

// 现金情形：买入第一名，仓位 w(第一名)。
func TestComputeAdviceCash(t *testing.T) {
	items := ComputeAdvice(adviceFixture())
	cash := findAdvice(t, items, "现金")
	if cash.Action != "买入 510880.SH" {
		t.Errorf("现金情形 action = %q，期望 买入 510880.SH", cash.Action)
	}
	wantWeight(t, cash, 0.80)
}

// 持有第一名：不换仓；目标仓位 w(X)，偏离 ≥5 个百分点才微调。
func TestComputeAdviceHoldRank1(t *testing.T) {
	items := ComputeAdvice(adviceFixture())
	it := findAdvice(t, items, "510880.SH")
	if it.Action != "持有" {
		t.Errorf("持有第一名 action = %q，期望 持有", it.Action)
	}
	wantWeight(t, it, 0.80)
	if !strings.Contains(it.Note, "不换仓") || !strings.Contains(it.Note, "5 个百分点") {
		t.Errorf("持有第一名 note 应说明不换仓与 5%% 死区微调: %q", it.Note)
	}
}

// 安全阀：持仓得分 < 0 时无视差距换入第一名。
func TestComputeAdviceSafetyValve(t *testing.T) {
	items := ComputeAdvice(adviceFixture())
	it := findAdvice(t, items, "518880.SH")
	if it.Action != "换入 510880.SH" {
		t.Errorf("持仓得分 < 0 action = %q，期望 换入 510880.SH", it.Action)
	}
	wantWeight(t, it, 0.80)
	if !strings.Contains(it.Note, "安全阀") {
		t.Errorf("安全阀情形 note 应注明安全阀: %q", it.Note)
	}

	// 差距再小也不豁免安全阀：rank1 0.0001，持仓 -0.0001（差距 0.0002 < δ）仍换仓。
	cards := []SignalCard{
		{TsCode: "510880.SH", Score: 0.0001, Weight: 0.7, Rank: 1},
		{TsCode: "518880.SH", Score: -0.0001, Weight: 1.0, Rank: 2},
	}
	it = findAdvice(t, ComputeAdvice(cards), "518880.SH")
	if it.Action != "换入 510880.SH" || !strings.Contains(it.Note, "安全阀") {
		t.Errorf("安全阀应无视差距换仓: %+v", it)
	}
}

// 差距缓冲：得分差 ≤ 0.005 不换（含恰好等于），> 0.005 换。
func TestComputeAdviceGapBuffer(t *testing.T) {
	items := ComputeAdvice(adviceFixture())
	it := findAdvice(t, items, "159915.SZ") // 差距 0.0035
	if it.Action != "持有" {
		t.Errorf("差距 0.0035 ≤ δ action = %q，期望 持有", it.Action)
	}
	wantWeight(t, it, 0.90)
	if !strings.Contains(it.Note, "差距缓冲") {
		t.Errorf("差距缓冲情形 note 应注明差距缓冲: %q", it.Note)
	}

	// 恰好 δ：rank1 0.005，持仓 0.000 → 差距 0.005 ≤ δ，不换。
	cards := []SignalCard{
		{TsCode: "510880.SH", Score: 0.005, Weight: 0.8, Rank: 1},
		{TsCode: "159915.SZ", Score: 0.0, Weight: 0.6, Rank: 2},
	}
	it = findAdvice(t, ComputeAdvice(cards), "159915.SZ")
	if it.Action != "持有" {
		t.Errorf("差距恰好 δ=0.005 应不换仓: %+v", it)
	}

	// 刚好超过 δ：rank1 0.0051，持仓 0.000 → 差距 0.0051 > δ，换。
	cards[0].Score = 0.0051
	it = findAdvice(t, ComputeAdvice(cards), "159915.SZ")
	if it.Action != "换入 510880.SH" {
		t.Errorf("差距 0.0051 > δ 应换仓: %+v", it)
	}
	wantWeight(t, it, 0.8)
}

// 普通换仓：持仓得分 ≥ 0 且差距 > δ。
func TestComputeAdviceSwitch(t *testing.T) {
	items := ComputeAdvice(adviceFixture())
	it := findAdvice(t, items, "513100.SH") // 差距 0.0107 > δ
	if it.Action != "换入 510880.SH" {
		t.Errorf("差距 0.0107 > δ action = %q，期望 换入 510880.SH", it.Action)
	}
	wantWeight(t, it, 0.80)
	if strings.Contains(it.Note, "安全阀") || strings.Contains(it.Note, "差距缓冲") {
		t.Errorf("普通换仓不应注明安全阀/差距缓冲: %q", it.Note)
	}
}

// QDII 溢价提示：第一名是 513100.SH 时，买入/换入情形附加溢价确认；不涉及买入的情形不附加。
func TestComputeAdvicePremiumNote(t *testing.T) {
	cards := []SignalCard{
		{TsCode: "513100.SH", Score: 0.0200, Weight: 0.60, Rank: 1},
		{TsCode: "510880.SH", Score: 0.0080, Weight: 0.80, Rank: 2}, // 差距 0.012 > δ → 换入
		{TsCode: "159915.SZ", Score: 0.0190, Weight: 0.90, Rank: 3}, // 差距 0.001 ≤ δ → 不换
	}
	items := ComputeAdvice(cards)
	for _, scenario := range []string{"现金", "510880.SH"} {
		it := findAdvice(t, items, scenario)
		if !strings.Contains(it.Note, "溢价率 ≤1%") || !strings.Contains(it.Note, "顺延第二名") {
			t.Errorf("情形 %s 涉及买入 513100，应附溢价确认: %q", scenario, it.Note)
		}
	}
	for _, scenario := range []string{"513100.SH", "159915.SZ"} {
		it := findAdvice(t, items, scenario)
		if strings.Contains(it.Note, "溢价率") {
			t.Errorf("情形 %s 不涉及买入，不应附溢价确认: %q", scenario, it.Note)
		}
	}
	// 第一名不是 513100 时任何情形都没有溢价提示。
	for _, it := range ComputeAdvice(adviceFixture()) {
		if strings.Contains(it.Note, "溢价率") {
			t.Errorf("第一名非 513100，不应出现溢价提示: %q", it.Note)
		}
	}
}

// 数据不足：score 为 NaN 的标的不参与建议，持有时维持不动；第一名缺失时全部不下单。
func TestComputeAdviceNaN(t *testing.T) {
	cards := adviceFixture()
	cards[3].Score = math.NaN()
	cards[3].YZVol = math.NaN()
	cards[3].Quantile = math.NaN()
	cards[3].Weight = 1.0
	cards[3].Rank = 0
	items := ComputeAdvice(cards)
	it := findAdvice(t, items, "159915.SZ")
	if it.Action != "持有" {
		t.Errorf("持有数据不足标的 action = %q，期望 持有（维持不动）", it.Action)
	}
	if it.TargetWeight != nil {
		t.Errorf("维持不动情形目标仓位应为 null，实际 %.4f", *it.TargetWeight)
	}
	if !strings.Contains(it.Note, "数据不足") || !strings.Contains(it.Note, "维持当前持仓与仓位不动") {
		t.Errorf("数据不足情形 note 应说明维持不动: %q", it.Note)
	}
	// 其余情形不受影响。
	wantWeight(t, findAdvice(t, items, "现金"), 0.80)
	if findAdvice(t, items, "513100.SH").Action != "换入 510880.SH" {
		t.Error("其余情形不应受单个 NaN 标的的影响")
	}

	// 全部 NaN：第一名缺失，所有情形「数据不足，今日不下单」。
	allNaN := []SignalCard{
		{TsCode: "510880.SH", Score: math.NaN(), Weight: 1.0},
		{TsCode: "513100.SH", Score: math.NaN(), Weight: 1.0},
	}
	items = ComputeAdvice(allNaN)
	if len(items) != 3 {
		t.Fatalf("全 NaN 时应输出 现金+2 标的 共 3 行，实际 %d 行", len(items))
	}
	for _, it := range items {
		if it.Action != "不下单" || it.TargetWeight != nil || !strings.Contains(it.Note, "数据不足，今日不下单") {
			t.Errorf("第一名缺失时应建议不下单: %+v", it)
		}
	}
}

// 输出顺序固定：现金第一行，其余按名次升序（无法打分的垫底）。
func TestComputeAdviceOrdering(t *testing.T) {
	cards := adviceFixture()
	// 打乱输入顺序，输出仍应稳定。
	cards[0], cards[2] = cards[2], cards[0]
	items := ComputeAdvice(cards)
	want := []string{"现金", "510880.SH", "513100.SH", "518880.SH", "159915.SZ"}
	if len(items) != len(want) {
		t.Fatalf("建议行数 = %d，期望 %d", len(items), len(want))
	}
	for i, s := range want {
		if items[i].Scenario != s {
			t.Errorf("第 %d 行情形 = %s，期望 %s", i, items[i].Scenario, s)
		}
	}
}
