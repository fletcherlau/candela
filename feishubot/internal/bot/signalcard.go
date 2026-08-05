// signalcard.go 把 syncer 的盘中信号结果渲染成飞书卡片 lark_md 文本（Signal Card Push 用）。
// 卡片无状态：只呈现 score / q / w(q) / 排名，不含「换/不换」结论。
package bot

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// RenderSignalCard 生成盘中信号卡片（markdown）。
// 按名次排序（rank 1 最高并粗体高亮，无法打分的 rank 0 垫底）；
// 任一标的新鲜度守卫触发时，顶部给出显著告警。
func RenderSignalCard(resp *SignalResp, now time.Time) string {
	var b strings.Builder
	b.WriteString("**信号卡片**\n\n")
	if resp == nil {
		b.WriteString("syncer 未返回结果")
		return b.String()
	}

	fmt.Fprintf(&b, "数据时间：%s（盘中快照）\n\n", dash(formatDate(resp.SnapshotDate)))

	// 新鲜度告警置顶。
	var stale []string
	for _, c := range resp.Cards {
		if c.Stale {
			stale = append(stale, c.TsCode)
		}
	}
	if len(stale) > 0 {
		fmt.Fprintf(&b, "**数据新鲜度告警：%s 快照过期或历史滞后，相关信号仅供参考**\n\n", strings.Join(stale, "、"))
	}

	// 按名次排序渲染（rank 1 粗体高亮，rank 0 垫底）。
	writeRankedCards(&b, resp.Cards)

	fmt.Fprintf(&b, "生成时间：%s", now.Format("2006-01-02 15:04:05"))
	return b.String()
}

// writeRankedCards 按名次渲染信号卡片列表：rank >= 1 升序在前（rank 1 粗体高亮），
// rank 0（无法打分）垫底并保持原顺序。Signal Card 与 Close Report 的收盘重算段共用。
func writeRankedCards(b *strings.Builder, items []SignalCardItem) {
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

	for _, c := range cards {
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
