// signalcard.go 把 syncer 的盘中信号结果渲染成飞书卡片（Signal Card Push 用）。
// 卡片为 JSON 2.0 schema，用原生 table 组件呈现信号表与五情形交易建议表；
// writeRankedCards 等 markdown 渲染件仍供 Close Report 的收盘重算段使用。
package bot

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// BuildSignalCardJSON 生成盘中信号卡片的飞书卡片 JSON（2.0 schema，原生表格组件）。纯函数。
// 信号表：排名/标的/动量得分 score/年化波动 σ_YZ/波动分位 q/目标仓位 w(q)，按名次排序，
// rank 1 的标的单元格粗体高亮；建议表：若你当前持有/操作建议/目标仓位（现金 + 各标的）。
// 任一标的新鲜度守卫触发时，表格上方给出红色告警；底部保留数据时间/生成时间。
func BuildSignalCardJSON(resp *SignalResp, now time.Time) string {
	if resp == nil {
		return marshalSignalCard([]map[string]any{markdownElement("syncer 未返回结果")})
	}

	elements := []map[string]any{
		markdownElement(fmt.Sprintf("数据时间：%s（盘中快照）", dash(formatDate(resp.SnapshotDate)))),
	}

	// 新鲜度告警置顶（表格上方）。
	var stale []string
	for _, c := range resp.Cards {
		if c.Stale {
			stale = append(stale, c.TsCode)
		}
	}
	if len(stale) > 0 {
		elements = append(elements, markdownElement(fmt.Sprintf(
			"<font color='red'>**数据新鲜度告警：%s 快照过期或历史滞后，相关信号仅供参考**</font>",
			strings.Join(stale, "、"))))
	}

	// 信号表 + 打分缺失原因附注。
	elements = append(elements, signalTable(resp.Cards))
	var cardNotes []string
	for _, c := range resp.Cards {
		if c.Message != "" {
			cardNotes = append(cardNotes, fmt.Sprintf("%s：%s", c.TsCode, c.Message))
		}
	}
	if len(cardNotes) > 0 {
		elements = append(elements, markdownElement("<font color='grey'>"+strings.Join(cardNotes, "\n")+"</font>"))
	}

	// 建议表 + 5pp 微调死区说明（运行手册 §4 v1.3）。
	elements = append(elements, adviceTable(resp.Advice, namesByCode(resp.Cards)))
	elements = append(elements, markdownElement("<font color='grey'>注：建议为「持有」时，若实际仓位偏离目标仓位 ≥ 5 个百分点，一笔微调到目标仓位（货基反向同调）。</font>"))

	// QDII 溢价提示：syncer 在涉及买入 513100 的建议 note 中附加，这里提升为醒目一行。
	for _, a := range resp.Advice {
		if strings.Contains(a.Note, "溢价率") {
			elements = append(elements, markdownElement("<font color='red'>**下单前确认 513100 溢价率 ≤1%，否则顺延第二名（次日重算）**</font>"))
			break
		}
	}

	elements = append(elements, markdownElement(fmt.Sprintf("生成时间：%s", now.Format("2006-01-02 15:04:05"))))
	return marshalSignalCard(elements)
}

// marshalSignalCard 打包 2.0 schema 卡片（蓝色标题头 + body elements）。
func marshalSignalCard(elements []map[string]any) string {
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{"wide_screen_mode": true, "update_multi": true},
		"header": map[string]any{
			"template": "blue",
			"title":    map[string]string{"tag": "plain_text", "content": "信号卡片"},
		},
		"body": map[string]any{"elements": elements},
	}
	data, _ := json.Marshal(card)
	return string(data)
}

// markdownElement 组装一个富文本 markdown 组件（卡片 JSON 2.0）。
func markdownElement(content string) map[string]any {
	return map[string]any{"tag": "markdown", "content": content}
}

// signalTable 组装信号表（table 组件）：排名/标的/动量得分/年化波动/波动分位/目标仓位。
// 按名次排序（rank 1 粗体高亮标的单元格，rank 0 垫底）；缺失值渲染为 -。
func signalTable(items []SignalCardItem) map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, c := range sortCardsByRank(items) {
		head := c.TsCode
		if c.Name != "" {
			head = fmt.Sprintf("%s（%s）", c.Name, c.TsCode)
		}
		if c.Rank == 1 {
			head = "**" + head + "**"
		}
		rank := "—"
		if c.Rank > 0 {
			rank = fmt.Sprint(c.Rank)
		}
		rows = append(rows, map[string]any{
			"rank":       rank,
			"instrument": head,
			"score":      formatScore(c.Score),
			"yzvol":      formatPct(c.YZVol),
			"quantile":   formatQuantile(c.Quantile),
			"weight":     fmt.Sprintf("%.2f", c.Weight),
		})
	}
	return map[string]any{
		"tag":        "table",
		"page_size":  10,
		"row_height": "low",
		"header_style": map[string]any{
			"background_style": "grey",
			"bold":             true,
			"lines":            2,
		},
		"columns": []map[string]any{
			{"name": "rank", "display_name": "排名", "data_type": "text", "horizontal_align": "center", "width": "auto"},
			{"name": "instrument", "display_name": "标的", "data_type": "lark_md", "horizontal_align": "left", "width": "auto"},
			{"name": "score", "display_name": "动量得分 score", "data_type": "text", "horizontal_align": "right", "width": "auto"},
			{"name": "yzvol", "display_name": "年化波动 σ_YZ", "data_type": "text", "horizontal_align": "right", "width": "auto"},
			{"name": "quantile", "display_name": "波动分位 q", "data_type": "text", "horizontal_align": "right", "width": "auto"},
			{"name": "weight", "display_name": "目标仓位 w(q)", "data_type": "text", "horizontal_align": "right", "width": "auto"},
		},
		"rows": rows,
	}
}

