package rotation

import (
	"context"
	"encoding/json"
	"github.com/zeromicro/go-zero/rest/router"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"syncer/internal/core"
	"syncer/internal/middleware"
	"syncer/internal/store"
	"testing"
	"time"
)

// This test-only control surface sets external inputs/time. All capture,
// publication, persistence, auth and read routes execute the production code.
func TestReferenceBrowserHarness(t *testing.T) {
	if os.Getenv("CANDELA_REFERENCE_BROWSER_TEST") != "1" {
		t.Skip("browser harness is opt-in")
	}
	db, svc, source := seedReferenceScenario(t, "20250103")
	for code, q := range source.quotes {
		q.Source = "验收测试行情"
		source.quotes[code] = q
	}
	target, _ := captureTarget("20250103")
	var clock atomic.Int64
	clock.Store(target.Add(-time.Minute).UnixNano())
	svc.Now = func() time.Time { return time.Unix(0, clock.Load()) }
	st := store.NewMySQLStore(db)
	var stop func()
	var controls sync.Mutex
	reset := func() error {
		if stop != nil {
			stop()
		}
		for _, q := range []string{"DELETE FROM rotation_capture_run", "DELETE FROM rotation_daily WHERE trade_date='20250103'", "DELETE FROM etf_daily WHERE trade_date='20250103'", "UPDATE rotation_coverage SET through_date='20250102'"} {
			if _, err := db.Exec(q); err != nil {
				return err
			}
		}
		clock.Store(target.Add(-time.Minute).UnixNano())
		if err := svc.PublishClose(context.Background(), "20250102"); err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); svc.ServeCaptures(ctx) }()
		go func() { defer wg.Done(); svc.ServeReferences(ctx) }()
		stop = func() { cancel(); wg.Wait() }
		return nil
	}
	if err := reset(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	routes := router.NewRouter()
	auth := middleware.NewApiKeyAuthMiddleware("fixture-only").Handle
	for _, route := range append(append(svc.CaptureRoutes(auth), svc.DailyRoutes(auth)...), svc.RecoveryRoutes(auth)...) {
		if err := routes.Handle(route.Method, route.Path, route.Handler); err != nil {
			t.Fatal(err)
		}
	}
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test-source-count" {
			_ = json.NewEncoder(w).Encode(map[string]int{"calls": source.count()})
			return
		}
		if r.URL.Path == "/test-phase" && r.Method == http.MethodPost {
			controls.Lock()
			defer controls.Unlock()
			phase := r.URL.Query().Get("phase")
			var err error
			switch phase {
			case "before":
				err = reset()
			case "empty":
				if stop != nil {
					stop()
					stop = nil
				}
				if _, err = db.Exec("DELETE FROM rotation_capture_run"); err == nil {
					_, err = db.Exec("DELETE FROM rotation_daily")
				}
			case "reference":
				clock.Store(target.Add(3 * time.Second).UnixNano())
			case "missed":
				clock.Store(target.Add(5 * time.Minute).UnixNano())
			case "partial", "close":
				clock.Store(target.Add(2 * time.Hour).UnixNano())
				for i, code := range core.RotationCodes {
					if phase == "partial" && i == 3 {
						continue
					}
					p := float64(100+i) * (1 + []float64{.003, -.00125, 0, -.0055}[i])
					_, err = st.UpsertDaily(r.Context(), []core.Bar{{TsCode: code, TradeDate: "20250103", Open: p, High: p, Low: p, Close: p}})
					if err != nil {
						break
					}
					if _, err = db.Exec("UPDATE rotation_coverage SET through_date='20250103' WHERE ts_code=?", code); err != nil {
						break
					}
				}
				if err == nil {
					err = svc.RefreshDaily(r.Context())
				}
			default:
				http.Error(w, "unknown fixture phase", 400)
				return
			}
			if err != nil {
				http.Error(w, "fixture input failed", 500)
				t.Error(err)
				return
			}
			w.WriteHeader(204)
			return
		}
		routes.ServeHTTP(w, r)
	})
	t.Fatal(http.ListenAndServe("127.0.0.1:18087", app))
}
