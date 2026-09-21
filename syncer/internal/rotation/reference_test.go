package rotation

import (
	"context"
	"encoding/json"
	"strings"
	"syncer/internal/core"
	"syncer/internal/store"
	"testing"
	"time"
)

func TestReferenceHTTPPublishesFrozenMetricsOnce(t *testing.T) {
	db := dailyDatabase(t)
	ctx := context.Background()
	target, _ := captureTarget("20250103")
	source := sourceAt("20250103")
	st := store.NewMySQLStore(db)
	for i, code := range core.RotationCodes {
		p := float64(100 + i)
		var bars []core.Bar
		for d := day("20241101"); !d.After(day("20250102")); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			bars = append(bars, core.Bar{TsCode: code, TradeDate: d.Format("20060102"), Open: p, High: p, Low: p, Close: p})
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
		q := source.quotes[code]
		q.Open = p
		q.High = p
		q.Low = p
		q.Latest = p
		source.quotes[code] = q
	}
	for d := day("20241101"); !d.After(day("20250103")); d = d.AddDate(0, 0, 1) {
		open := d.Weekday() != time.Saturday && d.Weekday() != time.Sunday
		if _, err := db.Exec("INSERT INTO rotation_calendar VALUES (?,?)", d.Format("20060102"), open); err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{DB: db, Realtime: source, QuantileWindow: 5, Now: func() time.Time { return target.Add(3 * time.Second) }}
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
