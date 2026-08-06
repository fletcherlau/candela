package core

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// 窗口与节流参数：一次定死，不做滚动调参（运行手册 §8）。
const (
	momentumWindow   = 20  // ER 动量点数（含当日盘中点）
	yzWindow         = 20  // YZ 波动率窗口
	yzAnnualFactor   = 240 // YZ 年化系数
	quantileDefault  = 1200
	staleCalendarDay = 3 // 新鲜度守卫容忍的日历天数：正常周一 T-1=周五 gap 3 不误报；
	// gap ≥ 4 即告警（能抓住周二数据停周五的 T-2 情形，见 issue #10）。
	// 代价：≥3 天长假后首个交易日会误报一次——偏安全方向，可人工忽略。
)

// 信号口径（Basis）：realtime = 当日第 20 点取盘中实时快照（Latest 作收盘）；
// close = 非交易日回退，当日点取最近交易日的官方收盘日线（ComputeCloseSignal）。
const (
	BasisRealtime = "realtime"
	BasisClose    = "close"
)

// DailyBarAdj 是读取侧的日线视图：Raw Daily Bar 的 OHLC 加上对齐后的 Adjustment Factor。
// 因子取 trade_date <= 当日的最近一条（两表日期可能不对齐，见 CONTEXT.md）。
type DailyBarAdj struct {
	TradeDate string // YYYYMMDD
	Open      float64
	High      float64
	Low       float64
	Close     float64
	AdjFactor float64
}

// IntradaySnapshot 是盘中快照（Intraday Snapshot）的持久化行：
// 14:45 实时 OHLC + 最新价；AdjMean = 后复权四点均值 (O+H+L+Latest)/4 × 最新复权因子。
type IntradaySnapshot struct {
	TsCode    string
	TradeDate string // YYYYMMDD
	Open      float64
	High      float64
	Low       float64
	Latest    float64
	AdjMean   float64
}

// SignalCard 是单个标的的盘中信号卡片。Signal Card 无状态（CONTEXT.md），每次全量重算。
type SignalCard struct {
	TsCode string `json:"tsCode"`
	// Score 是 ER 加权动量得分（正=上涨动能，负=下跌）；历史不足为 NaN。
	Score float64 `json:"score"`
	// YZVol 是 Yang-Zhang 年化波动率（窗口 20，×240）；历史不足为 NaN。
	YZVol float64 `json:"yzVol"`
	// Quantile 是 σ_YZ 在过去分位窗口内的分位（并列取半），0~100；历史不足为 NaN。
	Quantile float64 `json:"quantile"`
	// Weight 是 70/40 节流权重 w(q)；q 缺失时回退 1（与 rotation7.py 一致）。
	Weight float64 `json:"weight"`
	// Rank 是 Score 跨标的名次，1 最高；无法打分为 0。
	Rank int `json:"rank"`
	// Stale 是新鲜度守卫结果，见 ComputeSignal 注释。
	Stale   bool   `json:"stale"`
	Message string `json:"message,omitempty"`
}

// SignalReport 是一次盘中信号计算的整体结果。
type SignalReport struct {
	TradeDate string `json:"tradeDate"` // 今天，YYYYMMDD
	// SnapshotDate 是盘中快照交易日（取自行情时间戳）；与 TradeDate 不一致即非交易日。无行情时为空。
	SnapshotDate string `json:"snapshotDate"`
	// Basis 标定卡片口径：BasisRealtime（盘中实时）或 BasisClose（非交易日官方收盘回退）。
	Basis string       `json:"basis"`
	Cards []SignalCard `json:"cards"`
}

// SignalComputer 计算轮动信号并落库盘中快照。
// 只依赖 RealtimeSource 与 Store 两个窄接口，不感知 HTTP/MySQL/gtimg。
type SignalComputer struct {
	realtime RealtimeSource
	store    Store

	quantileWindow int // σ_YZ 分位窗口（交易日），注入以便测试用小窗口

	// today 返回今天（YYYYMMDD），注入以便测试。
	today func() string
}

// NewSignalComputer 构造信号计算核心。quantileWindow <= 0 时用 1200；today 为 nil 时用本地当天。
func NewSignalComputer(realtime RealtimeSource, store Store, quantileWindow int, today func() string) *SignalComputer {
	if quantileWindow <= 0 {
		quantileWindow = quantileDefault
	}
	if today == nil {
		today = func() string { return time.Now().Format("20060102") }
	}
	return &SignalComputer{realtime: realtime, store: store, quantileWindow: quantileWindow, today: today}
}

