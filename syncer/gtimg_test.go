package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 真实 gtimg 响应样本（2026-08-05 收盘后抓取，GBK 响应中的中文字段名已略去，
// 解析只依赖 ASCII 安全的数字与分隔符）。
const gtimgSample = `v_sh510880="1~X~510880~3.228~3.237~3.228~2641863~1199080~1442783~3.228~650~3.227~5867~3.226~1505~3.225~3893~3.224~41220~3.229~2325~3.230~29627~3.231~2899~3.232~2345~3.233~1036~~20260805161458~-0.009~-0.28~3.238~3.205~3.228/2641863/850400046~2641863~85040~3.89~~~3.238~3.205~1.02~219.35~219.35~0.00~3.561~2.913~0.99~14903~3.219~~~~~~85040.0046~30.4400~943~   A~ETF~6.18~-1.37~~~~3.408~2.896~-0.77~4.81~-0.89~6795175700~6795175700~16.31~1.25~6795175700~-0.00~3.2281~3.76~0.03~3.2355~CNY~0~___D__F__N~3.238~-13642~";
v_sz159915="51~X~159915~3.560~3.514~3.400~30236017~14159220~16076796~3.559~3859~3.558~10377~3.557~6679~3.556~5927~3.555~3793~3.560~39994~3.561~2683~3.562~2201~3.563~1643~3.564~1385~~20260805161436~0.046~1.31~3.607~3.400~3.560/30236017/10611238477~30236017~1061124~15.58~~~3.607~3.400~5.89~690.80~690.80~0.00~4.217~2.811~0.84~-17271~3.509~~~~~~1061123.8477~327.7336~9206~   A~ETF~11.74~4.64~~~~4.395~2.300~-0.73~-7.94~-9.64~19404454936~19404454936~-21.99~11.77~19404454936~-0.07~3.5625~53.45~0.08~3.5124~CNY~0~~3.551~7415~";
`

func TestFetchRealtimeParsesQuotes(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.RequestURI
		w.Write([]byte(gtimgSample))
	}))
	defer srv.Close()

	s := newGtimgSource(srv.URL)
	quotes, err := s.FetchRealtime(context.Background(), []string{"510880.SH", "159915.SZ"})
	if err != nil {
		t.Fatalf("FetchRealtime: %v", err)
	}

	// ts_code 映射为 gtimg 代码：510880.SH → sh510880，159915.SZ → sz159915
	// （gtimg 的 "q=..." 是路径的一部分，不是查询串）
	if gotQuery != "/q=sh510880,sz159915" {
		t.Errorf("RequestURI = %q, want %q", gotQuery, "/q=sh510880,sz159915")
	}

	if len(quotes) != 2 {
		t.Fatalf("len(quotes) = %d, want 2", len(quotes))
	}

	sh := quotes[0]
	if sh.TsCode != "510880.SH" {
		t.Errorf("TsCode = %q, want 510880.SH", sh.TsCode)
	}
	if sh.TradeDate != "20260805" {
		t.Errorf("TradeDate = %q, want 20260805（取自行情时间戳 20260805161458）", sh.TradeDate)
	}
	if sh.Open != 3.228 || sh.High != 3.238 || sh.Low != 3.205 || sh.Latest != 3.228 {
		t.Errorf("sh510880 OHLC/Latest = %v/%v/%v/%v, want 3.228/3.238/3.205/3.228",
			sh.Open, sh.High, sh.Low, sh.Latest)
	}

	sz := quotes[1]
	if sz.TsCode != "159915.SZ" {
		t.Errorf("TsCode = %q, want 159915.SZ", sz.TsCode)
	}
	if sz.Open != 3.400 || sz.High != 3.607 || sz.Low != 3.400 || sz.Latest != 3.560 {
		t.Errorf("sz159915 OHLC/Latest = %v/%v/%v/%v, want 3.400/3.607/3.400/3.560",
			sz.Open, sz.High, sz.Low, sz.Latest)
	}
}

func TestFetchRealtimeMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("this is not a gtimg response"))
	}))
	defer srv.Close()

	s := newGtimgSource(srv.URL)
	_, err := s.FetchRealtime(context.Background(), []string{"510880.SH"})
	if err == nil {
		t.Fatal("expected error for malformed response, got nil")
	}
	if !strings.Contains(err.Error(), "510880") {
		t.Errorf("error should mention the instrument, got: %v", err)
	}
}

func TestFetchRealtimeTruncatedFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`v_sh510880="1~X~510880~3.228~3.237";`))
	}))
	defer srv.Close()

	s := newGtimgSource(srv.URL)
	_, err := s.FetchRealtime(context.Background(), []string{"510880.SH"})
	if err == nil {
		t.Fatal("expected error for truncated fields, got nil")
	}
}

func TestFetchRealtimeRejectsUnsupportedTsCode(t *testing.T) {
	s := newGtimgSource("http://example.invalid")
	_, err := s.FetchRealtime(context.Background(), []string{"510880"})
	if err == nil {
		t.Fatal("expected error for ts_code without market suffix, got nil")
	}
}
