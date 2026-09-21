package syncrun

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"syncer/internal/middleware"
	"syncer/internal/store"
	"testing"
	"time"
)

// Only compiled into test binaries. Supplies the full HTTP + MySQL workflow to
// Playwright, with a controlled Tushare protocol server and no production access.
func TestSyncBrowserHarness(t *testing.T) {
	if os.Getenv("CANDELA_SYNC_BROWSER_TEST") != "1" {
		t.Skip("browser harness is opt-in")
	}
	db := testDB(t)
	st := NewStore(db)
	f := &sourceFixture{bars: fixtureBars("20041231", Cutoff(time.Now())), hook: func(from, to string) { time.Sleep(40 * time.Millisecond) }}
	src := sourceFor(t, f)
	var serviceMu sync.Mutex
	var cancel context.CancelFunc
	var done chan struct{}
	start := func() {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		done = make(chan struct{})
		finished := done
		go func() { defer close(finished); (&Worker{NewStore(db), src}).Serve(ctx) }()
	}
	start()
	defer func() { cancel(); <-done }()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/data/sync-runs", st.Handler())
	mux.HandleFunc("/api/v1/data/sync-runs/", st.Handler())
	mux.HandleFunc("/api/v1/data/catalog", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(store.NewMySQLStore(db).Catalog(r.Context()))
	})
	// Test control endpoint deliberately exists only in this test binary.
	mux.HandleFunc("/fixture/restart", func(w http.ResponseWriter, r *http.Request) {
		serviceMu.Lock()
		defer serviceMu.Unlock()
		cancel()
		<-done
		start()
		w.WriteHeader(204)
	})

	mux.HandleFunc("/fixture/slow", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hook = func(string, string) { time.Sleep(250 * time.Millisecond) }
		f.mu.Unlock()
		w.WriteHeader(204)
	})

	mux.HandleFunc("/fixture/permission", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.code = 2002
		f.message = "权限不足"
		f.mu.Unlock()
		w.WriteHeader(204)
	})
	app := middleware.NewApiKeyAuthMiddleware("fixture-only").Handle(mux.ServeHTTP)
	t.Fatal(http.ListenAndServe("127.0.0.1:18082", app))
}