// ComputeSignal 计算各标的的信号卡片并按主键 upsert 盘中快照（幂等，cron 14:45 口径）。
// 单个标的数据缺失/历史不足不中断其余标的，信息记入该卡片的 Message。
//
// 新鲜度守卫（启发式，无交易日历，见 issue #10 约定）：
// stale = 快照交易日 ≠ 今天，或 今天 − 库内最新日线交易日 > 4 个日历日。
// 4 天覆盖周末与短假；长假会偏向误报警告（宁可多报）。
func (c *SignalComputer) ComputeSignal(ctx context.Context, tsCodes []string) (SignalReport, error) {
	return c.computeSignal(ctx, tsCodes, true)
}

// computeSignal 是 ComputeSignal 的实现体；persist=false 时只读计算、不落盘中快照
// （signal 聊天命令等交互查询用，不污染 intraday_snapshot）。
func (c *SignalComputer) computeSignal(ctx context.Context, tsCodes []string, persist bool) (SignalReport, error) {
	today := c.today()
	if _, err := time.Parse("20060102", today); err != nil {
		return SignalReport{}, fmt.Errorf("today 注入值格式非法: %q", today)
	}
	report := SignalReport{TradeDate: today, Basis: BasisRealtime}
	if len(tsCodes) == 0 {
		return report, nil
	}

	quotes, err := c.realtime.FetchRealtime(ctx, tsCodes)
	if err != nil {
		return SignalReport{}, fmt.Errorf("拉取盘中行情失败: %v", err)
	}
	byCode := make(map[string]RealtimeQuote, len(quotes))
	for _, q := range quotes {
		byCode[q.TsCode] = q
	}
	// 快照交易日取自行情时间戳；全部标的同日（个别停牌标的以各自卡片 Stale 体现）。
	if len(quotes) > 0 {
		report.SnapshotDate = quotes[0].TradeDate
	}

	var snaps []IntradaySnapshot
	for _, code := range tsCodes {
		card := SignalCard{
			TsCode:   code,
			Score:    math.NaN(),
			YZVol:    math.NaN(),
			Quantile: math.NaN(),
			Weight:   1.0,
		}
		report.Cards = append(report.Cards, card)
		cur := &report.Cards[len(report.Cards)-1]

		q, ok := byCode[code]
		if !ok {
			cur.Message = "无盘中行情（停牌或数据源缺失），当日不参与打分"
			cur.Stale = true
			continue
		}
		cur.Stale = q.TradeDate != today

		hist, err := c.store.RecentDaily(ctx, code, c.quantileWindow+yzWindow)
		if err != nil {
			cur.Message = fmt.Sprintf("读取日线历史失败: %v", err)
			cur.Stale = true
			continue
		}

		// 最新复权因子：已按日期对齐（ffill），取最后一根日线的因子。
		latestFactor := 1.0
		if len(hist) > 0 {
			latestFactor = hist[len(hist)-1].AdjFactor
			gap, err := calendarDays(hist[len(hist)-1].TradeDate, today)
			if err != nil || gap > staleCalendarDay {
				cur.Stale = true
			}
		} else {
			cur.Stale = true
		}

		// 盘中快照落库：原始 OHLC + 最新价 + 后复权四点均值。
		snaps = append(snaps, IntradaySnapshot{
			TsCode:    code,
			TradeDate: q.TradeDate,
			Open:      q.Open,
			High:      q.High,
			Low:       q.Low,
			Latest:    q.Latest,
			AdjMean:   (q.Open + q.High + q.Low + q.Latest) / 4 * latestFactor,
		})

		// 后复权序列：t-1.. 来自库内日线（排除 >= 快照日的行，当日点以快照为准，
		// 避免日线已同步到今天时重复计数），t 来自盘中快照（Latest 作收盘，乘最新因子）。
		series := make([]DailyBarAdj, 0, len(hist)+1)
		for _, b := range hist {
			if b.TradeDate >= q.TradeDate {
				continue
			}
			series = append(series, b)
		}
		series = append(series, DailyBarAdj{
			TradeDate: q.TradeDate,
			Open:      q.Open * latestFactor,
			High:      q.High * latestFactor,
			Low:       q.Low * latestFactor,
			Close:     q.Latest * latestFactor,
			AdjFactor: 1,
		})
		// 库内日线未后复权，这里统一乘因子（快照行上面已乘，AdjFactor 置 1 防二次乘）。
		for i := range series[:len(series)-1] {
			series[i].Open *= series[i].AdjFactor
			series[i].High *= series[i].AdjFactor
			series[i].Low *= series[i].AdjFactor
			series[i].Close *= series[i].AdjFactor
		}

		c.scoreFromSeries(cur, series)
	}

	rankCards(report.Cards)

	if persist && len(snaps) > 0 {
		if _, err := c.store.UpsertIntradaySnapshots(ctx, snaps); err != nil {
			return SignalReport{}, fmt.Errorf("写入盘中快照失败: %v", err)
		}
	}
	return report, nil
}

