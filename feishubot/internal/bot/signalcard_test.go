package bot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

func signalFixture() *SignalResp {
	return &SignalResp{
		TradeDate:    "20260805",
		SnapshotDate: "20260805",
		TradingDay:   true,
		Cards: []SignalCardItem{
			{TsCode: "518880.SH", Name: "华安易富黄金ETF", Score: f64(-0.023456), YZVol: f64(0.15234), Quantile: f64(61.25), Weight: 1, Rank: 3},
			{TsCode: "510880.SH", Name: "华泰柏瑞上证红利ETF", Score: f64(0.123456), YZVol: f64(0.08123), Quantile: f64(45.5), Weight: 1, Rank: 1},
			{TsCode: "513100.SH", Name: "国泰纳斯达克100ETF(QDII)", Score: f64(0.056789), YZVol: f64(0.21567), Quantile: f64(82.4), Weight: 0.752, Rank: 2},
			{TsCode: "159915.SZ", Name: "易方达创业板ETF", Score: nil, YZVol: nil, Quantile: nil, Weight: 1, Rank: 0, Message: "历史不足 20 点，无法打分"},
		},
		Advice: []AdviceItem{
			{Scenario: "现金", Action: "买入 510880.SH", TargetWeight: f64(1), Note: "买入第一名，仓位 1.00；空仓部分买货币 ETF"},
			{Scenario: "510880.SH", Action: "持有", TargetWeight: f64(1), Note: "不换仓；实际仓位偏离目标仓位 ≥ 5 个百分点时，一笔微调到目标仓位（货基反向同调）"},
			{Scenario: "513100.SH", Action: "换入 510880.SH", TargetWeight: f64(1), Note: "得分差距超过缓冲，换入第一名；卖出旧仓，空仓部分买货币 ETF"},
			{Scenario: "518880.SH", Action: "换入 510880.SH", TargetWeight: f64(1), Note: "安全阀：持仓得分 < 0，无视差距换入第一名；卖出旧仓，空仓部分买货币 ETF"},
			{Scenario: "159915.SZ", Action: "持有", TargetWeight: nil, Note: "159915.SZ 数据不足（score 缺失），当日不参与打分；维持当前持仓与仓位不动"},
		},
	}
}

// parseSignalCard 解析卡片 JSON，返回卡片与 body elements。
func parseSignalCard(t *testing.T, js string) (map[string]any, []any) {
	t.Helper()
	var card map[string]any
	if err := json.Unmarshal([]byte(js), &card); err != nil {
		t.Fatalf("卡片 JSON 非法: %v\n%s", err, js)
	}
	body, ok := card["body"].(map[string]any)
	if !ok {
		t.Fatalf("卡片缺 body: %s", js)
	}
	elements, ok := body["elements"].([]any)
	if !ok {
		t.Fatalf("卡片缺 body.elements: %s", js)
	}
	return card, elements
}

// cardTables 取全部 table 组件。
func cardTables(t *testing.T, elements []any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, el := range elements {
		if m, ok := el.(map[string]any); ok && m["tag"] == "table" {
			out = append(out, m)
		}
	}
	return out
}

// colDisplayNames 取表格的表头列名。
func colDisplayNames(t *testing.T, table map[string]any) []string {
	t.Helper()
	cols, ok := table["columns"].([]any)
	if !ok {
		t.Fatalf("表格缺 columns: %v", table)
	}
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.(map[string]any)["display_name"].(string))
	}
	return out
}

// tableRows 取表格行数据。
func tableRows(t *testing.T, table map[string]any) []map[string]any {
	t.Helper()
	rows, ok := table["rows"].([]any)
	if !ok {
		t.Fatalf("表格缺 rows: %v", table)
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.(map[string]any))
	}
	return out
}

// markdownTexts 收集全部 markdown 组件的 content。
func markdownTexts(elements []any) []string {
	var out []string
	for _, el := range elements {
		if m, ok := el.(map[string]any); ok && m["tag"] == "markdown" {
			out = append(out, m["content"].(string))
		}
	}
	return out
}

