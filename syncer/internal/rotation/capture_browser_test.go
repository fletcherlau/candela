package rotation

import (
	"encoding/json"
	"github.com/zeromicro/go-zero/rest/router"
	"net/http"
	"os"
	"syncer/internal/core"
	"syncer/internal/middleware"
	"testing"
	"time"
)

// Production routes and workers, isolated MySQL; only time and quote inputs are
// controlled. The binary does not include this opt-in acceptance harness.
func TestCaptureBrowserHarness(t *testing.T) {
	if os.Getenv("CANDELA_CAPTURE_BROWSER_TEST") != "1" {
		t.Skip("browser harness is opt-in")
	}
	db := dailyDatabase(t)
	var sources []*captureSource
	for _, date := range []string{"20250103", "20250106", "20250107"} {
		if _, err := db.Exec("INSERT INTO rotation_calendar(cal_date,is_open) VALUES (?,1)", date); err != nil {
			t.Fatal(err)
		}
		target, _ := captureTarget(date)
		now := target.Add(3 * time.Second)
		source := sourceAt(date)
		sources = append(sources, source)
		state := "captured"
		if date == "20250103" {
			now = target.Add(5 * time.Minute)
			state = "missing"
		}
		if date == "20250106" {
			source.failures[core.RotationCodes[3]] = true
			state = "partial"
		}
		svc := &Service{DB: db, Realtime: source, Now: func() time.Time { return now }}
		api := captureAPI(t, svc)
		captureHTTP(t, "POST", api, `{"tradeDate":"`+date+`"}`, 202, nil)
		stop := startCaptureWorker(t, svc)
		waitCapture(t, api, date, state)
		stop()
	}
	svc := &Service{DB: db, Now: func() time.Time { return time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC) }}
	routes := router.NewRouter()
	for _, route := range svc.CaptureRoutes(middleware.NewApiKeyAuthMiddleware("fixture-only").Handle) {
		if err := routes.Handle(route.Method, route.Path, route.Handler); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal(http.ListenAndServe("127.0.0.1:18086", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test-source-count" {
			count := 0
			for _, source := range sources {
				count += source.count()
			}
			_ = json.NewEncoder(w).Encode(map[string]int{"calls": count})
			return
		}
		routes.ServeHTTP(w, r)
	})))
}
