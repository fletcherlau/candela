package rotation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"syncer/internal/core"
	"syncer/internal/store"
	"syncer/internal/syncrun"
	"testing"
	"time"
)

// Only the external source is controlled. Requests, durable jobs, raw writes,
// calculation, publication and public reads use the real production boundaries.
type correctionSource struct {
	failFactors bool
	holdFactors <-chan struct{}
	mu          sync.RWMutex
	bars        map[string][]core.Bar
	factors     map[string][]core.AdjFactor
}

func (f *correctionSource) FetchDaily(_ context.Context, code, from, to string) ([]core.Bar, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var result []core.Bar
	for _, bar := range f.bars[code] {
		if bar.TradeDate >= from && bar.TradeDate <= to {
			result = append(result, bar)
		}
	}
	return result, nil
}
func (f *correctionSource) FetchAdj(ctx context.Context, code, from, to string) ([]core.AdjFactor, error) {
	f.mu.RLock()
	if f.failFactors {
		f.mu.RUnlock()
		return nil, fmt.Errorf("isolated factor source unavailable")
	}
	var result []core.AdjFactor
	for _, factor := range f.factors[code] {
		if factor.TradeDate >= from && factor.TradeDate <= to {
			result = append(result, factor)
		}
	}
	hold := f.holdFactors
	f.mu.RUnlock()
	if hold != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-hold:
		}
	}
	return result, nil
}
func dailyFromHTTP(t *testing.T, api, date string) DailyView {
	t.Helper()
	var result DailyView
	captureHTTP(t, "GET", api+"/api/v1/rotation/daily?tradeDate="+date, "", 200, &result)
	return result
}
func TestCorrectionHTTPRepublishesRecordedCloseDaysAndSlippageWithoutChangingReference(t *testing.T) {
	db, captureService, realtime := seedReferenceScenario(t, "20250103")
	api := recoveryAPI(t, captureService)
	captureHTTP(t, "POST", api+capturePath, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, captureService)
	startReferenceWorker(t, captureService)
	waitCapture(t, api+capturePath, "20250103", "captured")
	until := time.Now().Add(4 * time.Second)
	var reference DailyView
	for {
		reference = dailyFromHTTP(t, api, "20250103")
		if reference.Reference != nil {
			break
		}
		if time.Now().After(until) {
			t.Fatal("initial reference did not publish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	frozen, _ := json.Marshal(reference.Reference)
	source := &correctionSource{bars: map[string][]core.Bar{}, factors: map[string][]core.AdjFactor{}}
	dates := []string{"20250103", "20250106", "20250107"}
	for i, code := range core.RotationCodes {
		price := float64(100 + i)
		for _, date := range dates {
			source.bars[code] = append(source.bars[code], core.Bar{TsCode: code, TradeDate: date, Open: price, High: price, Low: price, Close: price})
			source.factors[code] = append(source.factors[code], core.AdjFactor{TsCode: code, TradeDate: date, AdjFactor: 1})
		}
	}
	now := func() time.Time { return time.Date(2025, 1, 7, 10, 0, 0, 0, time.UTC) }
	jobs := &syncrun.ETFService{Store: syncrun.NewETFStore(db), Source: source, DefaultStart: "20241101", ChunkDays: 31, Now: now}
	etfAPI := etfRecoveryAPI(t, jobs)
	ctx, stop := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); jobs.Serve(ctx) }()
	t.Cleanup(func() { stop(); <-workerDone })
	body, _ := json.Marshal(map[string]any{"codes": core.RotationCodes})
	var accepted struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	captureHTTP(t, "POST", etfAPI, string(body), 202, &accepted)
	waitETFBatch(t, etfAPI, accepted.Batch.ID, "succeeded")
	service := &Service{DB: db, Calendar: fixtureCalendar{}, ETFSync: jobs, QuantileWindow: 5, Now: now}
	currentAPI := recoveryAPI(t, service)
	for _, date := range dates {
		if err := service.PublishClose(ctx, date); err != nil {
			t.Fatal(err)
		}
		before := dailyFromHTTP(t, currentAPI, date)
		if before.Close == nil || before.Close.Cards[0].Volatility == nil || *before.Close.Cards[0].Volatility != 0 {
			t.Fatalf("initial flat result missing %s: %+v", date, before)
		}
	}
	// Correct Jan 3 through the actual historical HTTP entry, with Jan 6/7
	// already stored. The immutable 14:45 price on Jan 3 remains exactly 100.
	source.mu.Lock()
	bar := &source.bars[core.RotationCodes[0]][0]
	bar.Open, bar.High, bar.Low, bar.Close = 110, 110, 110, 110
	source.mu.Unlock()
	body, _ = json.Marshal(map[string]any{"codes": []string{core.RotationCodes[0]}, "mode": "historical", "startDate": "20250103", "endDate": "20250103"})
	captureHTTP(t, "POST", etfAPI, string(body), 202, &accepted)
	waitETFBatch(t, etfAPI, accepted.Batch.ID, "succeeded")
	waiting := dailyFromHTTP(t, currentAPI, "20250103")
	if waiting.Close == nil || waiting.CloseState.Status != "updating" || *waiting.Close.Cards[0].Price != 100 {
		t.Fatalf("raw correction exposed an incomplete result: %+v", waiting)
	}
	publishCtx, stopPublisher := context.WithCancel(context.Background())
	publisherDone := make(chan struct{})
	go func() { defer close(publisherDone); service.Serve(publishCtx) }()
	t.Cleanup(func() { stopPublisher(); <-publisherDone })
	until = time.Now().Add(8 * time.Second)
	for {
		complete := true
		for _, date := range dates {
			view := dailyFromHTTP(t, currentAPI, date)
			complete = complete && view.Close != nil && view.CloseState.Status == "ready"
		}
		if complete {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("history did not republish: Jan3=%+v Jan6=%+v", dailyFromHTTP(t, currentAPI, "20250103").CloseState, dailyFromHTTP(t, currentAPI, "20250106").CloseState)
		}
		time.Sleep(20 * time.Millisecond)
	}
	corrected := dailyFromHTTP(t, currentAPI, "20250103")
	if *corrected.Close.Cards[0].Price != 110 || corrected.Close.Cards[0].Score == nil || *corrected.Close.Cards[0].Score <= 0 || corrected.PriceSlippage[0].Bps == nil || *corrected.PriceSlippage[0].Bps != 1000 {
		t.Fatalf("corrected close and known 1000 bps difference missing: %+v", corrected)
	}
	for _, date := range []string{"20250106", "20250107"} {
		after := dailyFromHTTP(t, currentAPI, date)
		if after.Close.Cards[0].Volatility == nil || *after.Close.Cards[0].Volatility <= 0 || after.Reference != nil {
			t.Fatalf("later dependent metrics unchanged or reference invented on %s: %+v", date, after)
		}
	}
	preserved, _ := json.Marshal(corrected.Reference)
	if string(frozen) != string(preserved) || realtime.count() != 4 {
		t.Fatal("historical correction changed frozen reference or fetched current quotes")
	}
}

type heldCorrectionSource struct {
	*correctionSource
	entered chan struct{}
	release chan struct{}
}

func (f *heldCorrectionSource) FetchAdj(ctx context.Context, code, from, to string) ([]core.AdjFactor, error) {
	if from <= "20250102" && to >= "20250102" {
		select {
		case f.entered <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.release:
		}
	}
	return f.correctionSource.FetchAdj(ctx, code, from, to)
}

func TestCorrectionHTTPCancelledHistoricalFactorCannotBeSkippedByLaterIncrementalSync(t *testing.T) {
	db, _, _ := seedReferenceScenario(t, "20250103")
	code := core.RotationCodes[0]
	// Baseline factor at the old date means a later incremental request starts
	// after that date and cannot complete its cancelled factor correction.
	raw := store.NewMySQLStore(db)
	if _, err := raw.UpsertAdjFactors(context.Background(), []core.AdjFactor{{TsCode: code, TradeDate: "20250102", AdjFactor: 1}}); err != nil {
		t.Fatal(err)
	}
	source := &heldCorrectionSource{
		correctionSource: &correctionSource{
			bars:    map[string][]core.Bar{code: {{TsCode: code, TradeDate: "20250102", Open: 110, High: 110, Low: 110, Close: 110}, {TsCode: code, TradeDate: "20250103", Open: 100, High: 100, Low: 100, Close: 100}}},
			factors: map[string][]core.AdjFactor{code: {{TsCode: code, TradeDate: "20250102", AdjFactor: 2}, {TsCode: code, TradeDate: "20250103", AdjFactor: 1}}},
		}, entered: make(chan struct{}, 1), release: make(chan struct{}),
	}
	now := func() time.Time { return time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC) }
	jobs := &syncrun.ETFService{Store: syncrun.NewETFStore(db), Source: source, DefaultStart: "20241101", ChunkDays: 31, Now: now}
	service := &Service{DB: db, ETFSync: jobs, Calendar: fixtureCalendar{}, QuantileWindow: 5, Now: now}
	if err := service.PublishClose(context.Background(), "20250102"); err != nil {
		t.Fatal(err)
	}
	api := recoveryAPI(t, service)
	original := dailyFromHTTP(t, api, "20250102")
	saved, _ := json.Marshal(original.Close)
	etfAPI := etfRecoveryAPI(t, jobs)
	var batch struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	historyBody, _ := json.Marshal(map[string]any{"codes": []string{code}, "mode": "historical", "startDate": "20250102", "endDate": "20250102"})
	captureHTTP(t, "POST", etfAPI, string(historyBody), 202, &batch)
	cancelledID := batch.Batch.ID
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); jobs.Serve(ctx) }()
	t.Cleanup(func() { stop(); <-done })
	select {
	case <-source.entered:
	case <-time.After(4 * time.Second):
		t.Fatal("historical factor request did not begin")
	}
	captureHTTP(t, "POST", etfAPI+"/"+cancelledID+"/cancel", "", 200, nil)
	close(source.release)
	waitETFBatch(t, etfAPI, cancelledID, "cancelled")
	// A read must reflect the terminal source task even before the publisher
	// gets its next turn; reading must not itself trigger a recomputation.
	cancelled := dailyFromHTTP(t, api, "20250102")
	cancelledPayload, _ := json.Marshal(cancelled.Close)
	if cancelled.CloseState.Status != "failed" || string(cancelledPayload) != string(saved) {
		t.Fatalf("cancelled correction still shown as active before publication: %+v", cancelled.CloseState)
	}
	incremental, _ := json.Marshal(map[string]any{"codes": []string{code}})
	captureHTTP(t, "POST", etfAPI, string(incremental), 202, &batch)
	waitETFBatch(t, etfAPI, batch.Batch.ID, "succeeded")
	// Run the same public maintenance pass used by Serve, then observe only HTTP.
	if err := service.RefreshDaily(ctx); err != nil {
		t.Fatal(err)
	}
	blocked := dailyFromHTTP(t, api, "20250102")
	retained, _ := json.Marshal(blocked.Close)
	if blocked.CloseState.Status != "failed" || string(retained) != string(saved) {
		t.Fatalf("incremental sync published a cancelled partial historical correction: status=%s old=%s new=%s", blocked.CloseState.Status, saved, retained)
	}
	// A newly requested full historical range can repair the cancelled work;
	// this does not resume or change the terminal cancelled execution.
	captureHTTP(t, "POST", etfAPI, string(historyBody), 202, &batch)
	waitETFBatch(t, etfAPI, batch.Batch.ID, "succeeded")
	if err := service.RefreshDaily(ctx); err != nil {
		t.Fatal(err)
	}
	repaired := dailyFromHTTP(t, api, "20250102")
	if repaired.CloseState.Status != "ready" || repaired.Close == nil || *repaired.Close.Cards[0].Price != 110 {
		t.Fatalf("replacement historical request did not publish: %+v", repaired)
	}
	waitETFBatch(t, etfAPI, cancelledID, "cancelled")
}
