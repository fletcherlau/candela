package rotation

import (
	"context"
	"math"
	"strings"
	"sync"
	"syncer/internal/core"
	"syncer/internal/schema"
	"syncer/internal/store"
	"testing"
	"time"
)

func TestDailyHTTPDefaultsFollowPublicationReadiness(t *testing.T) {
	db, svc, source := seedReferenceScenario(t, "20250103")
	target, _ := captureTarget("20250103")
	var mu sync.Mutex
	now := target.Add(-time.Minute)
	svc.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	setTime := func(value time.Time) { mu.Lock(); now = value; mu.Unlock() }
	if err := svc.PublishClose(context.Background(), "20250102"); err != nil {
		t.Fatal(err)
	}
	api := captureAPI(t, svc)
	url := strings.TrimSuffix(api, capturePath) + "/api/v1/rotation/daily"
	read := func() DailyView {
		t.Helper()
		var value DailyView
		captureHTTP(t, "GET", url, "", 200, &value)
		return value
	}
	before := read()
	if before.TradeDate != "20250102" || before.Close == nil || before.SelectionMode != "default" || !before.Fallback || before.CurrentTradingDate != "20250103" {
		t.Fatalf("pre-1445 default: %+v", before)
	}
	setTime(target.Add(3 * time.Second))
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 202, nil)
	queued := read()
	if queued.TradeDate != "20250102" || queued.Pending == nil || queued.Pending.TradeDate != "20250103" || queued.Pending.Reference.Status != "pending" {
		t.Fatalf("queued fallback: %+v", queued)
	}
	startCaptureWorker(t, svc)
	startReferenceWorker(t, svc)
	until := time.Now().Add(3 * time.Second)
	for {
		view := read()
		if view.TradeDate == "20250103" && view.Reference != nil {
			if view.Close != nil || view.Fallback {
				t.Fatalf("reference stage mixed dates: %+v", view)
			}
			break
		}
		if time.Now().After(until) {
			t.Fatalf("ready reference did not become default: %+v", view)
		}
		time.Sleep(10 * time.Millisecond)
	}
	setTime(target.Add(2 * time.Hour))
	st := store.NewMySQLStore(db)
	for i, code := range core.RotationCodes {
		p := float64(100+i) * 1.003
		if _, err := st.UpsertDaily(context.Background(), []core.Bar{{TsCode: code, TradeDate: "20250103", Open: p, High: p, Low: p, Close: p}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("UPDATE rotation_coverage SET through_date='20250103' WHERE ts_code=?", code); err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			if err := svc.PublishClose(context.Background(), "20250103"); err != nil {
				t.Fatal(err)
			}
			partial := read()
			if partial.TradeDate != "20250103" || partial.Reference == nil || partial.Close != nil || partial.CloseState.Available != 3 || len(partial.CloseState.Missing) != 1 || partial.CloseState.Missing[0].Code != "513100.SH" {
				t.Fatalf("partial close replaced reference: %+v", partial)
			}
		}
	}
	if err := svc.PublishClose(context.Background(), "20250103"); err != nil {
		t.Fatal(err)
	}
	complete := read()
	if complete.TradeDate != "20250103" || complete.Reference == nil || complete.Close == nil || len(complete.PriceSlippage) != 4 {
		t.Fatalf("complete comparison: %+v", complete)
	}
	if strings.Contains(complete.CloseState.Message, "参考尚未留存") {
		t.Fatal("published reference is contradicted by close stage", complete.CloseState.Message)
	}
	for _, slip := range complete.PriceSlippage {
		if slip.Bps == nil || math.Abs(*slip.Bps-30) > 1e-7 {
			t.Fatalf("100 to 100.3 must be +30bps: %+v", slip)
		}
	}
	if source.count() != 4 {
		t.Fatal("readiness polling fetched additional quotes")
	}
}

func TestDailyHTTPCalendarAbsenceIsExplicitAndBackgroundCanFillIt(t *testing.T) {
	db, svc, source := seedReferenceScenario(t, "20250103")
	target, _ := captureTarget("20250103")
	svc.Now = func() time.Time { return target.Add(-time.Minute) }
	if err := svc.PublishClose(context.Background(), "20250102"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM rotation_calendar WHERE cal_date='20250103'"); err != nil {
		t.Fatal(err)
	}
	api := captureAPI(t, svc)
	url := strings.TrimSuffix(api, capturePath) + "/api/v1/rotation/daily"
	var before, after DailyView
	captureHTTP(t, "GET", url, "", 200, &before)
	if before.Status != "calendar_unavailable" || before.Close != nil {
		t.Fatalf("missing calendar silently guessed today: %+v", before)
	}
	if err := svc.RefreshDaily(context.Background()); err != nil {
		t.Fatal(err)
	}
	captureHTTP(t, "GET", url, "", 200, &after)
	if after.CalendarStatus != "ready" || after.TradeDate != "20250102" || after.Close == nil {
		t.Fatalf("background did not cache today's calendar: %+v", after)
	}
	if source.count() != 0 {
		t.Fatal("reading or calendar preparation requested live quotes")
	}
}

