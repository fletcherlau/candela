package rotation

import (
	"context"
	"encoding/json"
	"github.com/zeromicro/go-zero/rest/router"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syncer/internal/core"
	"syncer/internal/middleware"
	"syncer/internal/store"
	"testing"
	"time"
)

func captureAPI(t *testing.T, s *Service) string {
	t.Helper()
	routes := router.NewRouter()
	for _, route := range append(s.CaptureRoutes(middleware.NewApiKeyAuthMiddleware("fixture-only").Handle), s.DailyRoutes(middleware.NewApiKeyAuthMiddleware("fixture-only").Handle)...) {
		if err := routes.Handle(route.Method, route.Path, route.Handler); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(routes)
	t.Cleanup(server.Close)
	return server.URL + capturePath
}
func captureHTTP(t *testing.T, method, url, body string, status int, out any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", "fixture-only")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != status {
		t.Fatalf("%s %s status=%d want=%d", method, url, res.StatusCode, status)
	}
	if out != nil {
		if err = json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}
func TestCaptureHTTPAcceptsFixedTargetAndDeduplicates(t *testing.T) {
	db := dailyDatabase(t)
	_, err := db.Exec("INSERT INTO rotation_calendar(cal_date,is_open) VALUES ('20250103',1)")
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db, Now: func() time.Time { return time.Date(2025, 1, 3, 6, 45, 3, 0, time.UTC) }}
	api := captureAPI(t, svc)
	var first, again struct {
		Run          struct{ TradeDate, TargetAt, State string }
		Deduplicated bool
	}
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 202, &first)
	if first.Run.TradeDate != "20250103" || first.Run.TargetAt != "2025-01-03T14:45:00+08:00" || first.Run.State != "queued" || first.Deduplicated {
		t.Fatalf("frozen acceptance: %+v", first)
	}
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 200, &again)
	if !again.Deduplicated || again.Run != first.Run {
		t.Fatalf("duplicate target changed: %+v", again)
	}
	// Reconstructing the service must find the same saved target, without collection.
	fresh := &Service{DB: db, Now: svc.Now}
	captureHTTP(t, "GET", captureAPI(t, fresh)+"/20250103", "", 200, &again)
	if again.Run != first.Run {
		t.Fatalf("target not durable: %+v", again)
	}
}

type captureSource struct {
	before   func(context.Context, string) error
	mu       sync.Mutex
	calls    int
	quotes   map[string]core.RealtimeQuote
	failures map[string]bool
}

func (s *captureSource) FetchRealtime(ctx context.Context, codes []string) ([]core.RealtimeQuote, error) {
	s.mu.Lock()
	s.calls++
	var out []core.RealtimeQuote
	failed := false
	for _, code := range codes {
		failed = failed || s.failures[code]
		if q, ok := s.quotes[code]; ok {
			out = append(out, q)
		}
	}
	s.mu.Unlock()
	if s.before != nil {
		if err := s.before(ctx, codes[0]); err != nil {
			return nil, err
		}
	}
	if failed {
		return nil, context.DeadlineExceeded
	}
	return out, nil
}

