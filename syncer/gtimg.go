package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"syncer/internal/core"
)

// gtimgDefaultBaseURL 是腾讯财经实时行情接口。
const gtimgDefaultBaseURL = "http://qt.gtimg.cn"

// gtimgSource 是 core.RealtimeSource 的生产实现：qt.gtimg.cn 的薄适配。
// 响应为 GBK 编码的 v_<code>="..." 赋值文本，但解析只依赖 ASCII 安全的
// 数字与 "~" 分隔符，因此按字节解析、不做 GBK 解码。
type gtimgSource struct {
	baseURL string
	client  *http.Client
}

// newGtimgSource 构造适配器；baseURL 传 "" 时用生产地址（测试注入 httptest 地址）。
func newGtimgSource(baseURL string) *gtimgSource {
	if baseURL == "" {
		baseURL = gtimgDefaultBaseURL
	}
	return &gtimgSource{baseURL: baseURL, client: &http.Client{Timeout: 10 * time.Second}}
}

func (s *gtimgSource) FetchRealtime(ctx context.Context, tsCodes []string) ([]core.RealtimeQuote, error) {
	codes := make([]string, 0, len(tsCodes))
	for _, c := range tsCodes {
		g, err := toGtimgCode(c)
		if err != nil {
			return nil, err
		}
		codes = append(codes, g)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/q="+strings.Join(codes, ","), nil)
	if err != nil {
		return nil, fmt.Errorf("构造 gtimg 请求失败: %v", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 gtimg 失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gtimg 返回非 200 状态: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 gtimg 响应失败: %v", err)
	}

	quotes := make([]core.RealtimeQuote, 0, len(tsCodes))
	for _, stmt := range strings.Split(string(body), "\n") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		q, err := parseGtimgStatement(stmt)
		if err != nil {
			return nil, fmt.Errorf("解析 gtimg 响应失败（请求标的 %v）: %v", tsCodes, err)
		}
		quotes = append(quotes, q)
	}
	if len(quotes) == 0 {
		return nil, fmt.Errorf("gtimg 响应中没有可解析的行情: %q", truncate(string(body), 80))
	}
	return quotes, nil
}

// toGtimgCode 把 Tushare ts_code（510880.SH）映射为 gtimg 代码（sh510880）。
func toGtimgCode(tsCode string) (string, error) {
	parts := strings.Split(tsCode, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("无法映射为 gtimg 代码的 ts_code %q（期望 <code>.<SH|SZ>）", tsCode)
	}
	switch strings.ToUpper(parts[1]) {
	case "SH":
		return "sh" + parts[0], nil
	case "SZ":
		return "sz" + parts[0], nil
	}
	return "", fmt.Errorf("无法映射为 gtimg 代码的 ts_code %q（未知市场后缀）", tsCode)
}

// gtimg 响应字段位置（"~" 分隔，0 起）。只取信号计算所需字段。
const (
	gtimgFieldLatest   = 3  // 最新价
	gtimgFieldOpen     = 5  // 今开
	gtimgFieldDatetime = 30 // 行情时间戳 yyyyMMddHHmmss
	gtimgFieldHigh     = 33 // 最高
	gtimgFieldLow      = 34 // 最低
	gtimgMinFields     = 35
)

// parseGtimgStatement 解析一行 v_sh510880="..."; 赋值语句。
func parseGtimgStatement(stmt string) (core.RealtimeQuote, error) {
	var q core.RealtimeQuote

	eq := strings.Index(stmt, `="`)
	if !strings.HasPrefix(stmt, "v_") || eq < 0 {
		return q, fmt.Errorf("无法识别的 gtimg 语句: %q", truncate(stmt, 80))
	}
	marketCode := stmt[2:eq]
	payload := stmt[eq+2:]
	payload = strings.TrimSuffix(strings.TrimSpace(payload), ";")
	payload = strings.TrimSuffix(payload, `"`)

	fields := strings.Split(payload, "~")
	if len(fields) < gtimgMinFields {
		return q, fmt.Errorf("gtimg 行情字段数不足（%s 共 %d 个，期望 ≥%d）", marketCode, len(fields), gtimgMinFields)
	}

	tsCode, err := fromGtimgCode(marketCode)
	if err != nil {
		return q, err
	}

	var errs []string
	if q.Latest, err = parseGtimgFloat(fields[gtimgFieldLatest]); err != nil {
		errs = append(errs, "最新价")
	}
	if q.Open, err = parseGtimgFloat(fields[gtimgFieldOpen]); err != nil {
		errs = append(errs, "今开")
	}
	if q.High, err = parseGtimgFloat(fields[gtimgFieldHigh]); err != nil {
		errs = append(errs, "最高")
	}
	if q.Low, err = parseGtimgFloat(fields[gtimgFieldLow]); err != nil {
		errs = append(errs, "最低")
	}
	if len(errs) > 0 {
		return q, fmt.Errorf("gtimg 行情数值解析失败（%s: %s）", tsCode, strings.Join(errs, "/"))
	}

	dt := fields[gtimgFieldDatetime]
	if len(dt) != 14 {
		return q, fmt.Errorf("gtimg 行情时间戳格式非法（%s: %q，期望 yyyyMMddHHmmss）", tsCode, dt)
	}
	q.TsCode = tsCode
	q.TradeDate = dt[:8]
	return q, nil
}

// fromGtimgCode 把 gtimg 变量名中的代码（sh510880）映射回 ts_code（510880.SH）。
func fromGtimgCode(marketCode string) (string, error) {
	if len(marketCode) < 3 {
		return "", fmt.Errorf("无法识别的 gtimg 代码 %q", marketCode)
	}
	switch marketCode[:2] {
	case "sh":
		return marketCode[2:] + ".SH", nil
	case "sz":
		return marketCode[2:] + ".SZ", nil
	}
	return "", fmt.Errorf("无法识别的 gtimg 市场前缀 %q", marketCode)
}

func parseGtimgFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
