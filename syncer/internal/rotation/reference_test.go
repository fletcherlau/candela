package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"syncer/internal/core"
	"syncer/internal/store"
	"testing"
	"time"
)

func TestReferenceHTTPPublishesFrozenMetricsOnce(t *testing.T) {
	db, svc, source := seedReferenceScenario(t, "20250103")
	target, _ := captureTarget("20250103")
	api := captureAPI(t, svc)
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, svc)
	startReferenceWorker(t, svc)
	waitCapture(t, api, "20250103", "captured")
	dailyURL := strings.TrimSuffix(api, capturePath) + "/api/v1/rotation/daily?tradeDate=20250103"
	var view DailyView
	until := time.Now().Add(3 * time.Second)
	for {
		captureHTTP(t, "GET", dailyURL, "", 200, &view)
		if view.Reference != nil {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("frozen reference was not published: %+v", view)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if view.Reference.Basis != "reference_1445" || view.Reference.QuantileWindow != 5 || len(view.Reference.Cards) != 4 {
		t.Fatalf("reference identity: %+v", view.Reference)
	}
	for i, card := range view.Reference.Cards {
		if card.Price == nil || *card.Price != float64(100+i) || card.Score == nil || *card.Score != 0 || card.Volatility == nil || *card.Volatility != 0 || card.Quantile == nil || *card.Quantile != 50 || card.Weight == nil || *card.Weight != 1 || card.Rank == nil || *card.Rank != i+1 {
			t.Fatalf("flat-series worked example: %+v", card)
		}
	}
	var record struct{ Run CaptureRun }
	captureHTTP(t, "GET", api+"/20250103", "", 200, &record)
	if record.Run.Stage != "reference_published" {
		t.Fatalf("management still claims reference pending: %+v", record.Run)
	}
	first, _ := json.Marshal(view.Reference)
	// Later history corrections, a reconstructed reader, and duplicate submission
	// cannot change the original reference or cause additional live quote requests.
	if _, err := db.Exec("UPDATE etf_daily SET open=200,high=200,low=200,close=200"); err != nil {
		t.Fatal(err)
	}
	fresh := &Service{DB: db, Realtime: source, QuantileWindow: 1200, Now: func() time.Time { return target.Add(24 * time.Hour) }}
	freshAPI := captureAPI(t, fresh)
	captureHTTP(t, "POST", freshAPI, `{"tradeDate":"20250103"}`, 200, nil)
	startCaptureWorker(t, fresh)
	startReferenceWorker(t, fresh)
	var reread DailyView
	captureHTTP(t, "GET", strings.TrimSuffix(freshAPI, capturePath)+"/api/v1/rotation/daily?tradeDate=20250103", "", 200, &reread)
	after, _ := json.Marshal(reread.Reference)
	if string(after) != string(first) || source.count() != 4 {
		t.Fatal("saved reference changed or reader fetched quotes")
	}
}

func startReferenceWorker(t *testing.T, s *Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.ServeReferences(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("reference worker did not stop")
		}
	})
}

