package rotation

import (
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"syncer/internal/core"
	"syncer/internal/middleware"
	"syncer/internal/store"
	"syncer/internal/syncrun"
	"testing"
	"time"
)

type continuousCorrectionScenario struct {
	service                    *Service
	source                     *correctionSource
	jobs                       *syncrun.ETFService
	dailyURL, etfURL, rangeURL string
	frozenReference            []byte
	realtime                   *captureSource
}

// Initial source history is a labelled flat-price fixture with enough data for
// the unchanged 1200-day backtest warmup. All corrections use the real ETF API.
func seedContinuousCorrection(t *testing.T, closeDates ...string) continuousCorrectionScenario {
	t.Helper()
	db, captureService, realtime := seedReferenceScenario(t, "20250107")
	raw := store.NewMySQLStore(db)
	source := &correctionSource{bars: map[string][]core.Bar{}, factors: map[string][]core.AdjFactor{}}
	for i, code := range core.RotationCodes {
		for d := day("20190101"); !d.After(day("20250113")); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			date := d.Format("20060102")
			price := float64(100 + i)
			factor := 1.0
			if date == "20250113" {
				price = 900
				factor = 9
			} // Future records must not enter Jan 10 results.
			source.bars[code] = append(source.bars[code], core.Bar{TsCode: code, TradeDate: date, Open: price, High: price, Low: price, Close: price})
			source.factors[code] = append(source.factors[code], core.AdjFactor{TsCode: code, TradeDate: date, AdjFactor: factor})
		}
		if _, err := raw.UpsertDaily(context.Background(), source.bars[code]); err != nil {
			t.Fatal(err)
		}
		if _, err := raw.UpsertAdjFactors(context.Background(), source.factors[code]); err != nil {
			t.Fatal(err)
		}
	}
	jobs := &syncrun.ETFService{Store: syncrun.NewETFStore(db), Source: source, DefaultStart: "20190101", ChunkDays: 366, Now: func() time.Time { return time.Date(2025, 1, 13, 10, 0, 0, 0, time.UTC) }}
	etfURL := etfRecoveryAPI(t, jobs)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); jobs.Serve(ctx) }()
	t.Cleanup(func() { stop(); <-done })
	body, _ := json.Marshal(map[string]any{"codes": core.RotationCodes})
	var accepted struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	captureHTTP(t, "POST", etfURL, string(body), 202, &accepted)
	waitETFBatch(t, etfURL, accepted.Batch.ID, "succeeded")
	// The reference clock and the result clock are separate immutable fixtures.
	captureURL := recoveryAPI(t, captureService)
	captureHTTP(t, "POST", captureURL+capturePath, `{"tradeDate":"20250107"}`, 202, nil)
	startCaptureWorker(t, captureService)
	startReferenceWorker(t, captureService)
	waitCapture(t, captureURL+capturePath, "20250107", "captured")
	until := time.Now().Add(4 * time.Second)
	var frozen []byte
	for {
		view := dailyFromHTTP(t, captureURL, "20250107")
		if view.Reference != nil {
			frozen, _ = json.Marshal(view.Reference)
			break
		}
		if time.Now().After(until) {
			t.Fatal("reference did not publish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	service := &Service{DB: db, Calendar: fixtureCalendar{}, ETFSync: jobs, QuantileWindow: 5, Now: func() time.Time { return time.Date(2025, 1, 10, 10, 0, 0, 0, time.UTC) }}
	dailyURL := recoveryAPI(t, service)
	if len(closeDates) == 0 {
		closeDates = []string{"20250106", "20250107", "20250108", "20250110"}
	}
	for _, date := range closeDates {
		if err := service.PublishClose(context.Background(), date); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(middleware.NewApiKeyAuthMiddleware("fixture-only").Handle(service.RangeHandler().ServeHTTP))
	t.Cleanup(server.Close)
	return continuousCorrectionScenario{service, source, jobs, dailyURL, etfURL, server.URL + "/api/v1/rotation/backtest/range?start=20250106&end=20250110", frozen, realtime}
}

func TestCorrectionHTTPFactorRepublishesContinuousBacktestAndSelectedRange(t *testing.T) {
	f := seedContinuousCorrection(t)
	var before RangeView
	captureHTTP(t, "GET", f.rangeURL, "", 200, &before)
	if before.Result == nil || before.Range == nil || before.Result.End != "20250110" || before.Range.Start != "20250106" || before.Range.End != "20250110" || before.Range.Gain == nil || *before.Range.Gain != 0 {
		t.Fatalf("initial as-of flat result invalid: %+v", before)
	}
	if before.Result.CostBPS == nil || *before.Result.CostBPS != 10 {
		t.Fatal("fee basis changed")
	}
	for _, row := range before.Result.Days {
		// Flat prices retain the first tied leader after the initial 10 bps
		// purchase: one unit becomes 0.999000999000999 net of fees.
		if row.NAV == nil || math.Abs(*row.NAV-0.999000999000999) > 1e-12 || (row.Holding == nil || *row.Holding != core.RotationCodes[0]) {
			t.Fatal("flat baseline lost its existing first-purchase fee or holding", row)
		}
	}
	// Correct one old factor while leaving unadjusted prices unchanged.
	code := core.RotationCodes[3]
	f.source.mu.Lock()
	for i := range f.source.factors[code] {
		if f.source.factors[code][i].TradeDate == "20250107" {
			f.source.factors[code][i].AdjFactor = 2
		}
	}
	f.source.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"codes": []string{code}, "mode": "historical", "startDate": "20250107", "endDate": "20250107"})
	var accepted struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	captureHTTP(t, "POST", f.etfURL, string(body), 202, &accepted)
	waitETFBatch(t, f.etfURL, accepted.Batch.ID, "succeeded")
	var waiting RangeView
	captureHTTP(t, "GET", f.rangeURL, "", 200, &waiting)
	oldPayload, _ := json.Marshal(before.Result)
	waitingPayload, _ := json.Marshal(waiting.Result)
	if waiting.Status != "pending" || string(oldPayload) != string(waitingPayload) {
		t.Fatal("raw correction replaced complete backtest prematurely", waiting.Status)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.service.Serve(ctx) }()
	t.Cleanup(func() { stop(); <-done })
	until := time.Now().Add(8 * time.Second)
	var after RangeView
	for {
		captureHTTP(t, "GET", f.rangeURL, "", 200, &after)
		if after.Status == "ready" && after.Range != nil && after.Range.Gain != nil && *after.Range.Gain < 0 {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("corrected continuous backtest did not publish: %+v", after)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if after.Range.Start != "20250106" || after.Range.End != "20250110" || after.Result.End != "20250110" || len(after.Result.Days) != len(before.Result.Days) || after.Result.Version != before.Result.Version || *after.Result.CostBPS != 10 {
		t.Fatal("correction changed selected range, continuous history or algorithm")
	}
	// Known source ratios: Jan 7's adjusted benchmark doubles, then returns to 1.
	seenSpike, seenLater := false, false
	for _, row := range after.Result.Days {
		if row.Date == "20250107" {
			seenSpike = true
			if row.Benchmarks[3] == nil || *row.Benchmarks[3] != 2 || row.Cost == nil || *row.Cost <= 0 {
				t.Fatal("factor or existing trading cost not reflected", row)
			}
		}
		if row.Date == "20250108" {
			seenLater = true
			if row.Benchmarks[3] == nil || *row.Benchmarks[3] != 1 || row.NAV == nil || *row.NAV >= 0.999000999000999 {
				t.Fatal("subsequent continuous holdings did not change", row)
			}
		}
	}
	if !seenSpike || !seenLater {
		t.Fatal("continuous days disappeared")
	}
	corrected := dailyFromHTTP(t, f.dailyURL, "20250107")
	card := corrected.Close.Cards[3]
	if card.Price == nil || *card.Price != 103 || card.Rank == nil || *card.Rank != 1 || card.Score == nil || *card.Score <= 0 || corrected.PriceSlippage[3].Bps == nil || *corrected.PriceSlippage[3].Bps != 0 {
		t.Fatalf("factor correction did not update metrics independently of raw price: %+v", corrected)
	}
	preserved, _ := json.Marshal(corrected.Reference)
	if string(preserved) != string(f.frozenReference) || f.realtime.count() != 4 {
		t.Fatal("factor correction changed original reference")
	}
}

func TestCorrectionHTTPPublishesCloseForRecordedReferenceOnlyDay(t *testing.T) {
	f := seedContinuousCorrection(t, "20250106", "20250108", "20250110")
	before := dailyFromHTTP(t, f.dailyURL, "20250107")
	if before.Reference == nil || before.Close != nil {
		t.Fatal("fixture must have a reference-only historical day")
	}
	code := core.RotationCodes[0]
	f.source.mu.Lock()
	for i := range f.source.bars[code] {
		if f.source.bars[code][i].TradeDate == "20250107" {
			b := &f.source.bars[code][i]
			b.Open, b.High, b.Low, b.Close = 110, 110, 110, 110
		}
	}
	f.source.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"codes": []string{code}, "mode": "historical", "startDate": "20250107", "endDate": "20250107"})
	var accepted struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	captureHTTP(t, "POST", f.etfURL, string(body), 202, &accepted)
	waitETFBatch(t, f.etfURL, accepted.Batch.ID, "succeeded")
	if err := f.service.RefreshDaily(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := dailyFromHTTP(t, f.dailyURL, "20250107")
	if after.Close == nil || after.CloseState.Status != "ready" || *after.Close.Cards[0].Price != 110 || after.PriceSlippage[0].Bps == nil || *after.PriceSlippage[0].Bps != 1000 {
		t.Fatalf("recorded reference-only day was skipped: %+v", after)
	}
	preserved, _ := json.Marshal(after.Reference)
	if string(preserved) != string(f.frozenReference) || f.realtime.count() != 4 {
		t.Fatal("historical publication replaced reference")
	}
	// A raw-data date without any recorded publication/capture is not archived.
	if unrecorded := dailyFromHTTP(t, f.dailyURL, "20250109"); unrecorded.Close != nil || unrecorded.Reference != nil {
		t.Fatal("unrecorded raw date became a fabricated archive entry")
	}
}

func TestCorrectionHTTPShowsComputationAndRejectsSupersededClose(t *testing.T) {
	f := seedContinuousCorrection(t)
	before := dailyFromHTTP(t, f.dailyURL, "20250107")
	saved, _ := json.Marshal(before.Close)
	correct := func(price float64) {
		t.Helper()
		code := core.RotationCodes[0]
		f.source.mu.Lock()
		for i := range f.source.bars[code] {
			if f.source.bars[code][i].TradeDate == "20250107" {
				b := &f.source.bars[code][i]
				b.Open, b.High, b.Low, b.Close = price, price, price, price
			}
		}
		f.source.mu.Unlock()
		body, _ := json.Marshal(map[string]any{"codes": []string{code}, "mode": "historical", "startDate": "20250107", "endDate": "20250107"})
		var accepted struct {
			Batch syncrun.ETFBatch `json:"batch"`
		}
		captureHTTP(t, "POST", f.etfURL, string(body), 202, &accepted)
		waitETFBatch(t, f.etfURL, accepted.Batch.ID, "succeeded")
	}
	correct(110)
	waiting := dailyFromHTTP(t, f.dailyURL, "20250107")
	if waiting.CloseState.Status != "updating" {
		t.Fatalf("raw update not visible: %+v", waiting.CloseState)
	}
	// Pause only the external calendar boundary, after raw input acceptance.
	if _, err := f.service.DB.Exec("DELETE FROM rotation_calendar WHERE cal_date='20250107'"); err != nil {
		t.Fatal(err)
	}
	calendar := &waitingCalendar{entered: make(chan struct{}), resume: make(chan struct{})}
	f.service.Calendar = calendar
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.service.PublishClose(ctx, "20250107") }()
	resumed := false
	defer func() {
		if !resumed {
			close(calendar.resume)
			<-done
		}
	}()
	select {
	case <-calendar.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("calculation did not reach calendar boundary")
	}
	computing := dailyFromHTTP(t, f.dailyURL, "20250107")
	retained, _ := json.Marshal(computing.Close)
	if computing.CloseState.Status != "computing" || string(retained) != string(saved) {
		t.Fatalf("calculation stage hidden or old complete result lost: %+v", computing.CloseState)
	}
	correct(120)
	superseded := dailyFromHTTP(t, f.dailyURL, "20250107")
	if superseded.CloseState.Status != "updating" {
		t.Fatal("old calculation reported current despite newer inputs")
	}
	close(calendar.resume)
	resumed = true
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	afterOld := dailyFromHTTP(t, f.dailyURL, "20250107")
	old, _ := json.Marshal(afterOld.Close)
	if string(old) != string(saved) || afterOld.CloseState.Status != "updating" {
		t.Fatal("superseded calculation replaced current result")
	}
	if err := f.service.RefreshDaily(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := dailyFromHTTP(t, f.dailyURL, "20250107")
	if after.CloseState.Status != "ready" || after.Close == nil || *after.Close.Cards[0].Price != 120 || *after.PriceSlippage[0].Bps != 2000 {
		t.Fatalf("newest input did not publish: %+v", after)
	}
	preserved, _ := json.Marshal(after.Reference)
	if string(preserved) != string(f.frozenReference) {
		t.Fatal("race changed immutable reference")
	}
}

func TestCorrectionHTTPHistoricalRecoveryPublishesDependentsAndBacktestWithWeekendEnd(t *testing.T) {
	f := seedContinuousCorrection(t)
	var before RangeView
	captureHTTP(t, "GET", f.rangeURL, "", 200, &before)
	code := core.RotationCodes[3]
	f.source.mu.Lock()
	for i := range f.source.factors[code] {
		if f.source.factors[code][i].TradeDate == "20250103" {
			f.source.factors[code][i].AdjFactor = 2
		}
	}
	f.source.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"codes": []string{code}, "mode": "historical", "startDate": "20250102", "endDate": "20250105"})
	var batch struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	captureHTTP(t, "POST", f.etfURL, string(body), 202, &batch)
	waitETFBatch(t, f.etfURL, batch.Batch.ID, "succeeded")
	var scopeBefore struct {
		Scope struct {
			Mode      string `json:"mode"`
			StartDate string `json:"startDate"`
			EndDate   string `json:"endDate"`
			Published bool   `json:"published"`
			ViewDate  string `json:"viewDate"`
		} `json:"scope"`
	}
	captureHTTP(t, "GET", f.dailyURL+recoveryPath+"/origins/close/"+batch.Batch.ID, "", 200, &scopeBefore)
	if scopeBefore.Scope.Mode != "historical" || scopeBefore.Scope.StartDate != "20250102" || scopeBefore.Scope.EndDate != "20250105" || scopeBefore.Scope.Published {
		t.Fatal("historical recovery scope/status not exposed", scopeBefore)
	}
	var accepted, duplicate struct {
		Run          RecoveryRun `json:"run"`
		Deduplicated bool        `json:"deduplicated"`
	}
	captureHTTP(t, "POST", f.dailyURL+closeRecoveryPath+"/"+batch.Batch.ID+"/retry", "", 202, &accepted)
	captureHTTP(t, "POST", f.dailyURL+closeRecoveryPath+"/"+batch.Batch.ID+"/retry", "", 200, &duplicate)
	if !duplicate.Deduplicated || duplicate.Run.ID != accepted.Run.ID || accepted.Run.TradeDate != "20250105" {
		t.Fatal("original range identity changed or duplicate recovery created")
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.service.ServeRecoveries(ctx) }()
	t.Cleanup(func() { stop(); <-done })
	finished := waitRecovery(t, f.dailyURL, accepted.Run.ID, "succeeded")
	if finished.SyncBatchID != batch.Batch.ID {
		t.Fatal("successful raw synchronization was needlessly restarted")
	}
	for _, date := range []string{"20250106", "20250107", "20250108", "20250110"} {
		view := dailyFromHTTP(t, f.dailyURL, date)
		if view.CloseState.Status != "ready" || view.Close == nil || view.Close.Cards[3].Volatility == nil || *view.Close.Cards[3].Volatility <= 0 {
			t.Fatalf("dependent recorded date %s not published by recovery: %+v", date, view)
		}
	}
	var after RangeView
	captureHTTP(t, "GET", f.rangeURL, "", 200, &after)
	if after.Status != "ready" || after.Result.End != "20250110" || after.Range.Start != "20250106" || after.Range.End != "20250110" || after.Range.Gain == nil || *after.Result.Days[len(after.Result.Days)-1].NAV >= *before.Result.Days[len(before.Result.Days)-1].NAV {
		t.Fatalf("recovery reported success without corrected continuous backtest: %+v", after)
	}
	preserved, _ := json.Marshal(dailyFromHTTP(t, f.dailyURL, "20250107").Reference)
	if string(preserved) != string(f.frozenReference) || f.realtime.count() != 4 {
		t.Fatal("recovery changed reference or requested new quotes")
	}
	captureHTTP(t, "GET", f.dailyURL+recoveryPath+"/origins/close/"+batch.Batch.ID, "", 200, &scopeBefore)
	if !scopeBefore.Scope.Published || scopeBefore.Scope.ViewDate != "20250106" {
		t.Fatal("management scope did not reflect all published results or linked to a weekend", scopeBefore)
	}
	if weekend := dailyFromHTTP(t, f.dailyURL, "20250105"); weekend.Close != nil {
		t.Fatal("weekend endpoint was invented as a trading day")
	}
}

