package syncrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"syncer/internal/middleware"
	"syncer/internal/schema"
	"testing"
	"time"
)

// The public maintenance HTTP API is the observation seam; only the market
// source and persisted crash/lease conditions are controlled by these tests.
func maintenanceAPI(t *testing.T, st *Store) string {
	t.Helper()
	api := httptest.NewServer(middleware.NewApiKeyAuthMiddleware("fixture-only").Handle(st.Handler()))
	t.Cleanup(api.Close)
	return api.URL + "/api/v1/data/sync-runs"
}
func maintenanceRequest(t *testing.T, method, url, body string, status int) Run {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	check(t, err)
	req.Header.Set("X-Api-Key", "fixture-only")
	res, err := http.DefaultClient.Do(req)
	check(t, err)
	defer res.Body.Close()
	var out struct {
		Run   Run    `json:"run"`
		Error string `json:"error"`
	}
	check(t, json.NewDecoder(res.Body).Decode(&out))
	if res.StatusCode != status {
		t.Fatalf("%s %s: status=%d want=%d error=%s", method, url, res.StatusCode, status, out.Error)
	}
	return out.Run
}
func TestMySQLHTTPCancelQueuedIsDurableAndIdempotent(t *testing.T) {
	db := testDB(t)
	st := NewStore(db)
	api := maintenanceAPI(t, st)
	accepted := maintenanceRequest(t, "POST", api, `{"mode":"backfill"}`, 202)
	cancelled := maintenanceRequest(t, "POST", api+"/"+accepted.ID+"/cancel", "", 200)
	if cancelled.State != "cancelled" || cancelled.ProcessedRows != 0 {
		t.Fatalf("cancel: %+v", cancelled)
	}
	again := maintenanceRequest(t, "POST", api+"/"+accepted.ID+"/cancel", "", 200)
	if again.State != "cancelled" || again.ID != cancelled.ID {
		t.Fatalf("repeat cancel: %+v", again)
	}
	// Replaying version 5 after interrupted migration registration preserves
	// accepted cancellation and its public event history.
	_, err := db.Exec("DELETE FROM schema_migration WHERE version=5")
	check(t, err)
	check(t, schema.Ensure(context.Background(), db))
	assertMaintenanceEvents(t, api+"/"+accepted.ID, "cancelled")
	// Reconstruct the service, then submit a new run with the same original intent.
	// Cancelled work must never be deduplicated into or resumed by the new worker.
	restarted := NewStore(db)
	api2 := maintenanceAPI(t, restarted)
	next := maintenanceRequest(t, "POST", api2, `{"mode":"backfill"}`, 202)
	if next.ID == accepted.ID {
		t.Fatal("cancelled request reused")
	}
	(&Worker{restarted, sourceFor(t, &sourceFixture{bars: fixtureBars("20260801", Cutoff(time.Now()))})}).Execute(context.Background(), mustClaim(t, restarted))
	saved := maintenanceRequest(t, "GET", api2+"/"+accepted.ID, "", 200)
	done := maintenanceRequest(t, "GET", api2+"/"+next.ID, "", 200)
	if saved.State != "cancelled" || saved.ProcessedRows != 0 || done.State != "succeeded" {
		t.Fatalf("restart: cancelled=%+v next=%+v", saved, done)
	}
	terminal := maintenanceRequest(t, "POST", api2+"/"+next.ID+"/cancel", "", 200)
	assertMaintenanceEvents(t, api2+"/"+accepted.ID, "cancelled")
	if terminal.State != "succeeded" {
		t.Fatalf("terminal changed: %+v", terminal)
	}
}
func mustClaim(t *testing.T, st *Store) Run {
	t.Helper()
	r, err := st.Claim(context.Background())
	check(t, err)
	return r
}

type sourceCall struct {
	from, to string
	reply    chan []Bar
}
type heldSource struct{ calls chan sourceCall }