func seedReferenceScenario(t *testing.T, date string) (*sql.DB, *Service, *captureSource) {
	t.Helper()
	db := dailyDatabase(t)
	ctx := context.Background()
	target, _ := captureTarget(date)
	source := sourceAt(date)
	st := store.NewMySQLStore(db)
	for i, code := range core.RotationCodes {
		p := float64(100 + i)
		var bars []core.Bar
		for d := target.AddDate(0, -2, 0); !d.After(target.AddDate(0, 0, -1)); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			bars = append(bars, core.Bar{TsCode: code, TradeDate: d.Format("20060102"), Open: p, High: p, Low: p, Close: p})
		}
		if _, err := st.UpsertDaily(ctx, bars); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertAdjFactors(ctx, []core.AdjFactor{{TsCode: code, TradeDate: target.AddDate(0, -2, -1).Format("20060102"), AdjFactor: 1}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO rotation_coverage VALUES (?,?)", code, target.AddDate(0, 0, -1).Format("20060102")); err != nil {
			t.Fatal(err)
		}
		q := source.quotes[code]
		q.Open = p
		q.High = p
		q.Low = p
		q.Latest = p
		source.quotes[code] = q
	}
	for d := target.AddDate(0, -2, 0); !d.After(target); d = d.AddDate(0, 0, 1) {
		open := d.Weekday() != time.Saturday && d.Weekday() != time.Sunday
		if _, err := db.Exec("INSERT INTO rotation_calendar VALUES (?,?)", d.Format("20060102"), open); err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{DB: db, Calendar: fixtureCalendar{}, Realtime: source, QuantileWindow: 5, Now: func() time.Time { return target.Add(3 * time.Second) }}

	return db, svc, source
}

func TestReferenceHTTPFrozenHistoryDistinguishesSuspensionGapAndShortWindow(t *testing.T) {
	for _, scenario := range []string{"verified_suspension", "unknown_gap", "short_window"} {
		t.Run(scenario, func(t *testing.T) {
			db, svc, _ := seedReferenceScenario(t, "20220114")
			code, date := "513100.SH", "20220113"
			if scenario == "unknown_gap" {
				date = "20220112"
			}
			if scenario == "short_window" {
				if _, err := db.Exec("DELETE FROM etf_daily WHERE ts_code=? AND trade_date<'20220113'", code); err != nil {
					t.Fatal(err)
				}
			} else if _, err := db.Exec("DELETE FROM etf_daily WHERE ts_code=? AND trade_date=?", code, date); err != nil {
				t.Fatal(err)
			}
			api := captureAPI(t, svc)
			captureHTTP(t, "POST", api, `{"tradeDate":"20220114"}`, 202, nil)
			startCaptureWorker(t, svc)
			waitCapture(t, api, "20220114", "captured")
			// Recover calculation after the live history/calendar has changed. Only the
			// evidence saved with the original input is permitted to determine metrics.
			if _, err := db.Exec("DELETE FROM rotation_calendar"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("UPDATE etf_daily SET close=190,high=190"); err != nil {
				t.Fatal(err)
			}
			startReferenceWorker(t, svc)
			until := time.Now().Add(3 * time.Second)
			var view DailyView
			for {
				captureHTTP(t, "GET", strings.TrimSuffix(api, capturePath)+"/api/v1/rotation/daily?tradeDate=20220114", "", 200, &view)
				if view.Reference != nil {
					break
				}
				if time.Now().After(until) {
					t.Fatal("reference calculation did not complete")
				}
				time.Sleep(10 * time.Millisecond)
			}
			card := view.Reference.Cards[3]
			if card.Price == nil || *card.Price != 103 {
				t.Fatalf("valid reference price lost: %+v", card)
			}
			if scenario == "verified_suspension" {
				if card.Score == nil || *card.Score != 0 || card.Quantile == nil || *card.Quantile != 50 {
					t.Fatalf("verified full suspension changed formula: %+v", card)
				}
			} else {
				if card.Score != nil || card.Quantile != nil || card.Weight != nil || card.Reasons["score"] == "" {
					t.Fatalf("missing history invented indicators: %+v", card)
				}
				for _, c := range view.Reference.Cards {
					if c.Rank != nil {
						t.Fatal("partial universe received formal rankings")
					}
				}
			}
		})
	}
}

func TestReferenceHTTPUnsupportedFrozenParametersAreReportedAsFailed(t *testing.T) {
	db, svc, _ := seedReferenceScenario(t, "20250103")
	if err := svc.PublishClose(context.Background(), "20250102"); err != nil {
		t.Fatal(err)
	}
	api := captureAPI(t, svc)
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, svc)
	waitCapture(t, api, "20250103", "captured")
	// A stored input from an unsupported calculator version must not silently
	// use the running service's current defaults after a deployment/recovery.
	if _, err := db.Exec("UPDATE rotation_capture_run SET params=JSON_SET(params,'$.version','unsupported-version')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE rotation_reference_input SET payload=JSON_SET(payload,'$.params.version','unsupported-version')"); err != nil {
		t.Fatal(err)
	}
	startReferenceWorker(t, svc)
	var view DailyView
	until := time.Now().Add(3 * time.Second)
	for {
		captureHTTP(t, "GET", strings.TrimSuffix(api, capturePath)+"/api/v1/rotation/daily", "", 200, &view)
		if view.Pending != nil && view.Pending.Reference.Status == "failed" {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("unsupported calculator not reported: %+v", view)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if view.TradeDate != "20250102" || view.Close == nil || view.Reference != nil {
		t.Fatalf("failed reference replaced fallback: %+v", view)
	}
	var record struct{ Run CaptureRun }
	captureHTTP(t, "GET", api+"/20250103", "", 200, &record)
	if record.Run.Stage != "reference_failed" {
		t.Fatalf("management failure missing: %+v", record.Run)
	}
}
