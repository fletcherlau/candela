package rotation

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"syncer/internal/core"
	"syncer/internal/middleware"
	"syncer/internal/syncrun"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/rest/router"
)

// Opt-in browser acceptance uses production routes, workers and isolated MySQL.
// Only market sources, the business clock and a transient storage fault differ.
func TestRecoveryBrowserHarness(t *testing.T) {
	if os.Getenv("CANDELA_RECOVERY_BROWSER_TEST") != "1" {
		t.Skip("browser harness is opt-in")
	}
	db, service, referenceSource := seedReferenceScenario(t, "20250103")
	source := &recoveryQuoteSource{failed: true, calls: map[string]int{}}
	jobs := &syncrun.ETFService{Store: syncrun.NewETFStore(db), Source: source, DefaultStart: "20240101", ChunkDays: 366, Now: func() time.Time { return time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC) }}
	service.ETFSync = jobs
	api := recoveryAPI(t, service)
	captureHTTP(t, "POST", api+capturePath, `{"tradeDate":"20250103"}`, 202, nil)
	stopCapture := startCaptureWorker(t, service)
	waitCapture(t, api+capturePath, "20250103", "captured")
	stopCapture()
	service.Now = func() time.Time { return time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC) }
	captureHTTP(t, "POST", api+capturePath, `{"tradeDate":"20250102"}`, 202, nil)
	stopMissing := startCaptureWorker(t, service)
	waitCapture(t, api+capturePath, "20250102", "missing")
	stopMissing()
	if _, err := db.Exec("CREATE TRIGGER recovery_browser_failure BEFORE INSERT ON rotation_daily FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated publication failure'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DROP TRIGGER IF EXISTS recovery_browser_failure") })
	var accepted struct {
		Run RecoveryRun `json:"run"`
	}
	captureHTTP(t, "POST", api+capturePath+"/20250103/retry", "", 202, &accepted)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.ServeRecoveries(ctx)
	waitRecovery(t, api, accepted.Run.ID, "failed")
	if _, err := db.Exec("DROP TRIGGER recovery_browser_failure"); err != nil {
		t.Fatal(err)
	}
	etfAPI := etfRecoveryAPI(t, jobs)
	body, _ := json.Marshal(map[string]any{"codes": core.RotationCodes})
	var original struct {
		Batch syncrun.ETFBatch `json:"batch"`
	}
	captureHTTP(t, "POST", etfAPI, string(body), 202, &original)
	go jobs.Serve(ctx)
	waitETFBatch(t, etfAPI, original.Batch.ID, "partial")
	if err := service.PublishClose(ctx, "20250103"); err != nil {
		t.Fatal(err)
	}
	source.mu.Lock()
	source.failed = false
	source.mu.Unlock()
	crowdRecoveryHistory(t, service, api, 51)
	routes := router.NewRouter()
	auth := middleware.NewApiKeyAuthMiddleware("fixture-only").Handle
	all := append(append(append(service.RecoveryRoutes(auth), service.CaptureRoutes(auth)...), service.DailyRoutes(auth)...), jobs.Routes(auth)...)
	for _, route := range all {
		if err := routes.Handle(route.Method, route.Path, route.Handler); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal(http.ListenAndServe("127.0.0.1:18090", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test-state" {
			source.mu.Lock()
			defer source.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"referenceCalls": referenceSource.count(), "calls": source.calls, "batchId": original.Batch.ID, "recoveryId": accepted.Run.ID})
			return
		}
		routes.ServeHTTP(w, r)
	})))
}