// anyTextContains 报告任一文本是否包含子串。
func anyTextContains(texts []string, sub string) bool {
	for _, s := range texts {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildSignalCardJSON(t *testing.T) {
	now := time.Date(2026, 8, 5, 14, 45, 0, 0, time.Local)
	js := BuildSignalCardJSON(signalFixture(), now)
	card, elements := parseSignalCard(t, js)

	// 2.0 schema 卡片骨架。
	if card["schema"] != "2.0" {
		t.Errorf("schema = %v，期望 2.0", card["schema"])
	}
	header, ok := card["header"].(map[string]any)
	if !ok || header["title"].(map[string]any)["content"] != "信号卡片" {
		t.Errorf("卡片标题头异常: %v", card["header"])
	}

	// 恰好两张表：信号表 + 建议表。
	tables := cardTables(t, elements)
	if len(tables) != 2 {
		t.Fatalf("表格数量 = %d，期望 2", len(tables))
	}

	// 信号表：表头注明列含义。
	if got := colDisplayNames(t, tables[0]); !equalStrings(got, []string{"排名", "标的", "动量得分 score", "年化波动 σ_YZ", "波动分位 q", "目标仓位 w(q)"}) {
		t.Errorf("信号表表头 = %v", got)
	}
	rows := tableRows(t, tables[0])
	if len(rows) != 4 {
		t.Fatalf("信号表行数 = %d，期望 4", len(rows))
	}
	// 按名次排序：rank 1 → 2 → 3，无法打分（rank 0）垫底。
	wantCodes := []string{"510880.SH", "513100.SH", "518880.SH", "159915.SZ"}
	for i, code := range wantCodes {
		if !strings.Contains(rows[i]["instrument"].(string), code) {
			t.Errorf("信号表第 %d 行应为 %s: %v", i, code, rows[i])
		}
	}
	// rank 1 粗体高亮，数值格式：score 4 位小数，σ_YZ / q 百分号，w(q) 2 位小数。
	r0 := rows[0]
	if r0["rank"] != "1" || !strings.HasPrefix(r0["instrument"].(string), "**") {
		t.Errorf("rank 1 行应粗体高亮: %v", r0)
	}
	if r0["score"] != "0.1235" || r0["yzvol"] != "8.12%" || r0["quantile"] != "45.50%" || r0["weight"] != "1.00" {
		t.Errorf("rank 1 行数值格式异常: %v", r0)
	}
	if rows[1]["weight"] != "0.75" {
		t.Errorf("w(q) 应保留 2 位小数: %v", rows[1])
	}
	if rows[2]["score"] != "-0.0235" || rows[2]["yzvol"] != "15.23%" {
		t.Errorf("负得分行数值格式异常: %v", rows[2])
	}
	// 无法打分的标的：缺失值渲染为 -，名次为 —。
	r3 := rows[3]
	if r3["rank"] != "—" || r3["score"] != "-" || r3["yzvol"] != "-" || r3["quantile"] != "-" {
		t.Errorf("数据不足行应渲染为 -: %v", r3)
	}

	// 建议表：3 列 5 行（现金 + 四标的）。
	if got := colDisplayNames(t, tables[1]); !equalStrings(got, []string{"若你当前持有", "操作建议", "目标仓位"}) {
		t.Errorf("建议表表头 = %v", got)
	}
	advice := tableRows(t, tables[1])
	if len(advice) != 5 {
		t.Fatalf("建议表行数 = %d，期望 5", len(advice))
	}
	if advice[0]["scenario"] != "现金" || !strings.Contains(advice[0]["action"].(string), "买入 510880.SH") || advice[0]["weight"] != "1.00" {
		t.Errorf("现金情形行异常: %v", advice[0])
	}
	// 情形带标的名称；安全阀/数据不足以标签附在建议后。
	var hold518, hold159 map[string]any
	for _, r := range advice {
		switch {
		case strings.Contains(r["scenario"].(string), "518880.SH"):
			hold518 = r
		case strings.Contains(r["scenario"].(string), "159915.SZ"):
			hold159 = r
		}
	}
	if hold518 == nil || !strings.Contains(hold518["scenario"].(string), "华安易富黄金ETF") ||
		!strings.Contains(hold518["action"].(string), "换入 510880.SH") || !strings.Contains(hold518["action"].(string), "安全阀") {
		t.Errorf("安全阀情形行异常: %v", hold518)
	}
	if hold159 == nil || hold159["weight"] != "—" || !strings.Contains(hold159["action"].(string), "数据不足") {
		t.Errorf("数据不足情形行应目标仓位 —: %v", hold159)
	}

	// 文本元素：数据时间/生成时间页脚、打分缺失原因附注；无 stale 不出现告警与溢价提示。
	texts := markdownTexts(elements)
	if !anyTextContains(texts, "数据时间：2026-08-05（盘中快照）") {
		t.Errorf("缺数据时间: %v", texts)
	}
	if !anyTextContains(texts, "生成时间：2026-08-05 14:45:00") {
		t.Errorf("缺生成时间: %v", texts)
	}
	if !anyTextContains(texts, "历史不足 20 点，无法打分") {
		t.Errorf("缺打分缺失原因附注: %v", texts)
	}
	if anyTextContains(texts, "新鲜度告警") {
		t.Errorf("无 stale 标的不应告警: %v", texts)
	}
	if anyTextContains(texts, "溢价率") {
		t.Errorf("第一名非 513100 不应出现溢价提示: %v", texts)
	}
}

func TestBuildSignalCardJSONStaleWarning(t *testing.T) {
	resp := signalFixture()
	resp.Cards[0].Stale = true
	resp.Cards[2].Stale = true
	_, elements := parseSignalCard(t, BuildSignalCardJSON(resp, time.Now()))

	// 告警存在并点名 stale 标的。
	warnIdx := -1
	for i, el := range elements {
		if m, ok := el.(map[string]any); ok && m["tag"] == "markdown" &&
			strings.Contains(m["content"].(string), "新鲜度告警") {
			warnIdx = i
			for _, code := range []string{"518880.SH", "513100.SH"} {
				if !strings.Contains(m["content"].(string), code) {
					t.Errorf("告警应点名 %s: %v", code, m["content"])
				}
			}
		}
	}
	if warnIdx < 0 {
		t.Fatalf("stale 标的应触发告警: %v", elements)
	}
	// 告警置顶：在第一张表之前出现。
	firstTable := -1
	for i, el := range elements {
		if m, ok := el.(map[string]any); ok && m["tag"] == "table" {
			firstTable = i
			break
		}
	}
	if firstTable < 0 || warnIdx > firstTable {
		t.Errorf("告警应在表格上方: warnIdx=%d firstTable=%d", warnIdx, firstTable)
	}
}

func TestBuildSignalCardJSONPremiumNote(t *testing.T) {
	resp := signalFixture()
	// 第一名是 513100.SH：syncer 会在买入/换入建议的 note 里附加溢价确认。
	premium := "；下单前确认 513100 溢价率 ≤1%，否则顺延第二名（次日重算）"
	resp.Advice[0].Note += premium
	texts := markdownTexts(func() []any {
		_, elements := parseSignalCard(t, BuildSignalCardJSON(resp, time.Now()))
		return elements
	}())
	if !anyTextContains(texts, "溢价率 ≤1%") || !anyTextContains(texts, "顺延第二名") {
		t.Errorf("涉及买入 513100 时应出现醒目溢价提示: %v", texts)
	}
}

func TestBuildSignalCardJSONNil(t *testing.T) {
	_, elements := parseSignalCard(t, BuildSignalCardJSON(nil, time.Now()))
	if !anyTextContains(markdownTexts(elements), "未返回结果") {
		t.Errorf("nil SignalResp 应渲染 未返回结果: %v", elements)
	}
}

// --- 口径行（basisLine） ---

func TestBasisLine(t *testing.T) {
	realtime := signalFixture() // TradingDay=true，SnapshotDate=20260805
	realtime.Basis = "realtime"
	closeBasis := signalFixture()
	closeBasis.TradeDate = "20260808"
	closeBasis.SnapshotDate = "20260807"
	closeBasis.TradingDay = false
	closeBasis.Basis = "close"
	legacy := signalFixture() // Basis 空串（旧版 syncer）按 realtime 处理

	cases := []struct {
		name string
		resp *SignalResp
		now  time.Time
		want string
	}{
		{"盘中实时", realtime, time.Date(2026, 8, 5, 14, 45, 0, 0, beijingTZ), "口径：盘中实时"},
		{"15点整已收盘", realtime, time.Date(2026, 8, 5, 15, 0, 0, 0, beijingTZ), "口径：已收盘（实时接口，最新价=收盘）"},
		{"盘后已收盘", realtime, time.Date(2026, 8, 5, 20, 30, 0, 0, beijingTZ), "口径：已收盘（实时接口，最新价=收盘）"},
		{"非交易日收盘回退", closeBasis, time.Date(2026, 8, 8, 12, 0, 0, 0, beijingTZ), "口径：非交易日，最近交易日 2026-08-07 收盘数据"},
		{"旧版无basis字段盘中", legacy, time.Date(2026, 8, 5, 10, 0, 0, 0, beijingTZ), "口径：盘中实时"},
	}
	for _, c := range cases {
		if got := basisLine(c.resp, c.now); got != c.want {
			t.Errorf("%s: basisLine = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBuildSignalCardJSONCloseBasis(t *testing.T) {
	// 收盘回退口径：数据时间括号注为官方收盘，口径行点名最近交易日。
	resp := signalFixture()
	resp.TradeDate = "20260808"
	resp.SnapshotDate = "20260807"
	resp.TradingDay = false
	resp.Basis = "close"
	_, elements := parseSignalCard(t, BuildSignalCardJSON(resp, time.Date(2026, 8, 8, 12, 0, 0, 0, beijingTZ)))

	texts := markdownTexts(elements)
	if !anyTextContains(texts, "口径：非交易日，最近交易日 2026-08-07 收盘数据") {
		t.Errorf("缺收盘回退口径行: %v", texts)
	}
	if !anyTextContains(texts, "数据时间：2026-08-07（官方收盘）") {
		t.Errorf("数据时间应注明官方收盘: %v", texts)
	}
}
