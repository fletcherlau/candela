package rotation

import (
	"context"
	"encoding/json"
	"syncer/internal/core"
	"syncer/internal/syncrun"
	"testing"
	"time"
)

func TestHistoricalETFHTTPDoesNotClaimIncrementalCoverage(t *testing.T) {
	db, service, _ := seedReferenceScenario(t, "20250103")
	now := func() time.Time { return time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC) }
	source := &recoveryQuoteSource{calls: map[string]int{}}
	jobs := &syncrun.ETFService{Store: syncrun.NewETFStore(db), Source: source, DefaultStart: "20240101", ChunkDays: 366, Now: now}
	service.Now = now
	api := etfRecoveryAPI(t, jobs)
	viewAPI := recoveryAPI(t, service)
	body, _ := json.Marshal(map[string]any{"codes": core.RotationCodes, "mode": "historical", "startDate": "20250103", "endDate": "20250103"})
	var accepted struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	captureHTTP(t, "POST", api, string(body), 202, &accepted)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); jobs.Serve(ctx) }()
	t.Cleanup(func() { stop(); <-done })
	waitETFBatch(t, api, accepted.Batch.ID, "succeeded")
	if err := service.PublishClose(ctx, "20250103"); err != nil {
		t.Fatal(err)
	}
	var before DailyView
	captureHTTP(t, "GET", viewAPI+"/api/v1/rotation/daily?tradeDate=20250103", "", 200, &before)
	if before.Close != nil || before.CloseState.Available != 0 {
		t.Fatalf("narrow historical request claimed full coverage: %+v", before)
	}
	// The established incremental path can independently confirm the cutoff.
	body, _ = json.Marshal(map[string]any{"codes": core.RotationCodes})
	captureHTTP(t, "POST", api, string(body), 202, &accepted)
	waitETFBatch(t, api, accepted.Batch.ID, "succeeded")
	if err := service.PublishClose(ctx, "20250103"); err != nil {
		t.Fatal(err)
	}
	var after DailyView
	captureHTTP(t, "GET", viewAPI+"/api/v1/rotation/daily?tradeDate=20250103", "", 200, &after)
	if after.Close == nil || after.Close.Available != 4 {
		t.Fatalf("incremental coverage no longer publishes: %+v", after)
	}
}
