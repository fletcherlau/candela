package core

import (
	"fmt"
	"math"
)

const BacktestVersion = "v1.4-close-5pp-cash0-cost10-v1"

var RotationCodes = []string{"510880.SH", "518880.SH", "159915.SZ", "513100.SH"}
var RotationNames = []string{"红利 ETF", "黄金 ETF", "创业板 ETF", "纳指 ETF"}

type BacktestDay struct {
	Suspended  []string  `json:"suspended,omitempty"`
	Date       string    `json:"date"`
	NAV        float64   `json:"nav"`
	Holding    string    `json:"holding"`
	Weight     float64   `json:"weight"`
	CashWeight float64   `json:"cashWeight"`
	Cost       float64   `json:"cost"`
	Turnover   float64   `json:"turnover"`
	Benchmarks []float64 `json:"benchmarks"`
}
type BacktestResult struct {
	Version string        `json:"version"`
	Start   string        `json:"start"`
	End     string        `json:"end"`
	Days    []BacktestDay `json:"days"`
	Codes   []string      `json:"codes"`
	Names   []string      `json:"names"`
	CostBPS float64       `json:"costBps"`
}

// Backtest consumes strictly aligned, already adjusted bars. Calendar and factor
// completeness are checked at the data boundary before this pure calculation.
func Backtest(panels [][]DailyBarAdj) (*BacktestResult, error) {
	if len(panels) != 4 {
		return nil, fmt.Errorf("需要四只 ETF 的完整行情")
	}
	n := len(panels[0])
	if n < 1220 {
		return nil, fmt.Errorf("有效历史不足：需要 1220 个交易日完成指标预热，当前 %d 个", n)
	}
	scores := make([][]float64, 4)
	weights := make([][]float64, 4)
	start := 0
	for j, bars := range panels {
		if len(bars) != n {
			return nil, fmt.Errorf("四标的交易日期未对齐")
		}
		scores[j] = make([]float64, n)
		weights[j] = make([]float64, n)
		history := make([]DailyBarAdj, 0, n)
		yz := make([]float64, 0, n)
		firstReady := -1
		for t, b := range bars {
			if b.TradeDate != panels[0][t].TradeDate || (t > 0 && b.TradeDate <= bars[t-1].TradeDate) {
				return nil, fmt.Errorf("四标的交易日期未对齐")
			}
			scores[j][t] = math.NaN()
			if b.Suspended {
				if t == 0 || b.Close != bars[t-1].Close {
					return nil, fmt.Errorf("停牌估值必须来自上一有效收盘")
				}
				if t > 0 {
					weights[j][t] = weights[j][t-1]
				}
				continue
			}
			if !validBacktestBar(b) {
				return nil, fmt.Errorf("%s %s 行情无效", RotationCodes[j], b.TradeDate)
			}
			history = append(history, b)
			h := len(history)
			if h >= 20 {
				scores[j][t] = momentumScore(history[h-20:])
			}
			if h >= 21 {
				v := yzVol(history[h-21:], 20)
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, fmt.Errorf("%s %s 波动率无效", RotationCodes[j], b.TradeDate)
				}
				yz = append(yz, v)
				if len(yz) >= 1200 {
					weights[j][t] = throttleWeight(quantileRank(yz[len(yz)-1200:], v))
					if firstReady < 0 {
						firstReady = t
					}
				}
			}
		}
		if firstReady < 0 {
			return nil, fmt.Errorf("%s 有效历史不足以完成 1200 日波动分位预热", RotationCodes[j])
		}
		if firstReady > start {
			start = firstReady
		}
	}
	out := &BacktestResult{Version: BacktestVersion, Codes: append([]string{}, RotationCodes...), Names: append([]string{}, RotationNames...), CostBPS: 10, Days: []BacktestDay{}}
	pos := -1
	asset, cash := 0.0, 1.0
	for t := start; t < n; t++ {
		if pos >= 0 {
			asset *= panels[pos][t].Close / panels[pos][t-1].Close
		}
		values := make([]float64, 4)
		for j := range values {
			values[j] = scores[j][t]
		}
		target := rotationTarget(values, pos)
		before := asset + cash
		cost, turnover := 0.0, 0.0
		if pos >= 0 && panels[pos][t].Suspended {
			// A suspended holding cannot be sold or rebalanced.
		} else if target >= 0 && target != pos {
			// Sell the old leg, then buy to a post-fee weight using only available cash.
			sellCost := asset * .001
			turnover += asset
			cash += asset - sellCost
			asset = 0
			cost += sellCost
			asset, cash, cost, turnover = rebalanceCash(asset, cash, weights[target][t], cost, turnover)
			pos = target
		} else if target >= 0 && needsRotationAdjustment(asset/before, weights[target][t]) {
			asset, cash, cost, turnover = rebalanceCash(asset, cash, weights[target][t], 0, 0)
		}
		nav := asset + cash
		bench := make([]float64, 4)
		for j := range bench {
			bench[j] = panels[j][t].Close / panels[j][start].Close
		}
		suspended := []string{}
		for j := range panels {
			if panels[j][t].Suspended {
				suspended = append(suspended, RotationCodes[j])
			}
		}
		holding := ""
		if pos >= 0 {
			holding = RotationCodes[pos]
		}
		out.Days = append(out.Days, BacktestDay{Suspended: suspended, Date: panels[0][t].TradeDate, NAV: nav, Holding: holding, Weight: asset / nav, CashWeight: cash / nav, Cost: cost, Turnover: turnover, Benchmarks: bench})
	}
	out.Start = out.Days[0].Date
	out.End = out.Days[len(out.Days)-1].Date
	return out, nil
}
func rotationTarget(scores []float64, pos int) int {
	target := -1
	if pos >= 0 && !math.IsNaN(scores[pos]) {
		target = pos
	}
	for j, v := range scores {
		if !math.IsNaN(v) && (target < 0 || v > scores[target]) {
			target = j
		}
	}
	if target < 0 {
		return pos
	}
	if pos >= 0 && target != pos && scores[target]-scores[pos] <= .005+1e-12 {
		return pos
	}
	return target
}

// Solve the post-fee target exactly. No trade creates or borrows cash.
func rebalanceCash(asset, cash, w, cost, turnover float64) (float64, float64, float64, float64) {
	total := asset + cash
	delta := (w*total - asset) / (1 + .001*w)
	if delta < 0 {
		delta = (w*total - asset) / (1 - .001*w)
	}
	fee := math.Abs(delta) * .001
	return asset + delta, cash - delta - fee, cost + fee, turnover + math.Abs(delta)
}
func validBacktestBar(b DailyBarAdj) bool {
	for _, v := range []float64{b.Open, b.High, b.Low, b.Close} {
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return b.High >= math.Max(b.Open, b.Close) && b.Low <= math.Min(b.Open, b.Close) && b.High >= b.Low
}

func needsRotationAdjustment(actual, target float64) bool {
	return math.Abs(actual-target)+1e-12 >= .05
}