// adviceTable 组装建议表（table 组件）：若你当前持有/操作建议/目标仓位。
// 情形为标的代码时带上名称；目标仓位为 null（不下单/维持不动）时渲染为 —。
func adviceTable(advice []AdviceItem, names map[string]string) map[string]any {
	rows := make([]map[string]any, 0, len(advice))
	for _, a := range advice {
		scenario := a.Scenario
		if name := names[a.Scenario]; name != "" {
			scenario = fmt.Sprintf("%s（%s）", name, a.Scenario)
		}
		weight := "—"
		if a.TargetWeight != nil {
			weight = fmt.Sprintf("%.2f", *a.TargetWeight)
		}
		rows = append(rows, map[string]any{
			"scenario": scenario,
			"action":   actionCell(a),
			"weight":   weight,
		})
	}
	return map[string]any{
		"tag":        "table",
		"page_size":  10,
		"row_height": "low",
		"header_style": map[string]any{
			"background_style": "grey",
			"bold":             true,
			"lines":            2,
		},
		"columns": []map[string]any{
			{"name": "scenario", "display_name": "若你当前持有", "data_type": "text", "horizontal_align": "left", "width": "auto"},
			{"name": "action", "display_name": "操作建议", "data_type": "text", "horizontal_align": "left", "width": "auto"},
			{"name": "weight", "display_name": "目标仓位", "data_type": "text", "horizontal_align": "right", "width": "auto"},
		},
		"rows": rows,
	}
}

// actionCell 在操作建议后附上简短规则标签（安全阀/差距缓冲/数据不足），详情见 note 字段。
func actionCell(a AdviceItem) string {
	switch {
	case strings.Contains(a.Note, "安全阀"):
		return a.Action + "（安全阀）"
	case strings.Contains(a.Note, "差距缓冲"):
		return a.Action + "（差距缓冲）"
	case strings.Contains(a.Note, "数据不足"):
		return a.Action + "（数据不足）"
	default:
		return a.Action
	}
}

// namesByCode 建立标的代码 → 名称映射（建议表情形列展示用）。
func namesByCode(items []SignalCardItem) map[string]string {
	out := make(map[string]string, len(items))
	for _, c := range items {
		if c.Name != "" {
			out[c.TsCode] = c.Name
		}
	}
	return out
}

// writeRankedCards 按名次渲染信号卡片列表：rank >= 1 升序在前（rank 1 粗体高亮），
// rank 0（无法打分）垫底并保持原顺序。Close Report 的收盘重算段使用。
func writeRankedCards(b *strings.Builder, items []SignalCardItem) {
	for _, c := range sortCardsByRank(items) {
		head := c.TsCode
		if c.Name != "" {
			head = fmt.Sprintf("%s（%s）", c.Name, c.TsCode)
		}
		rank := "—"
		if c.Rank > 0 {
			rank = fmt.Sprint(c.Rank)
		}
		if c.Rank == 1 {
			fmt.Fprintf(b, "**1｜%s**\n", head)
		} else {
			fmt.Fprintf(b, "%s｜%s\n", rank, head)
		}
		fmt.Fprintf(b, "score %s · σ_YZ %s · q %s · w(q) %.2f\n",
			formatScore(c.Score), formatPct(c.YZVol), formatQuantile(c.Quantile), c.Weight)
		if c.Message != "" {
			fmt.Fprintf(b, "%s\n", c.Message)
		}
		b.WriteString("\n")
	}
}

// sortCardsByRank 返回按名次排序的副本：rank >= 1 升序在前，rank 0（无法打分）垫底并保持原顺序。
func sortCardsByRank(items []SignalCardItem) []SignalCardItem {
	cards := make([]SignalCardItem, len(items))
	copy(cards, items)
	sort.SliceStable(cards, func(a, b int) bool {
		ra, rb := cards[a].Rank, cards[b].Rank
		if ra == 0 {
			return false
		}
		if rb == 0 {
			return true
		}
		return ra < rb
	})
	return cards
}

// formatScore 渲染动量得分（4 位小数）；缺失（null，历史不足）为 -。
func formatScore(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.4f", *v)
}

// formatPct 渲染年化波动率为百分数（0.0812 → 8.12%）；缺失为 -。
func formatPct(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", *v*100)
}

// formatQuantile 渲染分位（0~100，并列取半）；缺失为 -。
func formatQuantile(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", *v)
}

// formatDate 把 YYYYMMDD 渲染为 YYYY-MM-DD；其他格式原样返回。
func formatDate(s string) string {
	if len(s) == 8 {
		return s[:4] + "-" + s[4:6] + "-" + s[6:8]
	}
	return s
}
