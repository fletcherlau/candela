package core

// FieldDiff 是单字段的差值：官方日线 − 盘中快照（Slippage Diff，CONTEXT.md）。
// Bps 以快照为基准：bps = (official − snapshot) / snapshot × 10⁴，
// 正值表示官方价高于 14:45 快照（尾盘继续走高），负值相反。
// 运行手册 §7 监控的四点均值差即用此口径。
type FieldDiff struct {
	Abs float64 // 绝对差（价格单位）
	Bps float64 // 相对差，万分之一（快照为基准）
}

// InstrumentDiff 是单标的的 Slippage Diff（滑点差值）：
// 官方日线 OHLC 对盘中快照 O/H/L/Latest 的逐字段差 + 四点均值差。
type InstrumentDiff struct {
	TsCode string
	// Available 为 false 表示当日官方日线或盘中快照缺失，无差值（Message 记原因）。
	Available bool
	Open      FieldDiff // 官方 open vs 快照 open
	High      FieldDiff // 官方 high vs 快照 high
	Low       FieldDiff // 官方 low vs 快照 low
	Close     FieldDiff // 官方 close vs 快照 latest（latest 非收盘，尾盘位移主要体现于此）
	MeanBps   float64   // 四点均值差 bps：官方 (O+H+L+C)/4 vs 快照 (O+H+L+Latest)/4，快照均值为基准
	Message   string
}

// BuildSlippageDiffs 逐标的比对当日官方日线与盘中快照。
// bars 为各标的当日官方日线（key: tsCode，调用方保证日期已对齐当日）；
// snaps 为当日盘中快照（key: tsCode）。任一缺失则该标的 Available=false，不中断其余标的。
func BuildSlippageDiffs(tsCodes []string, bars map[string]DailyBarAdj, snaps map[string]IntradaySnapshot) []InstrumentDiff {
	diffs := make([]InstrumentDiff, 0, len(tsCodes))
	for _, code := range tsCodes {
		diffs = append(diffs, InstrumentDiff{TsCode: code})
		cur := &diffs[len(diffs)-1]

		snap, ok := snaps[code]
		if !ok {
			cur.Message = "无当日盘中快照（14:45 未取数或非交易日），无滑点差值"
			continue
		}
		bar, ok := bars[code]
		if !ok {
			cur.Message = "无当日官方日线（未同步或停牌），无滑点差值"
			continue
		}
		cur.Available = true
		cur.Open = fieldDiff(bar.Open, snap.Open)
		cur.High = fieldDiff(bar.High, snap.High)
		cur.Low = fieldDiff(bar.Low, snap.Low)
		cur.Close = fieldDiff(bar.Close, snap.Latest)
		snapMean := (snap.Open + snap.High + snap.Low + snap.Latest) / 4
		officialMean := (bar.Open + bar.High + bar.Low + bar.Close) / 4
		cur.MeanBps = bpsOf(officialMean, snapMean)
	}
	return diffs
}

// fieldDiff 计算单字段差值：绝对差 + 相对 bps（快照为基准，见 FieldDiff 注释）。
func fieldDiff(official, snapshot float64) FieldDiff {
	return FieldDiff{Abs: official - snapshot, Bps: bpsOf(official, snapshot)}
}

// bpsOf 是以 base（快照）为基准的相对差，单位万分之一。
// base 为 0 时返回 0（防御；ETF 价格不会为 0）。
func bpsOf(official, base float64) float64 {
	if base == 0 {
		return 0
	}
	return (official - base) / base * 10000
}