func TestDailyHTTPMissingAndHistoricalSelectionDoNotMixDates(t *testing.T) {
	for _, scenario := range []string{"missing_reference", "no_previous", "non_trading", "manual_missing", "calendar_gap", "close_without_reference"} {
		t.Run(scenario, func(t *testing.T) {
			db, svc, source := seedReferenceScenario(t, "20250103")
			target, _ := captureTarget("20250103")
			now := target.Add(2 * time.Hour)
			svc.Now = func() time.Time { return now }
			if scenario != "no_previous" {
				if err := svc.PublishClose(context.Background(), "20250102"); err != nil {
					t.Fatal(err)
				}
			}
			query := ""
			switch scenario {
			case "non_trading":
				now = target.Add(24 * time.Hour)
				if _, err := db.Exec("INSERT INTO rotation_calendar VALUES ('20250104',0)"); err != nil {
					t.Fatal(err)
				}
			case "calendar_gap":
				now = target.Add(48 * time.Hour)
				if _, err := db.Exec("INSERT INTO rotation_calendar VALUES ('20250105',0)"); err != nil {
					t.Fatal(err)
				}
			case "manual_missing":
				query = "?tradeDate=20241231"
			case "close_without_reference":
				st := store.NewMySQLStore(db)
				for i, code := range core.RotationCodes {
					p := float64(100 + i)
					if _, err := st.UpsertDaily(context.Background(), []core.Bar{{TsCode: code, TradeDate: "20250103", Open: p, High: p, Low: p, Close: p}}); err != nil {
						t.Fatal(err)
					}
					if _, err := db.Exec("UPDATE rotation_coverage SET through_date='20250103' WHERE ts_code=?", code); err != nil {
						t.Fatal(err)
					}
				}
				if err := svc.PublishClose(context.Background(), "20250103"); err != nil {
					t.Fatal(err)
				}
			}
			api := captureAPI(t, svc)
			var view DailyView
			captureHTTP(t, "GET", strings.TrimSuffix(api, capturePath)+"/api/v1/rotation/daily"+query, "", 200, &view)
			switch scenario {
			case "missing_reference":
				if view.TradeDate != "20250102" || !view.Fallback || view.Close == nil || view.Pending == nil || view.Pending.Reference.Status != "missing" {
					t.Fatalf("missing reference fallback: %+v", view)
				}
			case "no_previous":
				if view.Close != nil || view.Reference != nil || view.Status != "unavailable" {
					t.Fatalf("invented previous publication: %+v", view)
				}
			case "non_trading":
				if view.TradeDate != "20250102" || view.CurrentTradingDate != "20250103" || view.Close == nil {
					t.Fatalf("closed-day available date: %+v", view)
				}
			case "manual_missing":
				if view.TradeDate != "20241231" || view.SelectionMode != "manual" || view.Fallback || view.Close != nil || view.Reference != nil {
					t.Fatalf("manual date replaced: %+v", view)
				}
			case "calendar_gap":
				if view.Status != "calendar_unavailable" || view.Close != nil {
					t.Fatalf("missing day guessed closed: %+v", view)
				}
			case "close_without_reference":
				if view.TradeDate != "20250103" || view.Close == nil || view.Reference != nil || view.Status != "ready" {
					t.Fatalf("close blocked by missing reference: %+v", view)
				}
				for _, slip := range view.PriceSlippage {
					if slip.Bps != nil || slip.Reason == "" {
						t.Fatalf("missing reference converted to a number: %+v", slip)
					}
				}
			}
			if source.count() != 0 {
				t.Fatal("daily reads requested quotes")
			}
		})
	}
}

func TestDailyHTTPPartialReferenceKeepsPreviousSession(t *testing.T) {
	_, svc, source := seedReferenceScenario(t, "20250103")
	source.failures["513100.SH"] = true
	if err := svc.PublishClose(context.Background(), "20250102"); err != nil {
		t.Fatal(err)
	}
	api := captureAPI(t, svc)
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, svc)
	startReferenceWorker(t, svc)
	waitCapture(t, api, "20250103", "partial")
	var view DailyView
	captureHTTP(t, "GET", strings.TrimSuffix(api, capturePath)+"/api/v1/rotation/daily", "", 200, &view)
	if view.TradeDate != "20250102" || view.Close == nil || view.Reference != nil || view.Pending == nil || view.Pending.Reference.Available != 3 || len(view.Pending.Reference.Missing) != 1 || view.Pending.Reference.Missing[0].Code != "513100.SH" {
		t.Fatalf("partial reference escaped or lost missing object: %+v", view)
	}
}

