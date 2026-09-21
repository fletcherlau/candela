package rotation

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zeromicro/go-zero/rest/router"
	"net/http/httptest"
	"strings"
	"sync"
	"syncer/internal/core"
	"syncer/internal/middleware"
	"syncer/internal/syncrun"
	"testing"
	"time"
)

type recoveryQuoteSource struct {
	mu      sync.Mutex
	failed  bool
	calls   map[string]int
	entered chan struct{}
	release chan struct{}
}

func (f *recoveryQuoteSource) FetchDaily(ctx context.Context, code, from, to string) ([]core.Bar, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[code+"/daily"]++
	return []core.Bar{{TsCode: code, TradeDate: to, Open: 100, High: 100, Low: 100, Close: 100}}, nil
}
func (f *recoveryQuoteSource) FetchAdj(ctx context.Context, code, from, to string) ([]core.AdjFactor, error) {
	f.mu.Lock()
	f.calls[code+"/adj"]++
	failed, entered, release := f.failed, f.entered, f.release
	f.mu.Unlock()
	if code == "513100.SH" {
		if failed {
			return nil, fmt.Errorf("isolated source failure")
		}
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
			}
		}
	}
	return []core.AdjFactor{{TsCode: code, TradeDate: to, AdjFactor: 1}}, nil
}

