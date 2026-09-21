package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"syncer/internal/core"
	"syncer/internal/schema"
	"syncer/internal/store"
)

func dailyDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ROTATION_DAILY_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated MySQL required")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.DBName != "candela_daily_test" {
		t.Fatal("only isolated candela_daily_test allowed")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = schema.Ensure(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE etf_sync_object SET active_run=NULL"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"etf_sync_batch", "etf_sync_run", "rotation_capture_run", "rotation_daily", "rotation_coverage", "rotation_calendar", "etf_daily", "etf_adj_factor"} {
		if _, err = db.Exec("DELETE FROM " + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("UPDATE rotation_result SET status='pending',message='',payload=NULL,revision=revision+1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDailyClosePublishesFourMetricsGroupsThroughHTTP(t *testing.T) {
	db := dailyDatabase(t)
	ctx := context.Background()
	now := func() time.Time { return time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC) }
	svc := &Service{DB: db, Calendar: fixtureCalendar{}, Now: now}
	read := func() DailyView {
		t.Helper()
		w := httptest.NewRecorder()
		svc.DailyHandler().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/rotation/daily?tradeDate=20250102", nil))
		if w.Code != 200 {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
		var v DailyView
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := read(); v.Close != nil || v.Status != "unavailable" {
		t.Fatalf("empty: %+v", v)
	}
	st := store.NewMySQLStore(db)
	for i, code := range core.RotationCodes {
		bars := []core.Bar{}
		for d := day("20180101"); !d.After(day("20250102")); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			price := float64(100 + i)
			bars = append(bars, core.Bar{TsCode: code, TradeDate: d.Format("20060102"), Open: price, High: price, Low: price, Close: price})
		}
		// Later stored data must not push an old date outside its scoring window.
		bars = append(bars, core.Bar{TsCode: code, TradeDate: "20300101", Open: 900, High: 900, Low: 900, Close: 900})
		if _, err := st.UpsertDaily(ctx, bars); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertAdjFactors(ctx, []core.AdjFactor{{TsCode: code, TradeDate: "20171229", AdjFactor: 1}, {TsCode: code, TradeDate: "20300101", AdjFactor: 9}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO rotation_coverage VALUES (?,?)", code, "20250102"); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.PublishClose(ctx, "20250102"); err != nil {
		t.Fatal(err)
	}
	v := read()
	if v.Status != "ready" || v.Close == nil || len(v.Close.Cards) != 4 || v.Close.Available != 4 {
		t.Fatalf("published: %+v", v)
	}
	for i, c := range v.Close.Cards {
		if c.Code != core.RotationCodes[i] || c.Price == nil || *c.Price != float64(100+i) || c.Score == nil || *c.Score != 0 || c.Volatility == nil || *c.Volatility != 0 || c.Quantile == nil || *c.Quantile != 50 || c.Weight == nil || *c.Weight != 1 || c.Rank == nil {
			t.Fatalf("flat-price known result: %+v", c)
		}
	}
	if v.Reference != nil || v.ReferenceStatus != "missing" {
		t.Fatal("invented 14:45 reference")
	}
	stamp := v.Close.PublishedAt
	if err := svc.PublishClose(ctx, "20250102"); err != nil {
		t.Fatal(err)
	}
	if read().Close.PublishedAt != stamp {
		t.Fatal("unchanged input republished")
	}
}

func TestDailyCloseDoesNotFillUnknownHistoricalGaps(t *testing.T) {
	db := dailyDatabase(t)
	ctx := context.Background()
	svc := &Service{DB: db, Calendar: fixtureCalendar{}, Now: func() time.Time { return day("20250103") }}
	st := store.NewMySQLStore(db)
	for i, code := range core.RotationCodes {
		var bars []core.Bar
		for d := day("20241101"); !d.After(day("20250102")); d = d.AddDate(0, 0, 1) {
			date := d.Format("20060102")
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday || (i == 0 && date == "20241220") {
				continue
			}
			bars = append(bars, core.Bar{TsCode: code, TradeDate: date, Open: 100, High: 100, Low: 100, Close: 100})
		}
		if _, err := st.UpsertDaily(ctx, bars); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertAdjFactors(ctx, []core.AdjFactor{{TsCode: code, TradeDate: "20241031", AdjFactor: 1}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO rotation_coverage VALUES (?,?)", code, "20250102"); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.PublishClose(ctx, "20250102"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	svc.DailyHandler().ServeHTTP(w, httptest.NewRequest("GET", "/?tradeDate=20250102", nil))
	var v DailyView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Close == nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if v.Close.Cards[0].Score != nil || v.Close.Cards[0].Reasons["score"] == "" {
		t.Fatal("unknown historical gap presented as a valid score")
	}
	for _, card := range v.Close.Cards {
		if card.Rank != nil {
			t.Fatal("rank published with incomplete momentum")
		}
		if card.Weight != nil || card.Quantile != nil {
			t.Fatal("short history became an invented quantile or 100% weight")
		}
	}
	if v.Close.Cards[1].Score == nil || *v.Close.Cards[1].Score != 0 {
		t.Fatal("other complete input unnecessarily hidden")
	}
}

func TestDailyWorkerPublishesOnlyCompleteCloseGroup(t *testing.T) {
	db := dailyDatabase(t)
	ctx := context.Background()
	svc := &Service{DB: db, Calendar: fixtureCalendar{}, Now: func() time.Time { return day("20250103") }}
	st := store.NewMySQLStore(db)
	for i, code := range core.RotationCodes {
		if i < 3 {
			if _, err := st.UpsertDaily(ctx, []core.Bar{{TsCode: code, TradeDate: "20250102", Open: 100, High: 100, Low: 100, Close: 100}}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := st.UpsertAdjFactors(ctx, []core.AdjFactor{{TsCode: code, TradeDate: "20250102", AdjFactor: 1}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO rotation_coverage VALUES (?,?)", code, "20250102"); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(svc.DailyHandler())
	defer server.Close()
	read := func() DailyView {
		t.Helper()
		res, err := server.Client().Get(server.URL + "?tradeDate=20250102")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v DailyView
		if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 {
			t.Fatal(res.StatusCode)
		}
		return v
	}
	if err := svc.RefreshDaily(ctx); err != nil {
		t.Fatal(err)
	}
	if v := read(); v.Status != "pending" || v.Available != 3 || v.Close != nil || v.TradeDate != "20250102" || !strings.Contains(v.Message, "513100.SH") || !strings.Contains(v.Message, "未到齐") {
		t.Fatalf("partial data published: %+v", v)
	}
	if _, err := st.UpsertDaily(ctx, []core.Bar{{TsCode: core.RotationCodes[3], TradeDate: "20250102", Open: 100, High: 100, Low: 100, Close: 100}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshDaily(ctx); err != nil {
		t.Fatal(err)
	}
	v := read()
	if v.Status != "ready" || v.Available != 4 || v.Close == nil {
		t.Fatalf("completion not published: %+v", v)
	}
	for _, c := range v.Close.Cards {
		if c.Price == nil || *c.Price != 100 || c.Score != nil || c.Rank != nil || c.Weight != nil || c.Reasons["score"] == "" {
			t.Fatalf("short history: %+v", c)
		}
	}
}

type failedDailyCalendar struct{}

func (failedDailyCalendar) Calendar(context.Context, string, string) (map[string]bool, error) {
	return nil, fmt.Errorf("provider-token-secret")
}

func TestDailyFailedCalculationKeepsPublishedResultAndCanRecover(t *testing.T) {
	db := dailyDatabase(t)
	ctx := context.Background()
	svc := &Service{DB: db, Calendar: fixtureCalendar{}, Now: func() time.Time { return day("20250103") }}
	st := store.NewMySQLStore(db)
	for _, code := range core.RotationCodes {
		if _, err := st.UpsertDaily(ctx, []core.Bar{{TsCode: code, TradeDate: "20250102", Open: 100, High: 100, Low: 100, Close: 100}}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertAdjFactors(ctx, []core.AdjFactor{{TsCode: code, TradeDate: "20250102", AdjFactor: 1}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO rotation_coverage VALUES (?,?)", code, "20250102"); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.PublishClose(ctx, "20250102"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertDaily(ctx, []core.Bar{{TsCode: core.RotationCodes[0], TradeDate: "20250102", Open: 101, High: 101, Low: 101, Close: 101}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM rotation_calendar"); err != nil {
		t.Fatal(err)
	}
	svc.Calendar = failedDailyCalendar{}
	if err := svc.PublishClose(ctx, "20250102"); err == nil {
		t.Fatal("expected source failure")
	}
	w := httptest.NewRecorder()
	svc.DailyHandler().ServeHTTP(w, httptest.NewRequest("GET", "/?tradeDate=20250102", nil))
	var v DailyView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Status != "failed" || v.Close == nil || *v.Close.Cards[0].Price != 100 || strings.Contains(w.Body.String(), "provider-token-secret") {
		t.Fatalf("failure not reported safely: %d %s", w.Code, w.Body.String())
	}
	svc.Calendar = fixtureCalendar{}
	if err := svc.PublishClose(ctx, "20250102"); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	svc.DailyHandler().ServeHTTP(w, httptest.NewRequest("GET", "/?tradeDate=20250102", nil))
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Status != "ready" || v.Close == nil || *v.Close.Cards[0].Price != 101 {
		t.Fatalf("same revision recovery: %s", w.Body.String())
	}
}

func TestDailyReadRejectsInvalidQueriesWithoutFetching(t *testing.T) {
	db := dailyDatabase(t)
	svc := &Service{DB: db, Calendar: failedDailyCalendar{}, Now: func() time.Time { return day("20250103") }}
	server := httptest.NewServer(svc.DailyHandler())
	defer server.Close()
	for _, query := range []string{"tradeDate=20250104", "tradeDate=20250230", "tradeDate=", "tradeDate=20250102&tradeDate=20250101", "other=x", "tradeDate=%XX", "tradeDate=20250102;bad=1"} {
		res, err := server.Client().Get(server.URL + "?" + query)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Errorf("%s: got %d, want 400", query, res.StatusCode)
		}
	}
	res, err := server.Client().Get(server.URL + "?tradeDate=20250102")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var v DailyView
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || v.Status != "unavailable" {
		t.Fatalf("read fetched or wrote data: %+v", v)
	}
}

func TestDailySupersededCalculationCannotReplacePublishedGroup(t *testing.T) {
	db := dailyDatabase(t)
	ctx := context.Background()
	svc := &Service{DB: db, Calendar: fixtureCalendar{}, Now: func() time.Time { return day("20250103") }}
	st := store.NewMySQLStore(db)
	for _, code := range core.RotationCodes {
		if _, err := st.UpsertDaily(ctx, []core.Bar{{TsCode: code, TradeDate: "20250102", Open: 100, High: 100, Low: 100, Close: 100}}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertAdjFactors(ctx, []core.AdjFactor{{TsCode: code, TradeDate: "20250102", AdjFactor: 1}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO rotation_coverage VALUES (?,?)", code, "20250102"); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.PublishClose(ctx, "20250102"); err != nil {
		t.Fatal(err)
	}
	corrected := core.Bar{TsCode: core.RotationCodes[0], TradeDate: "20250102", Open: 101, High: 101, Low: 101, Close: 101}
	if _, err := st.UpsertDaily(ctx, []core.Bar{corrected}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM rotation_calendar"); err != nil {
		t.Fatal(err)
	}
	paused := &waitingCalendar{entered: make(chan struct{}), resume: make(chan struct{})}
	svc.Calendar = paused
	done := make(chan error, 1)
	go func() { done <- svc.PublishClose(ctx, "20250102") }()
	select {
	case <-paused.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("publication did not begin")
	}
	corrected.Close, corrected.High = 102, 102
	_, writeErr := st.UpsertDaily(ctx, []core.Bar{corrected})
	close(paused.resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	w := httptest.NewRecorder()
	svc.DailyHandler().ServeHTTP(w, httptest.NewRequest("GET", "/?tradeDate=20250102", nil))
	var v DailyView
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Close == nil || *v.Close.Cards[0].Price != 100 || v.Status != "updating" {
		t.Fatalf("superseded publication escaped: %s", w.Body.String())
	}
	if err := svc.RefreshDaily(ctx); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	svc.DailyHandler().ServeHTTP(w, httptest.NewRequest("GET", "/?tradeDate=20250102", nil))
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Close == nil || *v.Close.Cards[0].Price != 102 || v.Status != "ready" {
		t.Fatalf("latest revision not published: %s", w.Body.String())
	}
}

func TestDailyMalformedSourceDateDoesNotEnterMetrics(t *testing.T) {
	db := dailyDatabase(t)
	ctx := context.Background()
	svc := &Service{DB: db, Calendar: fixtureCalendar{}, Now: func() time.Time { return day("20250103") }}
	st := store.NewMySQLStore(db)
	for i, code := range core.RotationCodes {
		var bars []core.Bar
		for d := day("20241101"); !d.After(day("20250102")); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			bars = append(bars, core.Bar{TsCode: code, TradeDate: d.Format("20060102"), Open: 100, High: 100, Low: 100, Close: 100})
		}
		if i == 0 {
			bars = append(bars, core.Bar{TsCode: code, TradeDate: "20241200", Open: 120, High: 120, Low: 120, Close: 120})
		}
		if _, err := st.UpsertDaily(ctx, bars); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertAdjFactors(ctx, []core.AdjFactor{{TsCode: code, TradeDate: "20241031", AdjFactor: 1}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO rotation_coverage VALUES (?,?)", code, "20250102"); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.PublishClose(ctx, "20250102"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	svc.DailyHandler().ServeHTTP(w, httptest.NewRequest("GET", "/?tradeDate=20250102", nil))
	var v DailyView
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Close == nil {
		t.Fatal(w.Body.String())
	}
	if v.Close.Cards[0].Score != nil || !strings.Contains(v.Close.Cards[0].Reasons["score"], "日期") {
		t.Fatalf("invalid source date was used: %+v", v.Close.Cards[0])
	}
	for _, c := range v.Close.Cards {
		if c.Rank != nil {
			t.Fatal("invalid source date entered ranking")
		}
	}
}
