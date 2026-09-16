package syncrun

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	tushare "github.com/fletcherlau/go-tushare"
)

// One public index_daily record captured on 2026-09-16. The initial index value
// has OHLC, but no previous close, changes, or turnover. No credentials in fixture.
func inceptionSource(t *testing.T) *TushareSource {
	t.Helper()
	payload, err := os.ReadFile("testdata/index-daily-inception.json")
	if err != nil {
		t.Fatal(err)
	}
	var original tushare.Response
	if err = json.Unmarshal(payload, &original); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			API    string         `json:"api_name"`
			Params map[string]any `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		fields := original.Data.Fields
		items := original.Data.Items
		if req.API == "trade_cal" {
			fields = []string{"cal_date", "is_open"}
			items = [][]any{{"20041231", 1}}
		} else if req.Params["end_date"].(string) < "20041231" {
			items = [][]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"fields": fields, "items": items, "has_more": false}})
	}))
	t.Cleanup(server.Close)
	return &TushareSource{Client: tushare.NewClient("fixture-only", tushare.WithHTTPURL(server.URL), tushare.WithMinInterval(0), tushare.WithRetries(0))}
}
func TestIndexInceptionRowIsValidHistory(t *testing.T) {
	src := inceptionSource(t)
	first, _, err := src.Earliest(context.Background(), "20260915")
	if err != nil || first != "20041231" {
		t.Fatalf("valid inception record rejected: first=%q error=%v", first, err)
	}
	bars, err := src.Window(context.Background(), "20041231", "20041231")
	if err != nil || len(bars) != 1 {
		t.Fatalf("valid inception window rejected: rows=%d error=%v", len(bars), err)
	}
}

func TestIndexInceptionNullMetricsArePreserved(t *testing.T) {
	bars, err := inceptionSource(t).Window(context.Background(), "20041231", "20041231")
	if err != nil {
		t.Fatal(err)
	}
	b := bars[0]
	if b.Open != 1000 || b.High != 1000 || b.Low != 1000 || b.Close != 1000 || b.PreClose != nil || b.Change != nil || b.PctChange != nil || b.Volume != nil || b.Amount != nil {
		t.Fatalf("inception value changed: %+v", b)
	}
}

func TestMissingRequiredSourceValuesStillFail(t *testing.T) {
	for _, api := range []string{"index_daily", "trade_cal"} {
		fields := []string{"ts_code", "trade_date", "open", "high", "low", "close"}
		values := []any{Code, "20041231", 1000, 1000, 1000, 1000}
		if api == "trade_cal" {
			fields = []string{"cal_date", "is_open"}
			values = []any{"20041231", 1}
		}
		for i, field := range fields {
			t.Run(api+"/"+field, func(t *testing.T) {
				missing := append([]any(nil), values...)
				missing[i] = nil
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"fields": fields, "items": [][]any{missing}}})
				}))
				defer server.Close()
				src := TushareSource{Client: tushare.NewClient("fixture-only", tushare.WithHTTPURL(server.URL), tushare.WithRetries(0))}
				_, err := src.query(context.Background(), api, map[string]any{}, strings.Join(fields, ","))
				var sourceErr *SourceError
				if !errors.As(err, &sourceErr) || sourceErr.Code != "source_invalid" || !strings.Contains(sourceErr.Message, api+"."+field) {
					t.Fatalf("required field accepted or unidentified: %v", err)
				}
			})
		}
	}
}