func TestDailyHTTPInvalidSavedPricesNeverProduceSlippage(t *testing.T) {
	db, svc, _ := seedReferenceScenario(t, "20250103")
	if err := svc.PublishClose(context.Background(), "20250102"); err != nil {
		t.Fatal(err)
	}
	// Fixture an otherwise valid saved reference for the same day. HTTP is the
	// assertion boundary; corrupt nullable fields must never become zero bps.
	if _, err := db.Exec(`INSERT INTO rotation_daily(trade_date,basis,revision,status,available,payload,published_at) SELECT trade_date,'reference_1445',0,'ready',4,JSON_SET(payload,'$.basis','reference_1445'),published_at FROM rotation_daily WHERE trade_date='20250102' AND basis='close'`); err != nil {
		t.Fatal(err)
	}
	api := captureAPI(t, svc)
	url := strings.TrimSuffix(api, capturePath) + "/api/v1/rotation/daily?tradeDate=20250102"
	for _, value := range []string{"0", "-1", "null"} {
		if _, err := db.Exec(`UPDATE rotation_daily SET payload=JSON_SET(payload,'$.cards[0].price',CAST(? AS JSON)) WHERE trade_date='20250102' AND basis='reference_1445'`, value); err != nil {
			t.Fatal(err)
		}
		var view DailyView
		captureHTTP(t, "GET", url, "", 200, &view)
		if len(view.PriceSlippage) != 4 || view.PriceSlippage[0].Bps != nil || view.PriceSlippage[0].Reason == "" {
			t.Fatalf("%s became numeric slippage: %+v", value, view.PriceSlippage)
		}
	}
	if _, err := db.Exec(`UPDATE rotation_daily SET payload=JSON_SET(payload,'$.cards[0].price','NaN') WHERE trade_date='20250102' AND basis='reference_1445'`); err != nil {
		t.Fatal(err)
	}
	captureHTTP(t, "GET", url, "", 503, nil)
}

func TestDailyHTTPReadableAfterInterruptedSchemaUpgrade(t *testing.T) {
	db, svc, _ := seedReferenceScenario(t, "20250103")
	// Start with the deployed v6 schema, then exercise both a fresh upgrade and
	// recovery after its DDL succeeded but the version record was not committed.
	if _, err := db.Exec("ALTER TABLE rotation_daily DROP COLUMN missing"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM schema_migration WHERE version=7"); err != nil {
		t.Fatal(err)
	}
	if err := schema.Ensure(context.Background(), db); err != nil {
		t.Fatalf("fresh upgrade: %v", err)
	}
	if err := svc.PublishClose(context.Background(), "20250102"); err != nil {
		t.Fatal(err)
	}
	// Simulate termination after the non-transactional DDL committed, before
	// startup could record the migration version. Published data must survive.
	if _, err := db.Exec("DELETE FROM schema_migration WHERE version=7"); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := schema.Ensure(context.Background(), db); err != nil {
			t.Fatalf("restart %d: %v", attempt, err)
		}
	}
	api := captureAPI(t, svc)
	var view DailyView
	captureHTTP(t, "GET", strings.TrimSuffix(api, capturePath)+"/api/v1/rotation/daily?tradeDate=20250102", "", 200, &view)
	if view.Close == nil || view.Close.Available != 4 || view.Close.Cards[0].Price == nil || *view.Close.Cards[0].Price != 100 {
		t.Fatalf("publication lost during schema recovery: %+v", view)
	}
}

func TestDailyHTTPTradingDayFallbackRequiresCompleteCalendar(t *testing.T) {
	for _, offset := range []time.Duration{-time.Minute, time.Minute} {
		t.Run(offset.String(), func(t *testing.T) {
			db, svc, source := seedReferenceScenario(t, "20250106")
			target, _ := captureTarget("20250106")
			svc.Now = func() time.Time { return target.Add(offset) }
			if err := svc.PublishClose(context.Background(), "20250103"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("DELETE FROM rotation_calendar WHERE cal_date='20250104'"); err != nil {
				t.Fatal(err)
			}
			api := captureAPI(t, svc)
			url := strings.TrimSuffix(api, capturePath) + "/api/v1/rotation/daily"
			var unknown, saved, recovered DailyView
			captureHTTP(t, "GET", url, "", 200, &unknown)
			if unknown.Status != "calendar_unavailable" || unknown.CalendarStatus != "unavailable" || unknown.TradeDate != "" || unknown.Close != nil {
				t.Fatalf("fallback guessed missing history instead of missing calendar: %+v", unknown)
			}
			captureHTTP(t, "GET", url+"?tradeDate=20250103", "", 200, &saved)
			if saved.Close == nil {
				t.Fatal("published Friday was lost")
			}
			if _, err := db.Exec("INSERT INTO rotation_calendar VALUES ('20250104',0)"); err != nil {
				t.Fatal(err)
			}
			captureHTTP(t, "GET", url, "", 200, &recovered)
			if recovered.CalendarStatus != "ready" || recovered.TradeDate != "20250103" || recovered.Close == nil || !recovered.Fallback {
				t.Fatalf("confirmed calendar did not restore Friday: %+v", recovered)
			}
			if source.count() != 0 {
				t.Fatal("read requested live quotes")
			}
		})
	}
}
