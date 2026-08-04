// report.go 把 syncer 的状态/同步结果渲染成飞书卡片 lark_md 文本，
// status 命令回复与 Daily Report Push 共用。
package bot

import (
	"fmt"
	"strings"
	"time"
)

// RenderStatusReport 生成同步状态日报（markdown）。
func RenderStatusReport(st *StatusResp, now time.Time) string {
	var b strings.Builder
	b.WriteString("**ETF 日线同步日报**\n\n")
	if st == nil || len(st.Instruments) == 0 {
		b.WriteString("暂无标的\n\n")
	} else {
		for _, it := range st.Instruments {
			enabled := ""
			if !it.SyncEnabled {
				enabled = "（已停用）"
			}
			fmt.Fprintf(&b, "**%s**（%s）%s\n", it.Name, it.TsCode, enabled)
			fmt.Fprintf(&b, "行情截至 %s · 因子截至 %s\n", dash(it.LatestTradeDate), dash(it.LatestAdjDate))
			fmt.Fprintf(&b, "行数：行情 %d / 因子 %d\n\n", it.DailyRows, it.AdjRows)
		}
	}
	fmt.Fprintf(&b, "生成时间：%s", now.Format("2006-01-02 15:04:05"))
	return b.String()
}

// RenderSyncSummary 生成增量同步结果摘要（markdown）。
func RenderSyncSummary(resp *SyncResp) string {
	var b strings.Builder
	b.WriteString("**同步结果**\n\n")
	if resp == nil {
		b.WriteString("syncer 未返回结果")
		return b.String()
	}
	fmt.Fprintf(&b, "合计 %d 只，成功 %d 只\n\n", resp.Total, resp.Success)
	for _, r := range resp.Results {
		msg := r.Message
		if msg == "" {
			msg = "ok"
		}
		fmt.Fprintf(&b, "**%s**：写入 %d 行（行情 %d / 因子 %d）· %s\n", r.TsCode, r.Upserted, r.DailyUpserted, r.AdjUpserted, msg)
	}
	return b.String()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
