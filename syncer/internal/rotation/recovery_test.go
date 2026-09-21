package rotation

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/rest/router"
	"syncer/internal/middleware"
)

func recoveryAPI(t *testing.T, s *Service) string {
	t.Helper()
	routes := router.NewRouter()
	auth := middleware.NewApiKeyAuthMiddleware("fixture-only").Handle
	for _, route := range append(append(s.RecoveryRoutes(auth), s.CaptureRoutes(auth)...), s.DailyRoutes(auth)...) {
		if err := routes.Handle(route.Method, route.Path, route.Handler); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(routes)
	t.Cleanup(server.Close)
	return server.URL
}
func TestRecoveryHTTPPublishesSavedReferenceWithoutFetchingCurrentQuotes(t *testing.T) {
	db, service, source := seedReferenceScenario(t, "20250103")
	api := recoveryAPI(t, service)
	captureHTTP(t, "POST", api+capturePath, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, service)
	waitCapture(t, api+capturePath, "20250103", "captured")
	// A transient storage failure after inputs are frozen, before publication.
	if _, err := db.Exec("CREATE TRIGGER recovery_fixture_failure BEFORE INSERT ON rotation_daily FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated publication failure'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DROP TRIGGER IF EXISTS recovery_fixture_failure") })
	if err := service.publishReference(context.Background(), "20250103"); err == nil {
		t.Fatal("fixture must fail publication")
	}
	if _, err := db.Exec("DROP TRIGGER recovery_fixture_failure"); err != nil {
		t.Fatal(err)
	}
	later := &Service{DB: db, Realtime: source, QuantileWindow: 999, Now: func() time.Time { return time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC) }}
	laterAPI := recoveryAPI(t, later)
	var accepted, duplicate struct {
		Run          RecoveryRun `json:"run"`
		Deduplicated bool        `json:"deduplicated"`
	}
	captureHTTP(t, "POST", laterAPI+capturePath+"/20250103/retry", "", 202, &accepted)
	captureHTTP(t, "POST", laterAPI+capturePath+"/20250103/retry", "", 200, &duplicate)
	if accepted.Run.TradeDate != "20250103" || accepted.Run.State != "queued" || accepted.Run.ID != duplicate.Run.ID || !duplicate.Deduplicated {
		t.Fatalf("scope or deduplication lost: %+v %+v", accepted, duplicate)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); later.ServeRecoveries(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(4 * time.Second)
	for {
		var current struct {
			Run RecoveryRun `json:"run"`
		}
		captureHTTP(t, "GET", laterAPI+recoveryPath+"/"+accepted.Run.ID, "", 200, &current)
		if current.Run.State == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("recovery did not publish: %+v", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var view DailyView
	captureHTTP(t, "GET", laterAPI+"/api/v1/rotation/daily?tradeDate=20250103", "", 200, &view)
	if view.Reference == nil || view.Reference.QuantileWindow != 5 || source.count() != 4 {
		t.Fatal("recovery changed frozen context or fetched current quotes", view.Reference, source.count())
	}
	original, _ := json.Marshal(view.Reference)
	captureHTTP(t, "POST", laterAPI+capturePath+"/20250103/retry", "", 200, &duplicate)
	captureHTTP(t, "GET", laterAPI+"/api/v1/rotation/daily?tradeDate=20250103", "", 200, &view)
	current, _ := json.Marshal(view.Reference)
	if string(original) != string(current) || !strings.Contains(string(current), "reference_1445") {
		t.Fatal("published reference was replaced")
	}
}

func waitRecovery(t *testing.T, api, id, state string) RecoveryRun {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for {
		var result struct {
			Run RecoveryRun `json:"run"`
		}
		captureHTTP(t, "GET", api+recoveryPath+"/"+id, "", 200, &result)
		if result.Run.State == state {
			return result.Run
		}
		if time.Now().After(until) {
			t.Fatalf("recovery did not reach %s: %+v", state, result.Run)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestRecoveryHTTPMissingReferenceCannotBeReplacedWithCurrentQuote(t *testing.T) {
	_, service, source := seedReferenceScenario(t, "20250103")
	service.Now = func() time.Time { return time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC) }
	api := recoveryAPI(t, service)
	captureHTTP(t, "POST", api+capturePath, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, service)
	waitCapture(t, api+capturePath, "20250103", "missing")
	var accepted struct {
		Run RecoveryRun `json:"run"`
	}
	captureHTTP(t, "POST", api+capturePath+"/20250103/retry", "", 202, &accepted)
	if accepted.Run.State != "unavailable" || !strings.Contains(accepted.Run.Message, "原时点数据无法补取") || accepted.Run.TradeDate != "20250103" || source.count() != 0 {
		t.Fatalf("missing origin became recoverable/current: %+v calls=%d", accepted, source.count())
	}
	captureHTTP(t, "POST", api+capturePath+"/20250103/retry", `{"tradeDate":"20250110"}`, 400, nil)
	var view DailyView
	captureHTTP(t, "GET", api+"/api/v1/rotation/daily?tradeDate=20250103", "", 200, &view)
	if view.Reference != nil || source.count() != 0 {
		t.Fatal("invented reference")
	}
}
func TestRecoveryHTTPFailedAttemptCanRetryWithParentAndFrozenTarget(t *testing.T) {
	db, service, source := seedReferenceScenario(t, "20250103")
	api := recoveryAPI(t, service)
	captureHTTP(t, "POST", api+capturePath, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, service)
	waitCapture(t, api+capturePath, "20250103", "captured")
	if _, err := db.Exec("CREATE TRIGGER recovery_fixture_failure BEFORE INSERT ON rotation_daily FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated publication failure'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DROP TRIGGER IF EXISTS recovery_fixture_failure") })
	var root, child, duplicate struct {
		Run          RecoveryRun `json:"run"`
		Deduplicated bool        `json:"deduplicated"`
	}
	captureHTTP(t, "POST", api+capturePath+"/20250103/retry", "", 202, &root)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); service.ServeRecoveries(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	waitRecovery(t, api, root.Run.ID, "failed")
	if _, err := db.Exec("DROP TRIGGER recovery_fixture_failure"); err != nil {
		t.Fatal(err)
	}
	captureHTTP(t, "POST", api+recoveryPath+"/"+root.Run.ID+"/retry", "", 202, &child)
	captureHTTP(t, "POST", api+recoveryPath+"/"+root.Run.ID+"/retry", "", 200, &duplicate)
	if child.Run.ParentID != root.Run.ID || child.Run.TradeDate != "20250103" || child.Run.TargetAt != root.Run.TargetAt || child.Run.ID != duplicate.Run.ID || !duplicate.Deduplicated {
		t.Fatalf("retry lineage lost: %+v %+v", child, duplicate)
	}
	waitRecovery(t, api, child.Run.ID, "succeeded")
	if source.count() != 4 {
		t.Fatal("retry refetched current quotes", source.count())
	}
	original := waitRecovery(t, api, root.Run.ID, "failed")
	if original.Message == "" {
		t.Fatal("failure history lost")
	}
}

// The database barrier pauses a publication after it has read frozen inputs.
// A newer owner then finishes the attempt; the delayed old owner must neither
// publish its candidate nor change the observable newer terminal status.
func TestRecoveryHTTPLostOwnerCannotPublishFrozenReference(t *testing.T) {
	db, service, source := seedReferenceScenario(t, "20250103")
	api := recoveryAPI(t, service)
	captureHTTP(t, "POST", api+capturePath, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, service)
	waitCapture(t, api+capturePath, "20250103", "captured")
	gate, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	if _, err = gate.ExecContext(context.Background(), "SELECT GET_LOCK('recovery_fixture_gate',5)"); err != nil {
		t.Fatal(err)
	}
	defer gate.ExecContext(context.Background(), "SELECT RELEASE_LOCK('recovery_fixture_gate')")
	if _, err = db.Exec(`CREATE TRIGGER recovery_fixture_barrier BEFORE INSERT ON rotation_daily FOR EACH ROW BEGIN DO GET_LOCK('recovery_fixture_entered',0); DO GET_LOCK('recovery_fixture_gate',10); DO RELEASE_LOCK('recovery_fixture_gate'); DO RELEASE_LOCK('recovery_fixture_entered'); END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DROP TRIGGER IF EXISTS recovery_fixture_barrier") })
	var accepted struct {
		Run RecoveryRun `json:"run"`
	}
	captureHTTP(t, "POST", api+capturePath+"/20250103/retry", "", 202, &accepted)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); service.ServeRecoveries(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	until := time.Now().Add(4 * time.Second)
	for {
		var entered bool
		if err = db.QueryRow("SELECT IS_USED_LOCK('recovery_fixture_entered') IS NOT NULL").Scan(&entered); err != nil {
			t.Fatal(err)
		}
		if entered {
			break
		}
		if time.Now().After(until) {
			t.Fatal("publication never reached fixture barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Simulate the persisted outcome of a replacement executor after lease loss.
	if _, err = db.Exec("UPDATE rotation_recovery_run SET owner=owner+1,state='failed',stage='calculation_failed',message='replacement executor failed',lease_until=NULL WHERE id=?", accepted.Run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = gate.ExecContext(context.Background(), "SELECT RELEASE_LOCK('recovery_fixture_gate')"); err != nil {
		t.Fatal(err)
	}
	// Wait for the publication transaction to release its capture row lock.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var date string
	err = tx.QueryRow("SELECT trade_date FROM rotation_capture_run WHERE trade_date='20250103' FOR UPDATE").Scan(&date)
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	current := waitRecovery(t, api, accepted.Run.ID, "failed")
	if current.Message != "replacement executor failed" {
		t.Fatal("old executor replaced newer status", current)
	}
	var view DailyView
	captureHTTP(t, "GET", api+"/api/v1/rotation/daily?tradeDate=20250103", "", 200, &view)
	if view.Reference != nil {
		t.Fatal("lost executor published a reference", view.Reference)
	}
	if source.count() != 4 {
		t.Fatal("recovery requested new quotes")
	}
}