func (s *heldSource) Earliest(context.Context, string) (string, string, error) {
	return "20240909", "fixture fixed inception", nil
}
func (s *heldSource) Window(_ context.Context, from, to string) ([]Bar, error) {
	call := sourceCall{from, to, make(chan []Bar, 1)}
	s.calls <- call
	// Deliberately ignore cancellation: an old external request may return late.
	return <-call.reply, nil
}
func nextSourceCall(t *testing.T, s *heldSource) sourceCall {
	t.Helper()
	select {
	case c := <-s.calls:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("source was not called")
		return sourceCall{}
	}
}
func awaitWorker(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}
}
func TestMySQLHTTPCancelRunningRejectsLateSourceAndRetainsCommittedWindow(t *testing.T) {
	db := testDB(t)
	st := NewStore(db)
	api := maintenanceAPI(t, st)
	accepted := maintenanceRequest(t, "POST", api, `{"mode":"backfill"}`, 202)
	src := &heldSource{calls: make(chan sourceCall, 1)}
	claimed := mustClaim(t, st)
	done := make(chan struct{})
	go func() { defer close(done); (&Worker{st, src}).Execute(context.Background(), claimed) }()
	first := nextSourceCall(t, src)
	first.reply <- fixtureBars(first.from, first.to)
	second := nextSourceCall(t, src)
	before := maintenanceRequest(t, "GET", api+"/"+accepted.ID, "", 200)
	if before.CompletedSegments != 1 || before.Checkpoint != first.to {
		t.Fatalf("first commit missing: %+v", before)
	}
	cancelling := maintenanceRequest(t, "POST", api+"/"+accepted.ID+"/cancel", "", 200)
	if cancelling.State != "cancelling" {
		t.Fatalf("intent not persisted: %+v", cancelling)
	}
	second.reply <- fixtureBars(second.from, second.to)
	awaitWorker(t, done)
	after := maintenanceRequest(t, "GET", api+"/"+accepted.ID, "", 200)
	assertMaintenanceEvents(t, api+"/"+accepted.ID, "cancel_requested", "cancelled")
	if after.State != "cancelled" || after.CompletedSegments != 1 || after.Checkpoint != first.to || after.ProcessedRows != before.ProcessedRows {
		t.Fatalf("late writer after cancel: %+v", after)
	}
}

type resumedSource struct{ next, end string }

func (s *resumedSource) Earliest(context.Context, string) (string, string, error) {
	return "", "", &SourceError{"rediscovered_history", "恢复不得重新发现已冻结的起点"}
}
func (s *resumedSource) Window(_ context.Context, from, to string) ([]Bar, error) {
	if from != s.next || to > s.end {
		return nil, &SourceError{"wrong_range", "恢复改变了原范围或重跑成功分段"}
	}
	s.next = date(to).AddDate(0, 0, 1).Format("20060102")
	return fixtureBars(from, to), nil
}
func TestMySQLHTTPExpiredProcessResumesOriginalCheckpointAndFencesOldWorker(t *testing.T) {
	db := testDB(t)
	st := NewStore(db)
	api := maintenanceAPI(t, st)
	// The frozen request predates recovery by days: wall-clock today must not
	// expand its end date. Setup submits an earlier request through the same store.
	submitted, _, err := st.Submit(context.Background(), "backfill", at("20260914"))
	check(t, err)
	old := mustClaim(t, st)
	src := &heldSource{calls: make(chan sourceCall, 1)}
	done := make(chan struct{})
	go func() { defer close(done); (&Worker{st, src}).Execute(context.Background(), old) }()
	first := nextSourceCall(t, src)
	first.reply <- fixtureBars(first.from, first.to)
	late := nextSourceCall(t, src)
	before := maintenanceRequest(t, "GET", api+"/"+submitted.ID, "", 200)
	// Simulate an old process that can no longer renew its durable lease. This is
	// crash setup only; all outcome assertions use the maintenance HTTP endpoint.
	_, err = db.Exec("UPDATE sync_run SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", submitted.ID)
	check(t, err)
	restarted := NewStore(db)
	next, err := restarted.Claim(context.Background())
	if err != nil {
		late.reply <- fixtureBars(late.from, late.to)
		awaitWorker(t, done)
		t.Fatalf("unfinished request not recovered: %v", err)
	}
	if next.ID != submitted.ID || next.Owner <= old.Owner {
		late.reply <- nil
		awaitWorker(t, done)
		t.Fatalf("wrong recovered request: %+v", next)
	}
	resume := &resumedSource{next: date(first.to).AddDate(0, 0, 1).Format("20060102"), end: submitted.EndDate}
	(&Worker{restarted, resume}).Execute(context.Background(), next)
	late.reply <- fixtureBars(late.from, late.to)
	awaitWorker(t, done)
	after := maintenanceRequest(t, "GET", maintenanceAPI(t, restarted)+"/"+submitted.ID, "", 200)
	assertMaintenanceEvents(t, api+"/"+submitted.ID, "interrupted", "resumed")
	if after.State != "succeeded" || after.EndDate != "20260914" || after.EffectiveStart != before.EffectiveStart || after.Checkpoint != "20260914" || after.CompletedSegments != 3 || after.ProcessedRows != 526 {
		t.Fatalf("recovered publication: %+v", after)
	}
}