func (s *captureSource) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.calls }
func sourceAt(date string) *captureSource {
	target, _ := captureTarget(date)
	s := &captureSource{quotes: map[string]core.RealtimeQuote{}, failures: map[string]bool{}}
	for i, code := range core.RotationCodes {
		p := 100 + float64(i)
		s.quotes[code] = core.RealtimeQuote{TsCode: code, TradeDate: date, Source: "fixture", SourceTime: target.Add(2 * time.Second), Open: p, High: p + 2, Low: p - 2, Latest: p + 1}
	}
	return s
}
func startCaptureWorker(t *testing.T, s *Service) func() {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.ServeCaptures(ctx) }()
	closeWorker := func() {
		stop()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("capture worker did not stop")
		}
	}
	t.Cleanup(closeWorker)
	return closeWorker
}
func waitCapture(t *testing.T, api, date, state string) CaptureRun {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for {
		var out struct{ Run CaptureRun }
		captureHTTP(t, "GET", api+"/"+date, "", 200, &out)
		if out.Run.State == state {
			return out.Run
		}
		if time.Now().After(until) {
			t.Fatalf("capture state: %+v want=%s", out.Run, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestCaptureHTTPFreezesFirstInputsAndReadsWithoutFetching(t *testing.T) {
	db := dailyDatabase(t)
	ctx := context.Background()
	date := "20250103"
	target, _ := captureTarget(date)
	_, err := db.Exec("INSERT INTO rotation_calendar VALUES ('20250102',1),('20250103',1)")
	if err != nil {
		t.Fatal(err)
	}
	st := store.NewMySQLStore(db)
	for _, code := range core.RotationCodes {
		if _, err = st.UpsertDaily(ctx, []core.Bar{{TsCode: code, TradeDate: "20250102", Open: 100, High: 102, Low: 99, Close: 101}}); err != nil {
			t.Fatal(err)
		}
		if _, err = st.UpsertAdjFactors(ctx, []core.AdjFactor{{TsCode: code, TradeDate: "20250102", AdjFactor: 2}, {TsCode: code, TradeDate: "20250104", AdjFactor: 9}}); err != nil {
			t.Fatal(err)
		}
	}
	source := sourceAt(date)
	svc := &Service{DB: db, Realtime: source, Now: func() time.Time { return target.Add(3 * time.Second) }, QuantileWindow: 5}
	api := captureAPI(t, svc)
	captureHTTP(t, "GET", api, "", 200, nil)
	if source.count() != 0 {
		t.Fatal("GET fetched source")
	}
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 202, nil)
	if source.count() != 0 {
		t.Fatal("acceptance fetched source synchronously")
	}
	stop := startCaptureWorker(t, svc)
	before := waitCapture(t, api, date, "captured")
	stop()
	if before.Available != 4 || before.Stage != "awaiting_calculation" || len(before.Items) != 4 || source.count() != 4 {
		t.Fatalf("raw capture %+v calls=%d", before, source.count())
	}
	for _, item := range before.Items {
		if item.Input == nil || item.Input.Quote.SourceTime.Format(time.RFC3339) != "2025-01-03T14:45:02+08:00" || item.Input.Factor == nil || *item.Input.Factor != 2 || len(item.Input.History) != 1 || *item.Input.History[0].Close != 101 || item.Input.Params.QuantileWindow != 5 {
			t.Fatalf("frozen evidence: %+v", item)
		}
	}
	encoded, _ := json.Marshal(before.Items)
	// Changing raw history, adjustment factors and the legacy per-day snapshot
	// table must not rewrite already frozen inputs, even before publication.
	_, err = db.Exec("UPDATE etf_daily SET close=999 WHERE trade_date='20250102'")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE etf_adj_factor SET adj_factor=7 WHERE trade_date='20250102'")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range core.RotationCodes {
		_, err = db.Exec(`INSERT INTO intraday_snapshot(ts_code,trade_date,open,high,low,latest,adj_mean) VALUES (?,'20250103',1,1,1,1,1) ON DUPLICATE KEY UPDATE latest=1`, code)
		if err != nil {
			t.Fatal(err)
		}
	}
	later := &Service{DB: db, Realtime: source, Now: func() time.Time { return target.Add(10 * time.Minute) }, QuantileWindow: 1200}
	api2 := captureAPI(t, later)
	captureHTTP(t, "POST", api2, `{"tradeDate":"20250103"}`, 200, nil)
	after := waitCapture(t, api2, date, "captured")
	saved, _ := json.Marshal(after.Items)
	if string(saved) != string(encoded) || source.count() != 4 {
		t.Fatal("saved inputs changed or reread triggered a source fetch")
	}
}

func TestCaptureHTTPPartialAndLateTargetsNeverUseAnotherTime(t *testing.T) {
	for _, tc := range []struct {
		name             string
		offset           time.Duration
		modify           func(*captureSource)
		want             string
		available, calls int
	}{
		{"late task", 5 * time.Minute, func(*captureSource) {}, "missing", 0, 0},
		{"source failure", 3 * time.Second, func(s *captureSource) { s.failures[core.RotationCodes[1]] = true }, "partial", 3, 4},
		{"wrong trading day", 3 * time.Second, func(s *captureSource) {
			q := s.quotes[core.RotationCodes[0]]
			q.TradeDate = "20250102"
			s.quotes[q.TsCode] = q
		}, "partial", 3, 4},
		{"old quote", 3 * time.Second, func(s *captureSource) {
			q := s.quotes[core.RotationCodes[0]]
			q.SourceTime = q.SourceTime.Add(-time.Minute)
			s.quotes[q.TsCode] = q
		}, "partial", 3, 4},
		{"source ahead of collection", 3 * time.Second, func(s *captureSource) {
			q := s.quotes[core.RotationCodes[0]]
			q.SourceTime = q.SourceTime.Add(time.Second * 10)
			s.quotes[q.TsCode] = q
		}, "partial", 3, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := dailyDatabase(t)
			_, err := db.Exec("INSERT INTO rotation_calendar VALUES ('20250103',1)")
			if err != nil {
				t.Fatal(err)
			}
			target, _ := captureTarget("20250103")
			source := sourceAt("20250103")
			tc.modify(source)
			svc := &Service{DB: db, Realtime: source, Now: func() time.Time { return target.Add(tc.offset) }}
			api := captureAPI(t, svc)
			captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 202, nil)
			stop := startCaptureWorker(t, svc)
			result := waitCapture(t, api, "20250103", tc.want)
			stop()
			if result.Available != tc.available || source.count() != tc.calls {
				t.Fatalf("wrong partial accounting %+v calls=%d", result, source.count())
			}
			for _, item := range result.Items {
				if item.State == "missing" && (item.Reason == "" || item.Input != nil) {
					t.Fatalf("unproven input published: %+v", item)
				}
			}
		})
	}
}

