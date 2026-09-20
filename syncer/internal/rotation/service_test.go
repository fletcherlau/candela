package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	_ "github.com/go-sql-driver/mysql"
	"net/http/httptest"
	"os"
	"strings"
	"syncer/internal/core"
	"syncer/internal/schema"
	"syncer/internal/store"
	"testing"
	"time"
)

type waitingCalendar struct {
	entered chan struct{}
	resume  chan struct{}
	once    bool
}

func (w *waitingCalendar) Calendar(ctx context.Context, a, b string) (map[string]bool, error) {
	if !w.once {
		w.once = true
		close(w.entered)
		<-w.resume
	}
	return (fixtureCalendar{}).Calendar(ctx, a, b)
}

type emptySource struct{}

func (emptySource) FetchDaily(context.Context, string, string, string) ([]core.Bar, error) {
	return nil, nil
}
func (emptySource) FetchAdj(context.Context, string, string, string) ([]core.AdjFactor, error) {
	return nil, nil
}

type fixtureCalendar struct{}

func (fixtureCalendar) Calendar(_ context.Context, a, b string) (map[string]bool, error) {
	out := map[string]bool{}
	for d := day(a); !d.After(day(b)); d = d.AddDate(0, 0, 1) {
		out[d.Format("20060102")] = d.Weekday() != time.Saturday && d.Weekday() != time.Sunday
	}
	return out, nil
}
func TestPublicationLifecycle(t *testing.T) {
	dsn := os.Getenv("ROTATION_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated MySQL required")
	}
	if !strings.Contains(dsn, "/candela_rotation_test?") {
		t.Fatal("only isolated candela_rotation_test allowed")
	}
	db, e := sql.Open("mysql", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx := context.Background()
	if e = schema.Ensure(ctx, db); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"rotation_result", "rotation_coverage", "rotation_calendar", "etf_daily", "etf_adj_factor"} {
		if _, e = db.Exec("DELETE FROM " + table); e != nil {
			t.Fatal(e)
		}
	}
	db.Exec("INSERT INTO rotation_result(id) VALUES(1)")
	svc := &Service{DB: db, Calendar: fixtureCalendar{}}
	st := store.NewMySQLStore(db)
	start := day(horizon()).AddDate(-6, 0, 0)
	var bars []core.Bar
	var factors []core.AdjFactor
	for _, code := range core.RotationCodes {
		for d := start; !d.After(day(horizon())); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			dt := d.Format("20060102")
			bars = append(bars, core.Bar{TsCode: code, TradeDate: dt, Open: 100, High: 100, Low: 100, Close: 100})
			factors = append(factors, core.AdjFactor{TsCode: code, TradeDate: dt, AdjFactor: 1})
		}
		db.Exec("INSERT INTO rotation_coverage VALUES(?,?)", code, horizon())
	}
	for i := 0; i < len(bars); i += 500 {
		end := i + 500
		if end > len(bars) {
			end = len(bars)
		}
		if _, e = st.UpsertDaily(ctx, bars[i:end]); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = st.UpsertAdjFactors(ctx, factors); e != nil {
		t.Fatal(e)
	}
	if e = svc.Refresh(ctx); e != nil {
		t.Fatal(e)
	}
	read := func() View {
		w := httptest.NewRecorder()
		svc.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var v View
		if json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal(w.Body.String())
		}
		return v
	}
	first := read()
	if first.Status != "ready" || first.Result == nil || len(first.Result.Days) < 100 {
		t.Fatalf("%+v", first)
	}
	// Repeated refresh is idempotent, and browser reads do not refresh data.
	if e = svc.Refresh(ctx); e != nil {
		t.Fatal(e)
	}
	if read().UpdatedAt != first.UpdatedAt {
		t.Fatal("unnecessary republish")
	}
	release, e := svc.BeginSync(ctx, core.RotationCodes)
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.Refresh(ctx); e == nil {
		t.Fatal("published during sync")
	}
	release(core.Summary{})
	failed := read()
	if failed.Status != "failed" || failed.Result.End != first.Result.End {
		t.Fatal("failure lost prior result")
	}
	// A historical correction invalidates the complete result; restart recovers pending work.
	corrected := bars[len(bars)-2]
	corrected.Close = 105
	corrected.High = 105
	if _, e = st.UpsertDaily(ctx, []core.Bar{corrected}); e != nil {
		t.Fatal(e)
	}
	restarted := &Service{DB: db, Calendar: fixtureCalendar{}}
	if e = restarted.Refresh(ctx); e != nil {
		t.Fatal(e)
	}
	updated := read()
	if updated.Status != "ready" {
		t.Fatalf("%+v", updated)
	}
	// Completion through the real shared sync boundary schedules publication.
	runner := core.NewSyncer(emptySource{}, st, 366, "20100101", horizon)
	runner.SetObserver(svc)
	sum := runner.Run(ctx, core.RotationCodes)
	if sum.Success != 4 || read().Status != "pending" {
		t.Fatal("sync did not schedule refresh")
	}
	if e = svc.Refresh(ctx); e != nil {
		t.Fatal(e)
	}
	// A raw-data correction during calculation invalidates the candidate atomically.
	db.Exec("DELETE FROM rotation_calendar")
	db.Exec("UPDATE rotation_result SET status='computing'")
	paused := &waitingCalendar{entered: make(chan struct{}), resume: make(chan struct{})}
	racing := &Service{DB: db, Calendar: paused}
	done := make(chan error, 1)
	go func() { done <- racing.Refresh(ctx) }()
	<-paused.entered
	previous := read().Result.Days[len(read().Result.Days)-1].NAV
	corrected.Close = 110
	corrected.High = 110
	if _, e = st.UpsertDaily(ctx, []core.Bar{corrected}); e != nil {
		t.Fatal(e)
	}
	close(paused.resume)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	superseded := read()
	if superseded.Status != "pending" || superseded.Result.Days[len(superseded.Result.Days)-1].NAV != previous {
		t.Fatal("old calculation overwrote corrected input")
	}
	if e = restarted.Refresh(ctx); e != nil {
		t.Fatal(e)
	}
	if read().Status != "ready" {
		t.Fatal("pending computation not recovered")
	}
	// Missing factors must not silently become 1, and a failed calculation keeps old publication.
	db.Exec("DELETE FROM etf_adj_factor WHERE ts_code=?", core.RotationCodes[0])
	db.Exec("UPDATE rotation_result SET status='pending',revision=revision+1")
	if e = svc.Refresh(ctx); e != nil {
		t.Fatal(e)
	}
	missing := read()
	if missing.Status != "failed" || missing.Result == nil || !strings.Contains(missing.Message, "复权因子") {
		t.Fatalf("%+v", missing)
	}
	// Interrupted computation is reattempted; an interrupted source sync is reported, never published.
	db.Exec("UPDATE rotation_result SET status='syncing'")
	svc.Refresh(ctx)
	if read().Status != "failed" {
		t.Fatal("interrupted sync published")
	}
}