// ComputeQuerySignal 是交互式查询（signal 聊天命令）与 cron 推送共用的信号入口：
// persist 控制快照落库（命令恒为 false，cron 恒为 true）。
// 先按盘中实时口径计算；快照交易日 ≠ 今天（非交易日）时整体回退为
// ComputeCloseSignal(SnapshotDate)——最近交易日的官方收盘日线作当日点，Basis 标为 close。
// 日期语义不变：TradeDate 恒为今天，SnapshotDate 恒为行情最近交易日。
func (c *SignalComputer) ComputeQuerySignal(ctx context.Context, tsCodes []string, persist bool) (SignalReport, error) {
	report, err := c.computeSignal(ctx, tsCodes, persist)
	if err != nil {
		return SignalReport{}, err
	}
	if report.SnapshotDate == "" || report.SnapshotDate == report.TradeDate {
		return report, nil
	}
	closeReport, err := c.ComputeCloseSignal(ctx, tsCodes, report.SnapshotDate)
	if err != nil {
		return SignalReport{}, err
	}
	// 保留实时报告的日期语义（TradeDate=今天，SnapshotDate=最近交易日）。
	closeReport.TradeDate = report.TradeDate
	closeReport.SnapshotDate = report.SnapshotDate
	return closeReport, nil
}

// ComputeCloseSignal 用官方收盘日线重算信号（Close Report 用，issue #12）：
// 与 ComputeSignal 共用同一套打分逻辑（scoreFromSeries），唯一差别是当日第 20 点
// 取库内官方日线（Raw Daily Bar 的 close），而非盘中快照的 Latest。
// 无实时行情依赖，也无快照落库副作用。
// tradeDate（YYYYMMDD）当日官方日线缺失的标的记 Message 并跳过打分（Stale=true），不中断其余标的。
func (c *SignalComputer) ComputeCloseSignal(ctx context.Context, tsCodes []string, tradeDate string) (SignalReport, error) {
	if _, err := time.Parse("20060102", tradeDate); err != nil {
		return SignalReport{}, fmt.Errorf("tradeDate 格式非法: %q（期望 YYYYMMDD）", tradeDate)
	}
	report := SignalReport{TradeDate: tradeDate, Basis: BasisClose}
	for _, code := range tsCodes {
		card := SignalCard{
			TsCode:   code,
			Score:    math.NaN(),
			YZVol:    math.NaN(),
			Quantile: math.NaN(),
			Weight:   1.0,
		}
		report.Cards = append(report.Cards, card)
		cur := &report.Cards[len(report.Cards)-1]

		hist, err := c.store.RecentDaily(ctx, code, c.quantileWindow+yzWindow)
		if err != nil {
			cur.Message = fmt.Sprintf("读取日线历史失败: %v", err)
			cur.Stale = true
			continue
		}

		// 后复权序列：每根日线乘各自对齐因子（当日官方收盘点也不例外）；
		// 晚于 tradeDate 的行防御性剔除。
		series := make([]DailyBarAdj, 0, len(hist))
		for _, b := range hist {
			if b.TradeDate > tradeDate {
				continue
			}
			b.Open *= b.AdjFactor
			b.High *= b.AdjFactor
			b.Low *= b.AdjFactor
			b.Close *= b.AdjFactor
			series = append(series, b)
		}
		if len(series) == 0 || series[len(series)-1].TradeDate != tradeDate {
			cur.Message = "当日官方日线缺失（未同步或停牌），无法收盘重算"
			cur.Stale = true
			continue
		}
		c.scoreFromSeries(cur, series)
	}
	rankCards(report.Cards)
	return report, nil
}

// scoreFromSeries 用已后复权的序列（升序，含当日点）填充卡片的
// Score/YZVol/Quantile/Weight；历史不足时相应字段保持 NaN 并记 Message。
// ComputeSignal（盘中快照作当日点）与 ComputeCloseSignal（官方收盘作当日点）共用。
func (c *SignalComputer) scoreFromSeries(cur *SignalCard, series []DailyBarAdj) {
	if len(series) < momentumWindow {
		cur.Message = fmt.Sprintf("历史不足 %d 点，无法打分", momentumWindow)
		return
	}
	cur.Score = momentumScore(series)

	// YZ 波动率序列：yz[i] 需要 i-20..i 共 21 个点，故 i 从 yzWindow 起。
	if len(series) > yzWindow {
		yz := make([]float64, 0, len(series)-yzWindow)
		for i := yzWindow; i < len(series); i++ {
			yz = append(yz, yzVol(series[i-yzWindow:i+1], yzWindow))
		}
		cur.YZVol = yz[len(yz)-1]
		// 分位窗口含当前值本身（与 rotation7.py pct_of 一致）；
		// 可计算值不足整个窗口时 q 缺失，权重回退 1。
		if len(yz) >= c.quantileWindow {
			w := yz[len(yz)-c.quantileWindow:]
			cur.Quantile = quantileRank(w, cur.YZVol)
			cur.Weight = throttleWeight(cur.Quantile)
		}
	}
}

