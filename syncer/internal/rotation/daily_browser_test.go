package rotation

import (
	"context"
	"net/http"
	"os"
	"syncer/internal/core"
	"syncer/internal/store"
	"testing"
	"time"
)

// An opt-in real HTTP/MySQL fixture; this is never included in the service binary.
func TestDailyBrowserHarness(t *testing.T) {
	if os.Getenv("CANDELA_DAILY_BROWSER_TEST") != "1" {
		t.Skip("browser harness is opt-in")
	}
	db := dailyDatabase(t)
	ctx := context.Background()
	st := store.NewMySQLStore(db)
	for i, code := range core.RotationCodes {
		var bars []core.Bar
		p := float64(100 + i)
		for d := day("20180101"); !d.After(day("20250102")); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			bars = append(bars, core.Bar{TsCode: code, TradeDate: d.Format("20060102"), Open: p, High: p, Low: p, Close: p})
		}
		if _, err := st.UpsertDaily(ctx, bars); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertAdjFactors(ctx, []core.AdjFactor{{TsCode: code, TradeDate: "20171229", AdjFactor: 1}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO rotation_coverage VALUES (?,?)", code, "20250102"); err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{DB: db, Calendar: fixtureCalendar{}, Now: func() time.Time { return day("20250103") }}
	if err := svc.RefreshDaily(ctx); err != nil {
		t.Fatal(err)
	}
	h := svc.DailyHandler()
	t.Fatal(http.ListenAndServe("127.0.0.1:18084", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "fixture-only" {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path != "/api/v1/rotation/daily" {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})))
}
