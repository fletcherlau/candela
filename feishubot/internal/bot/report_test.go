package bot

import (
	"strings"
	"testing"
	"time"
)

func TestRenderStatusReport(t *testing.T) {
	now := time.Date(2026, 8, 4, 18, 0, 0, 0, time.Local)
	md := RenderStatusReport(&StatusResp{Instruments: []InstrumentStatusItem{
		{
			TsCode:          "510300.SH",
			Name:            "沪深300ETF",
			SyncEnabled:     true,
			LatestTradeDate: "2026-08-03",
			LatestAdjDate:   "2026-08-01",
			DailyRows:       500,
			AdjRows:         502,
		},
		{
			TsCode:      "510500.SH",
			Name:        "中证500ETF",
			SyncEnabled: false,
		},
	}}, now)

	for _, want := range []string{
		"ETF 日线同步日报",
		"沪深300ETF", "510300.SH",
		"行情截至 2026-08-03", "因子截至 2026-08-01",
		"行情 500 / 因子 502",
		"中证500ETF", "已停用",
		"行情截至 -", // 空日期兜底
		"生成时间：2026-08-04 18:00:00",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report missing %q:\n%s", want, md)
		}
	}
}

func TestRenderStatusReportEmpty(t *testing.T) {
	md := RenderStatusReport(&StatusResp{}, time.Now())
	if !strings.Contains(md, "暂无标的") {
		t.Errorf("expected 暂无标的, got:\n%s", md)
	}
	if !strings.Contains(RenderStatusReport(nil, time.Now()), "暂无标的") {
		t.Error("nil StatusResp should render 暂无标的")
	}
}

func TestRenderSyncSummary(t *testing.T) {
	md := RenderSyncSummary(&SyncResp{
		Total:   1,
		Success: 1,
		Results: []SyncResultItem{
			{TsCode: "510300.SH", Upserted: 4, DailyUpserted: 3, AdjUpserted: 1},
		},
	})
	for _, want := range []string{"合计 1 只，成功 1 只", "510300.SH", "写入 4 行（行情 3 / 因子 1）", "ok"} {
		if !strings.Contains(md, want) {
			t.Errorf("summary missing %q:\n%s", want, md)
		}
	}
}

func TestRenderSyncSummaryNil(t *testing.T) {
	if !strings.Contains(RenderSyncSummary(nil), "未返回结果") {
		t.Error("nil SyncResp should render 未返回结果")
	}
}
