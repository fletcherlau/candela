package rotation

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"syncer/internal/core"
	"syncer/internal/middleware"
	"testing"

	"github.com/zeromicro/go-zero/rest/router"
)

// Only source fixtures and clocks differ from production. Browser actions use
// the signed web session, real sync/recovery HTTP, durable workers and MySQL.
func TestCorrectionBrowserHarness(t *testing.T) {
	if os.Getenv("CANDELA_CORRECTION_BROWSER_TEST") != "1" {
		t.Skip("browser harness is opt-in")
	}
	f := seedContinuousCorrection(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go f.service.Serve(ctx)
	go f.service.ServeRecoveries(ctx)
	routes := router.NewRouter()
	auth := middleware.NewApiKeyAuthMiddleware("fixture-only").Handle
	all := append(append(append(f.service.RecoveryRoutes(auth), f.service.CaptureRoutes(auth)...), f.service.DailyRoutes(auth)...), f.jobs.Routes(auth)...)
	for _, route := range all {
		if err := routes.Handle(route.Method, route.Path, route.Handler); err != nil {
			t.Fatal(err)
		}
	}
	routes.Handle("GET", "/api/v1/rotation/backtest", auth(f.service.Handler().ServeHTTP))
	routes.Handle("GET", "/api/v1/rotation/backtest/range", auth(f.service.RangeHandler().ServeHTTP))
	var control sync.Mutex
	var held chan struct{}
	t.Fatal(http.ListenAndServe("127.0.0.1:18091", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test-source" {
			if r.Method == "POST" {
				control.Lock()
				defer control.Unlock()
				f.source.mu.Lock()
				mode := r.URL.Query().Get("mode")
				if mode == "hold" && held == nil {
					held = make(chan struct{})
					f.source.holdFactors = held
				}
				if mode == "release" && held != nil {
					close(held)
					held = nil
					f.source.holdFactors = nil
				}
				f.source.failFactors = mode == "fail"
				if mode == "correct" {
					for i := range f.source.bars[core.RotationCodes[0]] {
						b := &f.source.bars[core.RotationCodes[0]][i]
						if b.TradeDate == "20250107" {
							b.Open, b.High, b.Low, b.Close = 110, 110, 110, 110
						}
					}
					for i := range f.source.factors[core.RotationCodes[3]] {
						a := &f.source.factors[core.RotationCodes[3]][i]
						if a.TradeDate == "20250107" {
							a.AdjFactor = 2
						}
					}
				}
				if mode == "fail" {
					for i := range f.source.bars[core.RotationCodes[0]] {
						b := &f.source.bars[core.RotationCodes[0]][i]
						if b.TradeDate == "20250103" {
							b.Open, b.High, b.Low, b.Close = 120, 120, 120, 120
						}
					}
				}
				f.source.mu.Unlock()
			}
			json.NewEncoder(w).Encode(map[string]any{"referenceCalls": f.realtime.count(), "dataSource": "isolated source fixture; not market data"})
			return
		}
		routes.ServeHTTP(w, r)
	})))
}