func assertMaintenanceEvents(t *testing.T, url string, kinds ...string) {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	check(t, err)
	req.Header.Set("X-Api-Key", "fixture-only")
	res, err := http.DefaultClient.Do(req)
	check(t, err)
	defer res.Body.Close()
	var result struct {
		Run struct {
			Events []struct{ Kind, At, Message, Checkpoint string }
		}
	}
	check(t, json.NewDecoder(res.Body).Decode(&result))
	if res.StatusCode != 200 || len(result.Run.Events) != len(kinds) {
		t.Fatalf("event history: status=%d events=%+v want=%v", res.StatusCode, result.Run.Events, kinds)
	}
	for i, event := range result.Run.Events {
		if event.Kind != kinds[i] || event.At == "" || event.Message == "" {
			t.Fatalf("event %d: %+v", i, event)
		}
	}
}

func TestMySQLHTTPShutdownLeavesResumableCheckpoint(t *testing.T) {
	db := testDB(t)
	st := NewStore(db)
	api := maintenanceAPI(t, st)
	accepted := maintenanceRequest(t, "POST", api, `{"mode":"backfill"}`, 202)
	old := mustClaim(t, st)
	src := &heldSource{calls: make(chan sourceCall, 1)}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan struct{})
	go func() { defer close(done); (&Worker{st, src}).Execute(ctx, old) }()
	first := nextSourceCall(t, src)
	first.reply <- fixtureBars(first.from, first.to)
	pending := nextSourceCall(t, src)
	stop()
	pending.reply <- fixtureBars(pending.from, pending.to)
	awaitWorker(t, done)
	interrupted := maintenanceRequest(t, "GET", api+"/"+accepted.ID, "", 200)
	if interrupted.State != "queued" || interrupted.Stage != "recovering" || interrupted.Checkpoint != first.to {
		t.Fatalf("shutdown lost resumable work: %+v", interrupted)
	}
	restarted := NewStore(db)
	next := mustClaim(t, restarted)
	(&Worker{restarted, &resumedSource{next: date(first.to).AddDate(0, 0, 1).Format("20060102"), end: accepted.EndDate}}).Execute(context.Background(), next)
	complete := maintenanceRequest(t, "GET", api+"/"+accepted.ID, "", 200)
	if complete.State != "succeeded" || complete.EndDate != accepted.EndDate {
		t.Fatalf("shutdown recovery: %+v", complete)
	}
	assertMaintenanceEvents(t, api+"/"+accepted.ID, "interrupted", "resumed")
}
