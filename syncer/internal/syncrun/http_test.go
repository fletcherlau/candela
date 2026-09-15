package syncrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"syncer/internal/middleware"
	"testing"
	"time"
)

func TestMySQLHTTPAcceptanceAndDisconnectedBrowser(t *testing.T) {
	db := testDB(t)
	st := NewStore(db)
	src := sourceFor(t, &sourceFixture{bars: fixtureBars("20041231", Cutoff(time.Now()))})
	api := httptest.NewServer(middleware.NewApiKeyAuthMiddleware("fixture-only").Handle(st.Handler()))
	defer api.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", api.URL+"/api/v1/data/sync-runs", strings.NewReader(`{"mode":"backfill"}`))
	req.Header.Set("X-Api-Key", "fixture-only")
	res, err := http.DefaultClient.Do(req)
	check(t, err)
	var accepted struct {
		Run Run `json:"run"`
	}
	check(t, json.NewDecoder(res.Body).Decode(&accepted))
	res.Body.Close()
	cancel()
	if res.StatusCode != 202 || accepted.Run.State != "queued" || count(t, db, "index_daily") != 0 {
		t.Fatalf("not durably queued %+v", accepted)
	}
	service, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan struct{})
	go func() { defer close(done); (&Worker{st, src}).Serve(service) }()
	defer func() { stop(); <-done }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r, err := st.Get(context.Background(), accepted.Run.ID)
		check(t, err)
		if r.State == "succeeded" {
			break
		}
		if r.State == "failed" || time.Now().After(deadline) {
			t.Fatalf("detached job %+v", r)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Reopen the record through a fresh HTTP request after the original connection is gone.
	request, _ := http.NewRequest("GET", api.URL+"/api/v1/data/sync-runs/"+accepted.Run.ID, nil)
	request.Header.Set("X-Api-Key", "fixture-only")
	response, err := http.DefaultClient.Do(request)
	check(t, err)
	defer response.Body.Close()
	var detail struct {
		Run Run `json:"run"`
	}
	check(t, json.NewDecoder(response.Body).Decode(&detail))
	if detail.Run.State != "succeeded" || detail.Run.ID != accepted.Run.ID {
		t.Fatalf("lost job %+v", detail)
	}
	unauth, err := http.Post(api.URL+"/api/v1/data/sync-runs", "application/json", strings.NewReader(`{"mode":"backfill"}`))
	check(t, err)
	unauth.Body.Close()
	if unauth.StatusCode != 401 {
		t.Fatal("internal API not protected")
	}
	for _, body := range []string{`{"mode":"cancel"}`, `{"mode":"backfill","code":"510300.SH"}`, `{"mode":"backfill"}{}`} {
		r := httptest.NewRequest("POST", "/api/v1/data/sync-runs", strings.NewReader(body))
		w := httptest.NewRecorder()
		st.Handler()(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid request accepted %s", body)
		}
	}
}