func etfRecoveryAPI(t *testing.T, jobs *syncrun.ETFService) string {
	t.Helper()
	r := router.NewRouter()
	for _, route := range jobs.Routes(middleware.NewApiKeyAuthMiddleware("fixture-only").Handle) {
		if err := r.Handle(route.Method, route.Path, route.Handler); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server.URL + "/api/v1/data/etf-syncs"
}
func waitETFBatch(t *testing.T, api, id, state string) syncrun.ETFBatch {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for {
		var v struct {
			Batch syncrun.ETFBatch `json:"batch"`
		}
		captureHTTP(t, "GET", api+"/"+id, "", 200, &v)
		if v.Batch.State == state {
			return v.Batch
		}
		if time.Now().After(until) {
			t.Fatalf("batch did not become %s: %+v", state, v)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestRecoveryHTTPCloseRetriesOneFailedFactorAndPublishesOriginalDay(t *testing.T) {
	db, service, _ := seedReferenceScenario(t, "20250103")
	source := &recoveryQuoteSource{failed: true, calls: map[string]int{}}
	jobs := &syncrun.ETFService{Store: syncrun.NewETFStore(db), Source: source, DefaultStart: "20240101", ChunkDays: 366, Now: func() time.Time { return time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC) }}
	service.ETFSync = jobs
	api := recoveryAPI(t, service)
	etfAPI := etfRecoveryAPI(t, jobs)
	body, _ := json.Marshal(map[string]any{"codes": core.RotationCodes})
	var original struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	captureHTTP(t, "POST", etfAPI, string(body), 202, &original)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); jobs.Serve(ctx) }()
	t.Cleanup(func() { stop(); <-done })
	failed := waitETFBatch(t, etfAPI, original.Batch.ID, "partial")
	if failed.Success != 3 || failed.Failed != 1 {
		t.Fatal(failed)
	}
	if err := service.PublishClose(context.Background(), "20250103"); err != nil {
		t.Fatal(err)
	}
	var daily DailyView
	captureHTTP(t, "GET", api+"/api/v1/rotation/daily?tradeDate=20250103", "", 200, &daily)
	if daily.Close != nil || daily.CloseState.Available != 3 {
		t.Fatalf("incomplete close progress not exposed: %+v", daily)
	}
	source.mu.Lock()
	source.failed = false
	source.mu.Unlock()
	jobs.Now = func() time.Time { return time.Date(2025, 1, 10, 10, 0, 0, 0, time.UTC) }
	later := &Service{DB: db, Calendar: fixtureCalendar{}, ETFSync: jobs, QuantileWindow: 5, Now: jobs.Now}
	laterAPI := recoveryAPI(t, later)
	var accepted, duplicate struct {
		Run          RecoveryRun `json:"run"`
		Deduplicated bool        `json:"deduplicated"`
	}
	captureHTTP(t, "POST", laterAPI+closeRecoveryPath+"/"+original.Batch.ID+"/retry", "", 202, &accepted)
	captureHTTP(t, "POST", laterAPI+closeRecoveryPath+"/"+original.Batch.ID+"/retry", "", 200, &duplicate)
	if !duplicate.Deduplicated || accepted.Run.ID != duplicate.Run.ID || accepted.Run.TradeDate != "20250103" {
		t.Fatal("close recovery target changed", accepted, duplicate)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() { defer close(runDone); later.ServeRecoveries(runCtx) }()
	t.Cleanup(func() { cancel(); <-runDone })
	finished := waitRecovery(t, laterAPI, accepted.Run.ID, "succeeded")
	if finished.SyncBatchID == "" || finished.SyncBatchID == original.Batch.ID {
		t.Fatal("child sync batch not recorded", finished)
	}
	child := waitETFBatch(t, etfAPI, finished.SyncBatchID, "succeeded")
	if child.Total != 1 || child.EndDate != "20250103" || child.ParentID != original.Batch.ID || child.Items[0].Code != "513100.SH" {
		t.Fatal("recovery did not reuse original failed scope", child)
	}
	captureHTTP(t, "GET", laterAPI+"/api/v1/rotation/daily?tradeDate=20250103", "", 200, &daily)
	if daily.Close == nil || len(daily.Close.Cards) != 4 || daily.Close.TradeDate != "20250103" {
		t.Fatal("four-object close was not published", daily)
	}
	for _, card := range daily.Close.Cards {
		if card.Rank == nil || card.Score == nil {
			t.Fatal("group metrics not recalculated", card)
		}
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	for _, code := range core.RotationCodes {
		if source.calls[code+"/daily"] != 1 {
			t.Fatal("completed daily work fetched again", source.calls)
		}
		want := 1
		if code == "513100.SH" {
			want = 2
		}
		if source.calls[code+"/adj"] != want {
			t.Fatal("successful factor work fetched again", source.calls)
		}
	}
	captureHTTP(t, "POST", laterAPI+closeRecoveryPath+"/"+original.Batch.ID+"/retry", `{"tradeDate":"20250110"}`, 400, nil)
	if !strings.Contains(finished.TargetAt, "2025-01-03") {
		t.Fatal("close target changed", finished)
	}
}

func TestRecoveryHTTPCloseResumesSameBatchAfterServiceStops(t *testing.T) {
	db, service, _ := seedReferenceScenario(t, "20250103")
	source := &recoveryQuoteSource{failed: true, calls: map[string]int{}}
	now := func() time.Time { return time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC) }
	jobs := &syncrun.ETFService{Store: syncrun.NewETFStore(db), Source: source, DefaultStart: "20240101", ChunkDays: 366, Now: now}
	service.ETFSync = jobs
	service.Now = now
	api, etfAPI := recoveryAPI(t, service), etfRecoveryAPI(t, jobs)
	var original struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	body, _ := json.Marshal(map[string]any{"codes": core.RotationCodes})
	captureHTTP(t, "POST", etfAPI, string(body), 202, &original)
	syncCtx, stopSync := context.WithCancel(context.Background())
	syncDone := make(chan struct{})
	go func() { defer close(syncDone); jobs.Serve(syncCtx) }()
	t.Cleanup(func() { stopSync(); <-syncDone })
	waitETFBatch(t, etfAPI, original.Batch.ID, "partial")
	entered, release := make(chan struct{}, 1), make(chan struct{})
	source.mu.Lock()
	source.failed = false
	source.entered = entered
	source.release = release
	source.mu.Unlock()
	var accepted struct {
		Run RecoveryRun `json:"run"`
	}
	captureHTTP(t, "POST", api+closeRecoveryPath+"/"+original.Batch.ID+"/retry", "", 202, &accepted)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); service.ServeRecoveries(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not reach failed source step")
	}
	running := waitRecovery(t, api, accepted.Run.ID, "running")
	if running.SyncBatchID == "" {
		t.Fatal("recovery lost durable sync identity", running)
	}
	cancel()
	<-done
	queued := waitRecovery(t, api, accepted.Run.ID, "queued")
	if queued.SyncBatchID != running.SyncBatchID || queued.Stage != "recovering" {
		t.Fatal("interruption lost fixed scope", queued)
	}
	// The independent ETF task survives the recovery process, and a rebuilt
	// coordinator follows that same task instead of submitting a new batch.
	rebuilt := &Service{DB: db, Calendar: fixtureCalendar{}, ETFSync: jobs, QuantileWindow: 5, Now: func() time.Time { return time.Date(2025, 1, 10, 10, 0, 0, 0, time.UTC) }}
	rebuiltAPI := recoveryAPI(t, rebuilt)
	var duplicate struct {
		Run          RecoveryRun `json:"run"`
		Deduplicated bool        `json:"deduplicated"`
	}
	captureHTTP(t, "POST", rebuiltAPI+closeRecoveryPath+"/"+original.Batch.ID+"/retry", "", 200, &duplicate)
	if !duplicate.Deduplicated || duplicate.Run.ID != accepted.Run.ID {
		t.Fatal("restart duplicated recovery", duplicate)
	}
	restartCtx, stopRestart := context.WithCancel(context.Background())
	restartDone := make(chan struct{})
	go func() { defer close(restartDone); rebuilt.ServeRecoveries(restartCtx) }()
	t.Cleanup(func() { stopRestart(); <-restartDone })
	close(release)
	published := waitRecovery(t, rebuiltAPI, accepted.Run.ID, "succeeded")
	if published.SyncBatchID != running.SyncBatchID || published.Recoveries != 1 || published.TradeDate != "20250103" {
		t.Fatal("recovery rebuilt a different target", published)
	}
	var daily DailyView
	captureHTTP(t, "GET", rebuiltAPI+"/api/v1/rotation/daily?tradeDate=20250103", "", 200, &daily)
	if daily.Close == nil || daily.Close.Available != 4 {
		t.Fatal("restarted recovery did not publish", daily)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.calls["513100.SH/adj"] != 2 || source.calls["513100.SH/daily"] != 1 {
		t.Fatal("restart repeated completed steps", source.calls)
	}
}

func TestRecoveryHTTPCloseFailedChildRetainsSyncFailureAndCanRetry(t *testing.T) {
	db, service, _ := seedReferenceScenario(t, "20250103")
	source := &recoveryQuoteSource{failed: true, calls: map[string]int{}}
	now := func() time.Time { return time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC) }
	jobs := &syncrun.ETFService{Store: syncrun.NewETFStore(db), Source: source, DefaultStart: "20240101", ChunkDays: 366, Now: now}
	service.ETFSync = jobs
	service.Now = now
	api, etfAPI := recoveryAPI(t, service), etfRecoveryAPI(t, jobs)
	var original struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	body, _ := json.Marshal(map[string]any{"codes": core.RotationCodes})
	captureHTTP(t, "POST", etfAPI, string(body), 202, &original)
	syncCtx, stopSync := context.WithCancel(context.Background())
	syncDone := make(chan struct{})
	go func() { defer close(syncDone); jobs.Serve(syncCtx) }()
	t.Cleanup(func() { stopSync(); <-syncDone })
	waitETFBatch(t, etfAPI, original.Batch.ID, "partial")
	var accepted, child struct {
		Run RecoveryRun `json:"run"`
	}
	captureHTTP(t, "POST", api+closeRecoveryPath+"/"+original.Batch.ID+"/retry", "", 202, &accepted)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); service.ServeRecoveries(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	failed := waitRecovery(t, api, accepted.Run.ID, "failed")
	if failed.Stage != "synchronization_failed" || !strings.Contains(failed.Message, "同步") {
		t.Fatal("source failure was mislabeled as calculation failure", failed)
	}
	failedBatch := waitETFBatch(t, etfAPI, failed.SyncBatchID, "failed")
	if failedBatch.Total != 1 || failedBatch.Items[0].DailyCheckpoint != "20250103" {
		t.Fatal("failed retry lost completed steps", failedBatch)
	}
	source.mu.Lock()
	source.failed = false
	source.mu.Unlock()
	captureHTTP(t, "POST", api+recoveryPath+"/"+failed.ID+"/retry", "", 202, &child)
	finished := waitRecovery(t, api, child.Run.ID, "succeeded")
	if finished.ParentID != failed.ID || finished.OriginID != original.Batch.ID || finished.TargetAt != failed.TargetAt {
		t.Fatal("retry lineage lost", finished)
	}
	finalBatch := waitETFBatch(t, etfAPI, finished.SyncBatchID, "succeeded")
	if finalBatch.ParentID != failed.SyncBatchID || finalBatch.EndDate != "20250103" || finalBatch.Total != 1 {
		t.Fatal("recovery repeated the original failed batch instead of its failed child", finalBatch)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.calls["513100.SH/adj"] != 3 || source.calls["513100.SH/daily"] != 1 {
		t.Fatal("retry repeated completed work", source.calls)
	}
}