func TestCaptureHTTPValidatesCachedCalendarAndRequestBoundaries(t *testing.T) {
	db := dailyDatabase(t)
	target, _ := captureTarget("20250103")
	source := sourceAt("20250103")
	svc := &Service{DB: db, Realtime: source, Now: func() time.Time { return target.Add(time.Second) }}
	api := captureAPI(t, svc)
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 409, nil)
	if _, err := db.Exec("INSERT INTO rotation_calendar VALUES ('20250103',0)"); err != nil {
		t.Fatal(err)
	}
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 409, nil)
	if _, err := db.Exec("UPDATE rotation_calendar SET is_open=1"); err != nil {
		t.Fatal(err)
	}
	early := &Service{DB: db, Realtime: source, Now: func() time.Time { return target.Add(-time.Second) }}
	captureHTTP(t, "POST", captureAPI(t, early), `{"tradeDate":"20250103"}`, 409, nil)
	for _, body := range []string{`{"tradeDate":"20250230"}`, `{"tradeDate":"20250104"}`, `{"tradeDate":"20250103","time":"14:50"}`, `{"tradeDate":"20250103"}{}`} {
		captureHTTP(t, "POST", api, body, 400, nil)
	}
	captureHTTP(t, "POST", api+"?force=true", `{"tradeDate":"20250103"}`, 400, nil)
	res, err := http.Post(api, "application/json", strings.NewReader(`{"tradeDate":"20250103"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("unprotected submission %d", res.StatusCode)
	}
	if source.count() != 0 {
		t.Fatal("invalid submission fetched source")
	}
}

func TestCaptureHTTPRecoveryRetainsPartialInputsAndRejectsOldOwner(t *testing.T) {
	db := dailyDatabase(t)
	_, err := db.Exec("INSERT INTO rotation_calendar VALUES ('20250103',1)")
	if err != nil {
		t.Fatal(err)
	}
	target, _ := captureTarget("20250103")
	source := sourceAt("20250103")
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	source.before = func(_ context.Context, code string) error {
		if code == core.RotationCodes[1] {
			once.Do(func() { close(entered) })
			<-release
		}
		return nil
	}
	svc := &Service{DB: db, Realtime: source, Now: func() time.Time { return target.Add(3 * time.Second) }, QuantileWindow: 5}
	api := captureAPI(t, svc)
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 202, nil)
	stopOld := startCaptureWorker(t, svc)
	// Cleanup releases the deliberately uncooperative source before waiting for its worker.
	var releaseOnce sync.Once
	releaseSource := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseSource)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("second source call missing")
	}
	var before struct{ Run CaptureRun }
	captureHTTP(t, "GET", api+"/20250103", "", 200, &before)
	if before.Run.Available != 1 {
		t.Fatalf("first checkpoint not saved: %+v", before)
	}
	original, _ := json.Marshal(before.Run.Items[0].Input)
	_, err = db.Exec("UPDATE rotation_capture_run SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE trade_date='20250103'")
	if err != nil {
		t.Fatal(err)
	}
	laterSource := sourceAt("20250103")
	fresh := &Service{DB: db, Realtime: laterSource, Now: func() time.Time { return target.Add(24 * time.Hour) }, QuantileWindow: 1200}
	api2 := captureAPI(t, fresh)
	stopFresh := startCaptureWorker(t, fresh)
	after := waitCapture(t, api2, "20250103", "partial")
	stopFresh()
	releaseSource()
	stopOld()
	stable := waitCapture(t, api2, "20250103", "partial")
	saved, _ := json.Marshal(stable.Items[0].Input)
	if after.Available != 1 || after.Recoveries != 1 || after.Params.QuantileWindow != 5 || laterSource.count() != 0 || string(saved) != string(original) {
		t.Fatalf("recovery changed original target/input: %+v", after)
	}
	for _, item := range stable.Items[1:] {
		if item.Input != nil || !strings.Contains(item.Reason, "无法补取") {
			t.Fatalf("late recovery fabricated data: %+v", item)
		}
	}
}

func TestCaptureHTTPConcurrentAcceptanceAndWorkersCaptureOnce(t *testing.T) {
	db := dailyDatabase(t)
	_, err := db.Exec("INSERT INTO rotation_calendar VALUES ('20250103',1)")
	if err != nil {
		t.Fatal(err)
	}
	target, _ := captureTarget("20250103")
	source := sourceAt("20250103")
	svc := &Service{DB: db, Realtime: source, Now: func() time.Time { return target.Add(3 * time.Second) }}
	api := captureAPI(t, svc)
	codes := make(chan int, 10)
	var group sync.WaitGroup
	for i := 0; i < 10; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			req, _ := http.NewRequest("POST", api, strings.NewReader(`{"tradeDate":"20250103"}`))
			req.Header.Set("X-Api-Key", "fixture-only")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				codes <- 0
				return
			}
			defer res.Body.Close()
			codes <- res.StatusCode
		}()
	}
	group.Wait()
	close(codes)
	accepted := 0
	for status := range codes {
		if status == 202 {
			accepted++
		} else if status != 200 {
			t.Fatalf("duplicate acceptance returned %d", status)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d targets", accepted)
	}
	stopOne := startCaptureWorker(t, svc)
	stopTwo := startCaptureWorker(t, &Service{DB: db, Realtime: source, Now: svc.Now})
	result := waitCapture(t, api, "20250103", "captured")
	stopOne()
	stopTwo()
	if result.Available != 4 || source.count() != 4 {
		t.Fatalf("multiple owners fetched target: %+v calls=%d", result, source.count())
	}
}

func TestCaptureHTTPRejectsQuoteReceivedAfterOriginalWindow(t *testing.T) {
	db := dailyDatabase(t)
	if _, err := db.Exec("INSERT INTO rotation_calendar(cal_date,is_open) VALUES ('20250103',1)"); err != nil {
		t.Fatal(err)
	}
	target, _ := captureTarget("20250103")
	var mu sync.Mutex
	now := target.Add(3 * time.Second)
	source := sourceAt("20250103")
	source.before = func(context.Context, string) error { mu.Lock(); now = target.Add(time.Minute); mu.Unlock(); return nil }
	svc := &Service{DB: db, Realtime: source, Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return now }}
	api := captureAPI(t, svc)
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, svc)
	run := waitCapture(t, api, "20250103", "missing")
	if source.count() != 1 || run.Available != 0 || run.Items[0].Input != nil || !strings.Contains(run.Items[0].Reason, "未在原 14:45 窗口内完成") {
		t.Fatalf("late response was accepted: %+v", run)
	}
}
