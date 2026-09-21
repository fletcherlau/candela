package syncrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"syncer/internal/middleware"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/rest/router"
)

// Test-only, loopback-only source controls; the production app still verifies
// its signed Access credential and calls the real batch API with its own key.
func TestETFBrowserHarness(t *testing.T) {
	if os.Getenv("CANDELA_ETF_BROWSER_TEST") != "1" {
		t.Skip("browser harness is opt-in")
	}
	db := testDB(t)
	source := &etfFixture{failed: map[string]bool{}, calls: map[string]int{}}
	for i := 0; i < 20; i++ {
		code := fmt.Sprintf("500%03d.SH", i)
		_, err := db.Exec("INSERT INTO instrument(ts_code,name) VALUES (?,?)", code, "隔离测试 ETF "+code)
		check(t, err)
		if i >= 18 {
			source.failed[code] = true
		}
	}
	var hold atomic.Bool
	source.before = func(ctx context.Context, code, stage string) error {
		for hold.Load() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
		return nil
	}
	svc := &ETFService{Store: NewETFStore(db), Source: source, DefaultStart: "20250101", ChunkDays: 31, Now: func() time.Time { return at("20250103") }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Serve(ctx)
	routes := router.NewRouter()
	for _, route := range svc.Routes(middleware.NewApiKeyAuthMiddleware("fixture-only").Handle) {
		check(t, routes.Handle(route.Method, route.Path, route.Handler))
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", routes)
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
		source.mu.Lock()
		defer source.mu.Unlock()
		if r.Method == "POST" {
			switch r.URL.Query().Get("mode") {
			case "repair":
				source.failed = map[string]bool{}
			case "hold":
				hold.Store(true)
			case "release":
				hold.Store(false)
			default:
				http.Error(w, "unknown fixture", 400)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"calls": source.calls})
	})
	t.Fatal(http.ListenAndServe("127.0.0.1:18089", mux))
}
