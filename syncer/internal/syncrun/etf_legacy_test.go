package syncrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syncer/internal/core"
	"syncer/internal/handler"
	"syncer/internal/middleware"
	"syncer/internal/schema"
	"syncer/internal/store"
	"syncer/internal/svc"
	"syncer/internal/types"
	"testing"
	"time"
)

func TestETFLegacyHTTPAndCloseReportWaitForSharedBatch(t *testing.T) {
	db := testDB(t)
	_, err := db.Exec("INSERT INTO instrument(ts_code,name) VALUES ('510880.SH','红利 ETF')")
	check(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	source := &etfFixture{failed: map[string]bool{}, calls: map[string]int{}, before: func(ctx context.Context, code, stage string) error {
		once.Do(func() { close(entered) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}}
	service := &ETFService{Store: NewETFStore(db), Source: source, DefaultStart: "20250101", ChunkDays: 31, Now: func() time.Time { return at("20250103") }}
	api := etfAPI(t, service)
	var batch struct {
		Batch ETFBatch `json:"batch"`
	}
	etfHTTP(t, "POST", api, `{}`, 202, &batch)
	st := store.NewMySQLStore(db)
	sc := &svc.ServiceContext{Syncer: service, Store: st, SignalComputer: core.NewSignalComputer(nil, st, 0, nil)}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/etf-daily", handler.SyncEtfDailyHandler(sc))
	mux.HandleFunc("/api/v1/rotation/close-report", handler.RotationCloseReportHandler(sc))
	legacy := httptest.NewServer(middleware.NewApiKeyAuthMiddleware("fixture-only").Handle(mux.ServeHTTP))
	defer legacy.Close()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); service.Serve(ctx) }()
	t.Cleanup(func() { stop(); <-done })
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	responses := make(chan []byte, 2)
	for _, path := range []string{"/api/v1/sync/etf-daily", "/api/v1/rotation/close-report"} {
		go func(path string) {
			req, _ := http.NewRequest("POST", legacy.URL+path, strings.NewReader(`{"tsCodes":["510880.SH"]}`))
			req.Header.Set("X-Api-Key", "fixture-only")
			req.Header.Set("Content-Type", "application/json")
			res, e := http.DefaultClient.Do(req)
			if e != nil {
				t.Error(e)
				return
			}
			defer res.Body.Close()
			if res.StatusCode != 200 {
				t.Errorf("legacy %s status %d", path, res.StatusCode)
				return
			}
			var raw json.RawMessage
			if e = json.NewDecoder(res.Body).Decode(&raw); e != nil {
				t.Error(e)
				return
			}
			responses <- raw
		}(path)
	}
	select {
	case value := <-responses:
		t.Fatalf("legacy returned before sync finished: %s", value)
	case <-time.After(150 * time.Millisecond):
	}
	// Replaying an interrupted additive migration must retain both accepted
	// ETF work and the original index queue's identity and state.
	index, _, err := NewStore(db).Submit(context.Background(), "incremental", at("20250103"))
	check(t, err)
	_, err = db.Exec("DELETE FROM schema_migration WHERE version=8")
	check(t, err)
	check(t, schema.Ensure(context.Background(), db))
	got, err := NewStore(db).Get(context.Background(), index.ID)
	check(t, err)
	if got.State != "queued" || got.Code != Code {
		t.Fatal("index work lost during additive migration", got)
	}
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case raw := <-responses:
			var direct types.SyncResp
			check(t, json.Unmarshal(raw, &direct))
			if direct.Total == 0 {
				var report types.CloseReportResp
				check(t, json.Unmarshal(raw, &report))
				direct = report.Sync
				if report.TradingDay || report.HasSnapshot {
					t.Fatal("historical source invented a current-day report", report)
				}
			}
			if direct.Success != 1 || direct.Total != 1 || len(direct.Results) != 1 {
				t.Fatalf("legacy result not preserved: %s", raw)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("legacy request did not finish")
		}
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.calls["510880.SH/daily"] != 1 || source.calls["510880.SH/adj"] != 1 {
		t.Fatal("cross-entry source work repeated", source.calls)
	}
	var batches struct {
		Batches []ETFBatch `json:"batches"`
	}
	etfHTTP(t, "GET", api, "", 200, &batches)
	if len(batches.Batches) != 1 || batches.Batches[0].ID != batch.Batch.ID || batches.Batches[0].State != "succeeded" {
		t.Fatal("legacy bypassed shared acceptance", batches)
	}
}
