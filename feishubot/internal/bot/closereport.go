// closereport.go 把 syncer 的收盘日报渲染成飞书卡片 lark_md 文本（Close Report Push 用）。
// 三段式：① 同步结果摘要 → ② Slippage Diff（滑点差值）→ ③ 官方收盘重算的排名列表。
// 非交易日或无盘中快照时降级为纯同步摘要（18:00 日报每天照推，
// 不同于 14:45 信号卡片非交易日短路不推）。
package bot

import (
	"fmt"
	"strings"
	"time"
)

// RenderCloseReport 生成收盘日报卡片（markdown）。
func RenderCloseReport(resp *CloseReportResp, now time.Time) string {
	var b strings.Builder
	b.WriteString("**收盘日报**\n\n")
	if resp == nil {
		b.WriteString("syncer 未返回结果")
		return b.String()
	}
	fmt.Fprintf(&b, "交易日：%s\n\n", dash(formatDate(resp.TradeDate)))

	// ① 同步结果摘要。
	b.WriteString(RenderSyncSummary(&resp.Sync))
	b.WriteString("\n")

	// 降级：非交易日或无盘中快照 → 纯同步摘要，无差值与重算段落。
	if !resp.TradingDay || !resp.HasSnapshot {
		b.WriteString("非交易日或无盘中快照，今日无滑点差值与收盘重算\n\n")
		fmt.Fprintf(&b, "生成时间：%s", now.Format("2006-01-02 15:04:05"))
		return b.String()
	}

	// ② Slippage Diff：官方日线 − 14:45 盘中快照，bps 以快照为基准
	// （正 = 官方高于快照，口径见 CONTEXT.md「Slippage Diff」）。
	b.WriteString("**滑点差值**（官方 − 14:45 快照，bps 以快照为基准）\n\n")
	for _, d := range resp.Diffs {
		head := d.TsCode
		if d.Name != "" {
			head = fmt.Sprintf("%s（%s）", d.Name, d.TsCode)
		}
		fmt.Fprintf(&b, "**%s**\n", head)
		if !d.Available {
			fmt.Fprintf(&b, "%s\n\n", d.Message)
			continue
		}
		fmt.Fprintf(&b, "开 %s · 高 %s · 低 %s · 收 %s\n",
			formatFieldDiff(d.Open), formatFieldDiff(d.High), formatFieldDiff(d.Low), formatFieldDiff(d.Close))
		fmt.Fprintf(&b, "四点均值差 %s\n\n", formatSignedBps(d.MeanBps))
	}

	// ③ 官方收盘重算：与信号卡片同一渲染约定（rank 1 粗体高亮，无法打分垫底）。
	b.WriteString("**收盘重算**（官方收盘作当日第 20 点）\n\n")
	writeRankedCards(&b, resp.Cards)

	fmt.Fprintf(&b, "生成时间：%s", now.Format("2006-01-02 15:04:05"))
	return b.String()
}

// formatFieldDiff 渲染单字段差值：绝对差（4 位小数，带号）+ bps（带号）；缺失为 -。
func formatFieldDiff(d *DiffField) string {
	if d == nil {
		return "-"
	}
	return fmt.Sprintf("%+.4f（%+.2fbps）", d.Abs, d.Bps)
}

// formatSignedBps 渲染带号 bps（2 位小数）；缺失为 -。
func formatSignedBps(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%+.2fbps", *v)
}
