// advice.go 由盘中信号卡片推导五种持仓情形（现金 + 四标的一）的交易建议。
// 规则一次定死（运行手册 §3/§4 v1.3），纯函数无 I/O，确定性输出。
package core

import (
	"fmt"
	"math"
	"sort"
)

const (
	// gapDelta 是差距缓冲 δ（运行手册 §3）：第一名与持仓得分差 ≤ δ 视为噪音，不换仓。
	gapDelta = 0.005
	// premiumWatchCode 是 QDII 溢价检查标的（运行手册 §5）：
	// 第一名是它时，买入/换入建议附加「下单前确认溢价率 ≤1%」。
	premiumWatchCode = "513100.SH"
	// premiumNote 是 QDII 溢价提示（溢价率 > 1% 当日不换入，顺延第二名，次日重算）。
	premiumNote = "下单前确认 513100 溢价率 ≤1%，否则顺延第二名（次日重算）"
	// cashScenario 是空仓情形的情形名（建议表第一行）。
	cashScenario = "现金"
)

// AdviceItem 是一种持仓情形下的一句操作建议（Signal Card 建议表的一行）。
type AdviceItem struct {
	// Scenario 是情形名："现金" 或持仓标的代码。
	Scenario string `json:"scenario"`
	// Action 是操作建议：买入/换入（带目标标的代码）、持有（不换）、不下单。
	Action string `json:"action"`
	// TargetWeight 是目标仓位（标的占比，其余为货币 ETF）；不下单/维持不动时为 null。
	TargetWeight *float64 `json:"targetWeight"`
	// Note 是规则依据说明（安全阀/差距缓冲/5pp 微调死区/QDII 溢价提示等）。
	Note string `json:"note"`
}

// ComputeAdvice 由信号卡片推导五种持仓情形（现金 + 每个标的）的操作建议。纯函数，无 I/O。
//
// 规则（运行手册 §3/§4 v1.3）：
//   - 持有现金 → 买入第一名，仓位 w(第一名)；
//   - 持有 X 且 X 是第一名 → 不换仓；实际仓位偏离 w(X) ≥ 5 个百分点时，一笔微调到 w(X)（货基反向同调）；
//   - 持有 X ≠ 第一名且 score(X) < 0 → 安全阀：无视差距，换入第一名，仓位 w(第一名)；
//   - 持有 X ≠ 第一名且 score(第一名) − score(X) ≤ 0.005 → 差距缓冲：不换，仓位微调同上（5pp 死区）；
//   - 否则 → 换入第一名，仓位 w(第一名)；
//   - 第一名是 513100.SH → 买入/换入情形附加：下单前确认溢价率 ≤1%，否则顺延第二名（次日重算）；
//   - score 为 NaN 的标的不参与建议（恰好持有时维持持仓与仓位不动）；第一名缺失时建议「数据不足，今日不下单」。
//
// 返回顺序固定：现金第一行，其余按名次升序（rank 0 即无法打分的垫底，保持输入相对顺序）。
func ComputeAdvice(cards []SignalCard) []AdviceItem {
	var first *SignalCard
	for i := range cards {
		if cards[i].Rank == 1 {
			first = &cards[i]
			break
		}
	}

	sorted := make([]SignalCard, len(cards))
	copy(sorted, cards)
	sort.SliceStable(sorted, func(a, b int) bool {
		ra, rb := sorted[a].Rank, sorted[b].Rank
		if ra == 0 {
			return false
		}
		if rb == 0 {
			return true
		}
		return ra < rb
	})

	items := make([]AdviceItem, 0, len(sorted)+1)

	// 第一名缺失（全部标的无法打分）：所有情形不下单。
	if first == nil {
		items = append(items, AdviceItem{Scenario: cashScenario, Action: "不下单", Note: "数据不足，今日不下单"})
		for _, c := range sorted {
			items = append(items, AdviceItem{Scenario: c.TsCode, Action: "不下单", Note: "数据不足，今日不下单"})
		}
		return items
	}

	premium := first.TsCode == premiumWatchCode

	// 情形一：持有现金 → 买入第一名。
	cash := AdviceItem{
		Scenario:     cashScenario,
		Action:       "买入 " + first.TsCode,
		TargetWeight: weightPtr(first.Weight),
		Note:         fmt.Sprintf("买入第一名，仓位 %.2f；空仓部分买货币 ETF", first.Weight),
	}
	if premium {
		cash.Note += "；" + premiumNote
	}
	items = append(items, cash)

	// 情形二~五：持有某个标的。
	for _, c := range sorted {
		item := AdviceItem{Scenario: c.TsCode}
		switch {
		case math.IsNaN(c.Score):
			// 数据不足的标的不参与打分；恰好持有时维持不动（运行手册 §1）。
			item.Action = "持有"
			item.Note = c.TsCode + " 数据不足（score 缺失），当日不参与打分；维持当前持仓与仓位不动"
		case c.TsCode == first.TsCode:
			item.Action = "持有"
			item.TargetWeight = weightPtr(c.Weight)
			item.Note = "不换仓；实际仓位偏离目标仓位 ≥ 5 个百分点时，一笔微调到目标仓位（货基反向同调）"
		case c.Score < 0:
			item.Action = "换入 " + first.TsCode
			item.TargetWeight = weightPtr(first.Weight)
			item.Note = "安全阀：持仓得分 < 0，无视差距换入第一名；卖出旧仓，空仓部分买货币 ETF"
			if premium {
				item.Note += "；" + premiumNote
			}
		case first.Score-c.Score <= gapDelta:
			item.Action = "持有"
			item.TargetWeight = weightPtr(c.Weight)
			item.Note = fmt.Sprintf("差距缓冲：第一名与持仓得分差 %.4f ≤ 0.005，不换仓；实际仓位偏离目标 ≥ 5 个百分点时微调（货基反向同调）", first.Score-c.Score)
		default:
			item.Action = "换入 " + first.TsCode
			item.TargetWeight = weightPtr(first.Weight)
			item.Note = "得分差距超过缓冲，换入第一名；卖出旧仓，空仓部分买货币 ETF"
			if premium {
				item.Note += "；" + premiumNote
			}
		}
		items = append(items, item)
	}
	return items
}

func weightPtr(w float64) *float64 { return &w }