func TestCorrectionHTTPHistoricalRecoveryCannotPublishAfterOwnershipChanges(t *testing.T) {
	f := seedContinuousCorrection(t)
	var before RangeView
	captureHTTP(t, "GET", f.rangeURL, "", 200, &before)
	old, _ := json.Marshal(before.Result)
	code := core.RotationCodes[3]
	f.source.mu.Lock()
	for i := range f.source.factors[code] {
		if f.source.factors[code][i].TradeDate == "20250107" {
			f.source.factors[code][i].AdjFactor = 2
		}
	}
	f.source.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"codes": []string{code}, "mode": "historical", "startDate": "20250107", "endDate": "20250107"})
	var batch struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	captureHTTP(t, "POST", f.etfURL, string(body), 202, &batch)
	waitETFBatch(t, f.etfURL, batch.Batch.ID, "succeeded")
	// Remove only an ancient calendar entry: daily windows remain available,
	// while the continuous backtest must wait at the external calendar seam.
	if _, err := f.service.DB.Exec("DELETE FROM rotation_calendar WHERE cal_date='20190102'"); err != nil {
		t.Fatal(err)
	}
	calendar := &waitingCalendar{entered: make(chan struct{}), resume: make(chan struct{})}
	f.service.Calendar = calendar
	var accepted struct {
		Run RecoveryRun `json:"run"`
	}
	captureHTTP(t, "POST", f.dailyURL+closeRecoveryPath+"/"+batch.Batch.ID+"/retry", "", 202, &accepted)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.service.ServeRecoveries(ctx) }()
	resumed := false
	t.Cleanup(func() {
		if !resumed {
			close(calendar.resume)
		}
		stop()
		<-done
	})
	select {
	case <-calendar.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("continuous calculation did not reach calendar seam")
	}
	var computing RangeView
	captureHTTP(t, "GET", f.rangeURL, "", 200, &computing)
	retained, _ := json.Marshal(computing.Result)
	if computing.Status != "computing" || string(retained) != string(old) {
		t.Fatal("complete backtest lost during historical recovery")
	}
	// Approved isolated execution fault: another executor owns and finishes
	// this recovery. Observe all consequences through public HTTP afterward.
	if _, err := f.service.DB.Exec("UPDATE rotation_recovery_run SET owner=owner+1,state='failed',stage='calculation_failed',message='replacement executor failed',lease_until=NULL WHERE id=?", accepted.Run.ID); err != nil {
		t.Fatal(err)
	}
	close(calendar.resume)
	resumed = true
	// The named calculation lock proves the old executor has finished; do not
	// infer this from a transient observation timeout or a recovery status.
	barrier, err := f.service.DB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var acquired int
	if err := barrier.QueryRowContext(context.Background(), "SELECT GET_LOCK('candela_etf_publication',10)").Scan(&acquired); err != nil || acquired != 1 {
		barrier.Close()
		t.Fatal("old calculation did not finish", err)
	}
	barrier.ExecContext(context.Background(), "SELECT RELEASE_LOCK('candela_etf_publication')")
	barrier.Close()
	var afterOld RangeView
	captureHTTP(t, "GET", f.rangeURL, "", 200, &afterOld)
	preserved, _ := json.Marshal(afterOld.Result)
	if string(preserved) != string(old) || afterOld.Status != "computing" {
		t.Fatal("displaced recovery published or changed current backtest state")
	}
	original := waitRecovery(t, f.dailyURL, accepted.Run.ID, "failed")
	if original.Message != "replacement executor failed" {
		t.Fatal("old recovery overwrote its successor")
	}
	var retry struct {
		Run RecoveryRun `json:"run"`
	}
	captureHTTP(t, "POST", f.dailyURL+recoveryPath+"/"+original.ID+"/retry", "", 202, &retry)
	finished := waitRecovery(t, f.dailyURL, retry.Run.ID, "succeeded")
	if finished.ParentID != original.ID || finished.SyncBatchID != batch.Batch.ID {
		t.Fatal("retry changed original raw scope")
	}
	var repaired RangeView
	captureHTTP(t, "GET", f.rangeURL, "", 200, &repaired)
	if repaired.Status != "ready" || repaired.Range.Gain == nil || *repaired.Range.Gain >= 0 {
		t.Fatal("current owner did not publish corrected continuous result")
	}
}
