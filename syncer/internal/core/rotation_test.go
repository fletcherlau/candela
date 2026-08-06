package core

import (
	"context"
	"math"
	"testing"
	"time"
)

// --- 内存 fake（RealtimeSource 侧；Store 侧复用 sync_test.go 的 fakeStore） ---

type fakeRealtimeSource struct {
	quotes map[string]RealtimeQuote // key: tsCode；缺失表示停牌/无数据
}

func (f *fakeRealtimeSource) FetchRealtime(ctx context.Context, tsCodes []string) ([]RealtimeQuote, error) {
	var out []RealtimeQuote
	for _, code := range tsCodes {
		if q, ok := f.quotes[code]; ok {
			out = append(out, q)
		}
	}
	return out, nil
}

// --- 测试辅助 ---

// mkDates 生成 end 之前（含 end）的 n 个连续日历日期（升序，YYYYMMDD）。
// 假数据无需交易日历，连续日历日即可。
func mkDates(n int, end string) []string {
	t, err := time.Parse("20060102", end)
	if err != nil {
		panic(err)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = t.AddDate(0, 0, i-(n-1)).Format("20060102")
	}
	return out
}

// mkFlatHistory 构造 n 根四点同价的日线（O=H=L=C=price，因子 factor），日期升序止于 end。
func mkFlatHistory(tsCode string, n int, end string, price, factor float64) []DailyBarAdj {
	dates := mkDates(n, end)
	out := make([]DailyBarAdj, n)
	for i, d := range dates {
		out[i] = DailyBarAdj{TradeDate: d, Open: price, High: price, Low: price, Close: price, AdjFactor: factor}
	}
	return out
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// --- 纯函数：节流权重、分位、YZ ---

func TestThrottleWeightBoundaries(t *testing.T) {
	cases := []struct {
		q    float64
		want float64
	}{
		{70, 1.0},         // q ≤ 70 → 满仓
		{85, 0.7},         // 1 − 0.6×(85−70)/30 = 0.7
		{100, 0.4},        // 下限 40%
		{50, 1.0},         // 折线以下恒 1
		{math.NaN(), 1.0}, // q 缺失回退 1（同 rotation7.py）
	}
	for _, c := range cases {
		if got := throttleWeight(c.q); !almostEqual(got, c.want) {
			t.Fatalf("throttleWeight(%v) = %v, want %v", c.q, got, c.want)
		}
	}
}

func TestQuantileRankTiesHalved(t *testing.T) {
	// 窗口 [1,2,3,3,5]，当前值 3：小于 3 的有 2 个，等于 3 的有 2 个（含自身），
	// 并列取半 → (2 + 0.5×2)/5 × 100 = 60。
	got := quantileRank([]float64{1, 2, 3, 3, 5}, 3)
	if !almostEqual(got, 60) {
		t.Fatalf("quantileRank = %v, want 60", got)
	}
}

func TestYZVolConstantPrices(t *testing.T) {
	// 恒定价格：overnight/intraday/RS 全为 0 → σ = 0。
	bars := make([]DailyBarAdj, yzWindow+1)
	for i := range bars {
		bars[i] = DailyBarAdj{Open: 1, High: 1, Low: 1, Close: 1, AdjFactor: 1}
	}
	if got := yzVol(bars, yzWindow); got != 0 {
		t.Fatalf("yzVol(constant) = %v, want 0", got)
	}
}

func TestYZVolTinySeries(t *testing.T) {
	// 期望值由 rotation7.py yz_vol(win=2) 原样计算导出（numpy）：
	// k = 0.34/(1.34+3/1) ≈ 0.0783410，σ² ≈ 1.7895758e-4，σ = sqrt(σ²×240) ≈ 0.2072433823896252。
	// 首点仅供昨收（C=100），窗口为后两点。
	bars := []DailyBarAdj{
		{Open: 100, High: 100.5, Low: 99.5, Close: 100},
		{Open: 101, High: 102, Low: 100, Close: 101},
		{Open: 102, High: 103, Low: 101, Close: 102},
	}
	if got := yzVol(bars, 2); !almostEqual(got, 0.2072433823896252) {
		t.Fatalf("yzVol = %v, want 0.2072433823896252", got)
	}
}

// --- ComputeSignal 行为 ---

func TestMomentumMonotonicSeries(t *testing.T) {
	// 严格单调对数价格：L_j = 0.01×j（j=0..19）⇒ 路径全同向，ER = 1，
	// score = D = L_19 − L_0 = 0.19（手算期望值，非公式重实现）。
	// 19 根历史（j=0..18）+ 盘中快照（j=19，四点同价 e^0.19）。
	hist := mkFlatHistory("510880.SH", 19, "20240119", 0, 1)
	for i := range hist {
		p := math.Exp(0.01 * float64(i))
		hist[i].Open, hist[i].High, hist[i].Low, hist[i].Close = p, p, p, p
	}
	today := math.Exp(0.01 * 19)

	st := newFakeStore()
	st.recentDaily["510880.SH"] = hist
	rt := &fakeRealtimeSource{quotes: map[string]RealtimeQuote{
		"510880.SH": {TsCode: "510880.SH", TradeDate: "20240120", Open: today, High: today, Low: today, Latest: today},
	}}

	c := NewSignalComputer(rt, st, 5, fixedToday("20240120"))
	report, err := c.ComputeSignal(context.Background(), []string{"510880.SH"})
	if err != nil {
		t.Fatalf("ComputeSignal err: %v", err)
	}
	card := report.Cards[0]
	if !almostEqual(card.Score, 0.19) {
		t.Fatalf("Score = %v, want 0.19 (ER=1 × D=0.19)", card.Score)
	}
	if card.Stale {
		t.Fatalf("T-1 历史 + 当日快照不应 stale: %+v", card)
	}
	if card.Rank != 1 {
		t.Fatalf("单标的名次应为 1, got %d", card.Rank)
	}
}

func TestTodayPointUsesLatestTimesLatestAdjFactor(t *testing.T) {
	// 19 根历史四点同价 1、因子 2 → 后复权 M = 2，L = ln2，各步位移 0。
	// 快照 O=H=L=1、Latest=3，乘最新因子 2 → 当日 M = (1+1+1+3)/4×2 = 3，L = ln3。
	// D = ln3 − ln2 = ln1.5，全部位移集中在最后一步 → ER = 1，score = ln1.5。
	// 任一环节错（未用 Latest、未乘因子、历史未复权）期望值都不成立。
	st := newFakeStore()
	st.recentDaily["518880.SH"] = mkFlatHistory("518880.SH", 19, "20240119", 1, 2)
	rt := &fakeRealtimeSource{quotes: map[string]RealtimeQuote{
		"518880.SH": {TsCode: "518880.SH", TradeDate: "20240120", Open: 1, High: 1, Low: 1, Latest: 3},
	}}

	c := NewSignalComputer(rt, st, 5, fixedToday("20240120"))
	report, err := c.ComputeSignal(context.Background(), []string{"518880.SH"})
	if err != nil {
		t.Fatalf("ComputeSignal err: %v", err)
	}
	card := report.Cards[0]
	if want := math.Log(1.5); !almostEqual(card.Score, want) {
		t.Fatalf("Score = %v, want ln(1.5) = %v", card.Score, want)
	}
	// 落库快照：AdjMean = (O+H+L+Latest)/4 × 最新因子 = 1.5×2 = 3。
	snap, ok := st.snapshots["518880.SH|20240120"]
	if !ok {
		t.Fatalf("快照未落库: %v", st.snapshots)
	}
	if !almostEqual(snap.AdjMean, 3) {
		t.Fatalf("AdjMean = %v, want 3", snap.AdjMean)
	}
}

func TestQuantileWithInjectedSmallWindow(t *testing.T) {
	// 注入小分位窗口 3：22 根历史 + 当日快照共 23 点 → 可算 3 个 σ_YZ（全为 0）。
	// 三个值全相等，并列取半 → q = (0 + 0.5×3)/3 × 100 = 50。
	st := newFakeStore()
	st.recentDaily["510880.SH"] = mkFlatHistory("510880.SH", 22, "20240119", 1, 1)
	rt := &fakeRealtimeSource{quotes: map[string]RealtimeQuote{
		"510880.SH": {TsCode: "510880.SH", TradeDate: "20240120", Open: 1, High: 1, Low: 1, Latest: 1},
	}}

	c := NewSignalComputer(rt, st, 3, fixedToday("20240120"))
	report, err := c.ComputeSignal(context.Background(), []string{"510880.SH"})
	if err != nil {
		t.Fatalf("ComputeSignal err: %v", err)
	}
	card := report.Cards[0]
	if card.YZVol != 0 {
		t.Fatalf("恒定价格 σ_YZ 应为 0, got %v", card.YZVol)
	}
	if !almostEqual(card.Quantile, 50) {
		t.Fatalf("Quantile = %v, want 50", card.Quantile)
	}
	if !almostEqual(card.Weight, 1.0) {
		t.Fatalf("q=50 ≤ 70 → Weight 应为 1, got %v", card.Weight)
	}
}

func TestFreshnessGuard(t *testing.T) {
	// A：库内最新日线 T-1 + 当日快照 → 不 stale；
	// B：库内最新日线滞后 19 个日历日（> 3）→ stale；
	// C：快照交易日非当日 → stale；
	// D：库内最新日线 gap 恰好 4（如周二数据停在周五的 T-2 情形）→ stale。
	st := newFakeStore()
	st.recentDaily["A"] = mkFlatHistory("A", 19, "20240119", 1, 1)
	st.recentDaily["B"] = mkFlatHistory("B", 19, "20240101", 1, 1)
	st.recentDaily["C"] = mkFlatHistory("C", 19, "20240118", 1, 1)
	st.recentDaily["D"] = mkFlatHistory("D", 19, "20240116", 1, 1)
	rt := &fakeRealtimeSource{quotes: map[string]RealtimeQuote{
		"A": {TsCode: "A", TradeDate: "20240120", Open: 1, High: 1, Low: 1, Latest: 1},
		"B": {TsCode: "B", TradeDate: "20240120", Open: 1, High: 1, Low: 1, Latest: 1},
		"C": {TsCode: "C", TradeDate: "20240119", Open: 1, High: 1, Low: 1, Latest: 1},
		"D": {TsCode: "D", TradeDate: "20240120", Open: 1, High: 1, Low: 1, Latest: 1},
	}}

	c := NewSignalComputer(rt, st, 5, fixedToday("20240120"))
	report, err := c.ComputeSignal(context.Background(), []string{"A", "B", "C", "D"})
	if err != nil {
		t.Fatalf("ComputeSignal err: %v", err)
	}
	stale := map[string]bool{}
	for _, card := range report.Cards {
		stale[card.TsCode] = card.Stale
	}
	if stale["A"] {
		t.Fatalf("A (T-1) 不应 stale")
	}
	if !stale["B"] {
		t.Fatalf("B (滞后 19 天) 应 stale")
	}
	if !stale["C"] {
		t.Fatalf("C (快照非当日) 应 stale")
	}
	if !stale["D"] {
		t.Fatalf("D (gap=4) 应 stale")
	}
}

func TestRankAcrossInstruments(t *testing.T) {
	// UP：L_j = +0.01j → score = +0.19；DOWN：L_j = −0.01j → score = −0.19。
	// 名次按 score 降序：UP 第 1，DOWN 第 2。
	st := newFakeStore()
	up := mkFlatHistory("UP", 19, "20240119", 0, 1)
	down := mkFlatHistory("DOWN", 19, "20240119", 0, 1)
	for i := range up {
		pu, pd := math.Exp(0.01*float64(i)), math.Exp(-0.01*float64(i))
		up[i].Open, up[i].High, up[i].Low, up[i].Close = pu, pu, pu, pu
		down[i].Open, down[i].High, down[i].Low, down[i].Close = pd, pd, pd, pd
	}
	st.recentDaily["UP"] = up
	st.recentDaily["DOWN"] = down
	upT, downT := math.Exp(0.01*19), math.Exp(-0.01*19)
	rt := &fakeRealtimeSource{quotes: map[string]RealtimeQuote{
		"UP":   {TsCode: "UP", TradeDate: "20240120", Open: upT, High: upT, Low: upT, Latest: upT},
		"DOWN": {TsCode: "DOWN", TradeDate: "20240120", Open: downT, High: downT, Low: downT, Latest: downT},
	}}

	c := NewSignalComputer(rt, st, 5, fixedToday("20240120"))
	report, err := c.ComputeSignal(context.Background(), []string{"UP", "DOWN"})
	if err != nil {
		t.Fatalf("ComputeSignal err: %v", err)
	}
	rank := map[string]int{}
	score := map[string]float64{}
	for _, card := range report.Cards {
		rank[card.TsCode] = card.Rank
		score[card.TsCode] = card.Score
	}
	if !almostEqual(score["UP"], 0.19) || !almostEqual(score["DOWN"], -0.19) {
		t.Fatalf("score 不符: UP=%v DOWN=%v", score["UP"], score["DOWN"])
	}
	if rank["UP"] != 1 || rank["DOWN"] != 2 {
		t.Fatalf("名次不符: UP=%d DOWN=%d", rank["UP"], rank["DOWN"])
	}
}

func TestSnapshotUpsertIdempotent(t *testing.T) {
	st := newFakeStore()
	st.recentDaily["510880.SH"] = mkFlatHistory("510880.SH", 19, "20240119", 1, 1)
	rt := &fakeRealtimeSource{quotes: map[string]RealtimeQuote{
		"510880.SH": {TsCode: "510880.SH", TradeDate: "20240120", Open: 1, High: 1, Low: 1, Latest: 1},
	}}

	c := NewSignalComputer(rt, st, 5, fixedToday("20240120"))
	if _, err := c.ComputeSignal(context.Background(), []string{"510880.SH"}); err != nil {
		t.Fatalf("first run err: %v", err)
	}
	// 第二次 Latest 变了（同一主键重算），应覆盖而非新增。
	q := rt.quotes["510880.SH"]
	q.Latest = 2
	rt.quotes["510880.SH"] = q
	if _, err := c.ComputeSignal(context.Background(), []string{"510880.SH"}); err != nil {
		t.Fatalf("second run err: %v", err)
	}

	if len(st.snapshots) != 1 {
		t.Fatalf("重复执行产生重复行: %d 条", len(st.snapshots))
	}
	if got := st.snapshots["510880.SH|20240120"].Latest; got != 2 {
		t.Fatalf("同主键应覆盖为新值, Latest = %v, want 2", got)
	}
}

// --- ComputeQuerySignal：persist 开关与非交易日收盘回退 ---

func TestComputeQuerySignalPersistFlag(t *testing.T) {
	// 交易日实时口径：persist=false 不落快照（signal 聊天命令），persist=true 落快照（cron 默认）。
	st := newFakeStore()
	st.recentDaily["510880.SH"] = mkFlatHistory("510880.SH", 19, "20240119", 1, 1)
	rt := &fakeRealtimeSource{quotes: map[string]RealtimeQuote{
		"510880.SH": {TsCode: "510880.SH", TradeDate: "20240120", Open: 1, High: 1, Low: 1, Latest: 1},
	}}
	c := NewSignalComputer(rt, st, 5, fixedToday("20240120"))

	report, err := c.ComputeQuerySignal(context.Background(), []string{"510880.SH"}, false)
	if err != nil {
		t.Fatalf("ComputeQuerySignal err: %v", err)
	}
	if report.Basis != BasisRealtime {
		t.Fatalf("交易日 Basis = %q, want %q", report.Basis, BasisRealtime)
	}
	if len(st.snapshots) != 0 {
		t.Fatalf("persist=false 不应落库快照: %v", st.snapshots)
	}

	if _, err := c.ComputeQuerySignal(context.Background(), []string{"510880.SH"}, true); err != nil {
		t.Fatalf("ComputeQuerySignal(persist=true) err: %v", err)
	}
	if len(st.snapshots) != 1 {
		t.Fatalf("persist=true 应落库快照: %v", st.snapshots)
	}
}

func TestComputeQuerySignalNonTradingDayFallsBackToClose(t *testing.T) {
	// 非交易日：今天 20240121，gtimg 行情停在最近交易日 20240120。
	// 20 根严格单调日线（L_j = 0.01j）止于 20240120 → 官方收盘口径 score = 0.19；
	// 盘中快照 Latest 故意离谱（100），若错用实时口径得分必不同。
	st := newFakeStore()
	hist := mkFlatHistory("510880.SH", 20, "20240120", 0, 1)
	for i := range hist {
		p := math.Exp(0.01 * float64(i))
		hist[i].Open, hist[i].High, hist[i].Low, hist[i].Close = p, p, p, p
	}
	st.recentDaily["510880.SH"] = hist
	rt := &fakeRealtimeSource{quotes: map[string]RealtimeQuote{
		"510880.SH": {TsCode: "510880.SH", TradeDate: "20240120", Open: 100, High: 100, Low: 100, Latest: 100},
	}}
	c := NewSignalComputer(rt, st, 5, fixedToday("20240121"))

	report, err := c.ComputeQuerySignal(context.Background(), []string{"510880.SH"}, false)
	if err != nil {
		t.Fatalf("ComputeQuerySignal err: %v", err)
	}
	if report.Basis != BasisClose {
		t.Fatalf("非交易日 Basis = %q, want %q", report.Basis, BasisClose)
	}
	// 日期语义不变：TradeDate=今天，SnapshotDate=行情最近交易日。
	if report.TradeDate != "20240121" || report.SnapshotDate != "20240120" {
		t.Fatalf("日期语义异常: TradeDate=%q SnapshotDate=%q", report.TradeDate, report.SnapshotDate)
	}
	card := report.Cards[0]
	if !almostEqual(card.Score, 0.19) {
		t.Fatalf("收盘口径 Score = %v, want 0.19（官方日线重算）", card.Score)
	}
	if card.Stale {
		t.Fatalf("最近交易日官方日线齐时不应 stale: %+v", card)
	}
	if len(st.snapshots) != 0 {
		t.Fatalf("persist=false 不应落库快照: %v", st.snapshots)
	}
}

func TestComputeQuerySignalNoQuotesKeepsRealtimeBasis(t *testing.T) {
	// 全部标的无行情（SnapshotDate 为空）：无法判定最近交易日，不回退，Basis 保持 realtime。
	st := newFakeStore()
	st.recentDaily["510880.SH"] = mkFlatHistory("510880.SH", 19, "20240119", 1, 1)
	c := NewSignalComputer(&fakeRealtimeSource{quotes: map[string]RealtimeQuote{}}, st, 5, fixedToday("20240121"))

	report, err := c.ComputeQuerySignal(context.Background(), []string{"510880.SH"}, false)
	if err != nil {
		t.Fatalf("ComputeQuerySignal err: %v", err)
	}
	if report.Basis != BasisRealtime || report.SnapshotDate != "" {
		t.Fatalf("无行情应保持 realtime 口径且 SnapshotDate 为空: %+v", report)
	}
}