// rankCards 按 Score 降序填名次（1 最高）；无法打分（NaN）的卡片 Rank 保持 0。
func rankCards(cards []SignalCard) {
	order := make([]int, 0, len(cards))
	for i, card := range cards {
		if !math.IsNaN(card.Score) {
			order = append(order, i)
		}
	}
	sort.Slice(order, func(a, b int) bool {
		return cards[order[a]].Score > cards[order[b]].Score
	})
	for rank, i := range order {
		cards[i].Rank = rank + 1
	}
}

// momentumScore 计算 ER 加权动量（窗口 20 个点，运行手册 §2，口径同 rotation7.py）：
// M_t = (O+H+L+C)/4（后复权四点均值），L_t = ln M_t，
// D = L_t − L_{t−19}，P = Σ_{i=t−18..t} |L_i − L_{i−1}|，score = |D|/P × D。
// series 为后复权日线（升序），至少 momentumWindow 个点，取末尾 20 点。
func momentumScore(series []DailyBarAdj) float64 {
	n := len(series)
	logM := make([]float64, momentumWindow)
	for i := 0; i < momentumWindow; i++ {
		b := series[n-momentumWindow+i]
		logM[i] = math.Log((b.Open + b.High + b.Low + b.Close) / 4)
	}
	d := logM[momentumWindow-1] - logM[0]
	var p float64
	for i := 1; i < momentumWindow; i++ {
		p += math.Abs(logM[i] - logM[i-1])
	}
	if p == 0 {
		return 0
	}
	return math.Abs(d) / p * d
}

// yzVol 计算 Yang-Zhang 年化波动率（窗口 win，×240），口径同 rotation7.py yz_vol：
// σ² = σ²_overnight + k·σ²_intraday + (1−k)·mean(RS)，k = 0.34/(1.34+(win+1)/(win-1))，
// win=20 时 k ≈ 0.1390；overnight = ln(O_i/C_{i−1})，intraday = ln(C_i/O_i)，均取样本方差（ddof=1）；
// RS（Rogers-Satchell）取均值。bars 需 win+1 个点（首点仅供昨收）。
func yzVol(bars []DailyBarAdj, win int) float64 {
	n := len(bars)
	k := 0.34 / (1.34 + float64(win+1)/float64(win-1))
	overnight := make([]float64, 0, win)
	intraday := make([]float64, 0, win)
	rs := make([]float64, 0, win)
	for i := n - win; i < n; i++ {
		o, h, l, c := bars[i].Open, bars[i].High, bars[i].Low, bars[i].Close
		overnight = append(overnight, math.Log(o/bars[i-1].Close))
		intraday = append(intraday, math.Log(c/o))
		rs = append(rs, math.Log(h/c)*math.Log(h/o)+math.Log(l/c)*math.Log(l/o))
	}
	variance := sampleVariance(overnight) + k*sampleVariance(intraday) + (1-k)*mean(rs)
	return math.Sqrt(variance * yzAnnualFactor)
}

// quantileRank 计算 current 在 window 内的分位（并列取半 / average-rank），0~100。
// window 含当前值本身（与 rotation7.py pct_of 一致）。
func quantileRank(window []float64, current float64) float64 {
	var less, equal float64
	for _, v := range window {
		switch {
		case v < current:
			less++
		case v == current:
			equal++
		}
	}
	return (less + 0.5*equal) / float64(len(window)) * 100
}

// throttleWeight 是 70/40 节流折线（运行手册 §2）：
// q ≤ 70 → 1；70 < q < 100 → 1 − 0.6×(q−70)/30；q ≥ 100 → 0.4。q 缺失（NaN）回退 1。
func throttleWeight(q float64) float64 {
	if math.IsNaN(q) {
		return 1.0
	}
	return math.Max(0.4, math.Min(1.0, 1-0.6*(q-70)/30))
}

// sampleVariance 是样本方差（ddof=1，与 numpy var(ddof=1) 一致）。
func sampleVariance(xs []float64) float64 {
	m := mean(xs)
	var sum float64
	for _, x := range xs {
		sum += (x - m) * (x - m)
	}
	return sum / float64(len(xs)-1)
}

func mean(xs []float64) float64 {
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// calendarDays 返回 to − from 的日历天数（均 YYYYMMDD）。
func calendarDays(from, to string) (int, error) {
	t0, err := time.Parse("20060102", from)
	if err != nil {
		return 0, fmt.Errorf("非法日期 %q（期望 YYYYMMDD）", from)
	}
	t1, err := time.Parse("20060102", to)
	if err != nil {
		return 0, fmt.Errorf("非法日期 %q（期望 YYYYMMDD）", to)
	}
	return int(t1.Sub(t0).Hours() / 24), nil
}
