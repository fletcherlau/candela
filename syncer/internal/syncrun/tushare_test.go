package syncrun

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tushare "github.com/fletcherlau/go-tushare"
)

// Fixture implements the source protocol, including per-call truncation and a
// full daily calendar. No real Tushare credentials or production data are used.
type sourceFixture struct {
	mu             sync.Mutex
	bars           []Bar
	cap            int
	code           int
	message        string
	gap            string
	empty          bool
	malformed      bool
	truncatedEmpty bool
	calls          int
	hook           func(string, string)
}

func number(value float64) *float64 { return &value }

func fixtureBars(start, end string) []Bar {
	var bars []Bar
	for d := date(start); !d.After(date(end)); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		bars = append(bars, Bar{Code: Code, Date: d.Format("20060102"), Open: 100, Close: 101, High: 102, Low: 99, PreClose: number(100), Change: number(1), PctChange: number(1), Volume: number(1000), Amount: number(10000)})
	}
	return bars
}
func (f *sourceFixture) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		API    string         `json:"api_name"`
		Params map[string]any `json:"params"`
		Fields string         `json:"fields"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		w.WriteHeader(400)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	from, _ := req.Params["start_date"].(string)
	to, _ := req.Params["end_date"].(string)
	if f.hook != nil {
		f.hook(from, to)
	}
	if f.code != 0 {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": f.code, "msg": f.message})
		return
	}
	fields := strings.Split(req.Fields, ",")
	items := [][]any{}
	hasMore := false
	if req.API == "index_daily" {
		for i := len(f.bars) - 1; i >= 0; i-- {
			b := f.bars[i]
			if b.Date < from || b.Date > to || b.Date == f.gap || f.empty {
				continue
			}
			items = append(items, []any{b.Code, b.Date, b.Open, b.High, b.Low, b.Close, b.PreClose, b.Change, b.PctChange, b.Volume, b.Amount})
		}
		cap := pageLimit
		if f.cap > 0 {
			cap = f.cap
		}
		if len(items) > cap {
			items = items[:cap]
			hasMore = f.cap > 0
		} // no flag at advertised limit exercises length detection
		if f.truncatedEmpty {
			items = [][]any{}
			hasMore = true
		}
	} else if req.API == "trade_cal" {
		for d := date(from); !d.After(date(to)); d = d.AddDate(0, 0, 1) {
			open := 1
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				open = 0
			}
			items = append(items, []any{d.Format("20060102"), open})
		}
	} else {
		w.WriteHeader(400)
		return
	}
	if f.malformed {
		fields = []string{"ts_code"}
		items = [][]any{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"fields": fields, "items": items, "has_more": hasMore}})
}
func sourceFor(t *testing.T, f *sourceFixture) *TushareSource {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	return &TushareSource{Client: tushare.NewClient("fixture-only", tushare.WithHTTPURL(server.URL), tushare.WithMinInterval(0), tushare.WithRetries(0))}
}
func TestSourceDiscoversHistoryBefore2010AndSplitsTruncation(t *testing.T) {
	f := &sourceFixture{bars: fixtureBars("20041231", "20070108"), cap: 40}
	src := sourceFor(t, f)
	first, evidence, err := src.Earliest(context.Background(), "20070108")
	if err != nil || first != "20041231" || !strings.Contains(evidence, "20041230") {
		t.Fatalf("history %s %s %v", first, evidence, err)
	}
	bars, err := src.Window(context.Background(), "20041231", "20051231")
	if err != nil || len(bars) != 261 {
		t.Fatalf("split rows=%d err=%v", len(bars), err)
	}
}
func TestSourceFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fixture  sourceFixture
		expected string
	}{
		{name: "permission", fixture: sourceFixture{code: 2002, message: "权限不足 fixture-secret"}, expected: "permission_denied"},
		{name: "rate", fixture: sourceFixture{code: 40203, message: "rate fixture-secret"}, expected: "rate_limited"},
		{name: "malformed", fixture: sourceFixture{malformed: true}, expected: "source_invalid"},
		{name: "empty history", fixture: sourceFixture{empty: true}, expected: "source_empty"},
		{name: "truncated empty", fixture: sourceFixture{truncatedEmpty: true}, expected: "source_truncated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := sourceFor(t, &tc.fixture)
			_, _, err := src.Earliest(context.Background(), "20240909")
			var e *SourceError
			if !errors.As(err, &e) || e.Code != tc.expected || strings.Contains(e.Message, "fixture-secret") {
				t.Fatalf("%+v", err)
			}
		})
	}
}
func TestSourceRejectsMissingTradingDayButAllowsClosedDays(t *testing.T) {
	f := &sourceFixture{bars: fixtureBars("20240902", "20240908"), gap: "20240904"}
	src := sourceFor(t, f)
	_, err := src.Window(context.Background(), "20240902", "20240908")
	var e *SourceError
	if !errors.As(err, &e) || e.Code != "source_gap" {
		t.Fatalf("gap %v", err)
	}
	bars, err := src.Window(context.Background(), "20240907", "20240908")
	if err != nil || len(bars) != 0 {
		t.Fatalf("closed days %v", err)
	}
}
func TestCutoffFreezesShanghaiBoundary(t *testing.T) {
	for _, tc := range []struct{ now, want string }{{"2026-09-15T09:59:59Z", "20260914"}, {"2026-09-15T10:00:00Z", "20260915"}} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		if got := Cutoff(now); got != tc.want {
			t.Fatalf("%s != %s", got, tc.want)
		}
	}
}

func TestSourceRejectsMissingDataStructure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"code":0,"data":null}`)) }))
	defer server.Close()
	src := TushareSource{Client: tushare.NewClient("fixture-only", tushare.WithHTTPURL(server.URL), tushare.WithRetries(0))}
	_, _, err := src.Earliest(context.Background(), "20240909")
	var e *SourceError
	if !errors.As(err, &e) || e.Code != "source_invalid" {
		t.Fatalf("missing data: %v", err)
	}
}
