package syncrun

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zeromicro/go-zero/rest/router"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syncer/internal/core"
	"syncer/internal/middleware"
	"testing"
	"time"
)

type etfFixture struct {
	mu     sync.Mutex
	failed map[string]bool
	calls  map[string]int
	before func(context.Context, string, string) error
}

func (f *etfFixture) FetchDaily(ctx context.Context, code, from, to string) ([]core.Bar, error) {
	if f.before != nil {
		if err := f.before(ctx, code, "daily"); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[code+"/daily"]++
	return []core.Bar{{TsCode: code, TradeDate: to, Open: 100, High: 100, Low: 100, Close: 100}}, nil
}
func (f *etfFixture) FetchAdj(ctx context.Context, code, from, to string) ([]core.AdjFactor, error) {
	if f.before != nil {
		if err := f.before(ctx, code, "adj"); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[code+"/adj"]++
	if f.failed[code] {
		return nil, fmt.Errorf("source unavailable")
	}
	return []core.AdjFactor{{TsCode: code, TradeDate: to, AdjFactor: 1}}, nil
}
func etfAPI(t *testing.T, s *ETFService) string {
	t.Helper()
	r := router.NewRouter()
	for _, route := range s.Routes(middleware.NewApiKeyAuthMiddleware("fixture-only").Handle) {
		check(t, r.Handle(route.Method, route.Path, route.Handler))
	}
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server.URL + "/api/v1/data/etf-syncs"
}
func etfHTTP(t *testing.T, method, url, body string, status int, out any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	check(t, err)
	req.Header.Set("X-Api-Key", "fixture-only")
	res, err := http.DefaultClient.Do(req)
	check(t, err)
	defer res.Body.Close()
	if res.StatusCode != status {
		t.Fatalf("%s %s status %d want %d", method, url, res.StatusCode, status)
	}
	if out != nil {
		check(t, json.NewDecoder(res.Body).Decode(out))
	}
}
func TestETFHTTPBatchRetriesOnlyFailedFactorsWithFrozenTargets(t *testing.T) {
	db := testDB(t)
	source := &etfFixture{failed: map[string]bool{}, calls: map[string]int{}}
	for i := 0; i < 20; i++ {
		code := fmt.Sprintf("500%03d.SH", i)
		_, err := db.Exec("INSERT INTO instrument(ts_code,name) VALUES (?,?)", code, code)
		check(t, err)
		if i >= 18 {
			source.failed[code] = true
		}
	}
	svc := &ETFService{Store: NewETFStore(db), Source: source, DefaultStart: "20250101", ChunkDays: 31, Now: func() time.Time { return at("20250103") }}
	api := etfAPI(t, svc)
	var accepted, duplicate struct {
		Batch        ETFBatch `json:"batch"`
		Deduplicated bool     `json:"deduplicated"`
	}
	etfHTTP(t, "POST", api, `{}`, 202, &accepted)
	etfHTTP(t, "POST", api, `{}`, 200, &duplicate)
	if duplicate.Batch.ID != accepted.Batch.ID || !duplicate.Deduplicated || accepted.Batch.Total != 20 {
		t.Fatalf("acceptance %+v %+v", accepted, duplicate)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); svc.Serve(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	wait := func(id, state string) ETFBatch {
		t.Helper()
		until := time.Now().Add(8 * time.Second)
		for {
			var response struct {
				Batch ETFBatch `json:"batch"`
			}
			etfHTTP(t, "GET", api+"/"+id, "", 200, &response)
			if response.Batch.State == state {
				return response.Batch
			}
			if time.Now().After(until) {
				t.Fatalf("batch did not reach %s: %+v", state, response.Batch)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	failed := wait(accepted.Batch.ID, "partial")
	if failed.Success != 18 || failed.Failed != 2 || len(failed.Items) != 20 {
		t.Fatalf("partial summary %+v", failed)
	}
	source.mu.Lock()
	source.failed = map[string]bool{}
	source.mu.Unlock()
	// Later time must not extend the retry's original range.
	svc.Now = func() time.Time { return at("20250110") }
	var retry struct {
		Batch ETFBatch `json:"batch"`
	}
	etfHTTP(t, "POST", api+"/"+failed.ID+"/retry", "", 202, &retry)
	if retry.Batch.ParentID != failed.ID || retry.Batch.Total != 2 || retry.Batch.EndDate != "20250103" {
		t.Fatalf("retry targets changed: %+v", retry.Batch)
	}
	completed := wait(retry.Batch.ID, "succeeded")
	if completed.Success != 2 {
		t.Fatal(completed)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	for i := 0; i < 20; i++ {
		code := fmt.Sprintf("500%03d.SH", i)
		wantAdj := 1
		if i >= 18 {
			wantAdj = 2
		}
		if source.calls[code+"/daily"] != 1 || source.calls[code+"/adj"] != wantAdj {
			t.Fatalf("successful work repeated %s: %+v", code, source.calls)
		}
	}
}

func TestETFHTTPConcurrentSubmissionReusesOneAcceptedBatch(t *testing.T) {
	db := testDB(t)
	_, err := db.Exec("INSERT INTO instrument(ts_code,name) VALUES ('510880.SH','红利 ETF')")
	check(t, err)
	svc := &ETFService{Store: NewETFStore(db), DefaultStart: "20250101", ChunkDays: 31, Now: func() time.Time { return at("20250103") }}
	api := etfAPI(t, svc)
	start := make(chan struct{})
	ids := make(chan string, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req, err := http.NewRequest("POST", api, strings.NewReader(`{}`))
			if err != nil {
				t.Error(err)
				return
			}
			req.Header.Set("X-Api-Key", "fixture-only")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer res.Body.Close()
			if res.StatusCode != 200 && res.StatusCode != 202 {
				t.Errorf("submission status %d", res.StatusCode)
				return
			}
			var value struct {
				Batch ETFBatch `json:"batch"`
			}
			if err = json.NewDecoder(res.Body).Decode(&value); err != nil {
				t.Error(err)
				return
			}
			ids <- value.Batch.ID
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatalf("duplicate accepted batches: %v", unique)
	}
}

func TestETFLegacyWaiterDisconnectDoesNotCancelBackgroundBatch(t *testing.T) {
	db := testDB(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	source := &etfFixture{failed: map[string]bool{}, calls: map[string]int{}, before: func(ctx context.Context, code, stage string) error {
		once.Do(func() { close(entered) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}}
	svc := &ETFService{Store: NewETFStore(db), Source: source, DefaultStart: "20250101", ChunkDays: 31, Now: func() time.Time { return at("20250103") }}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); svc.Serve(ctx) }()
	t.Cleanup(func() { stop(); <-done })
	request, cancel := context.WithCancel(context.Background())
	returned := make(chan core.Summary, 1)
	go func() { returned <- svc.Run(request, []string{"510880.SH"}) }()
	select {
	case <-entered:
	case <-time.After(4 * time.Second):
		t.Fatal("legacy request did not enter durable worker")
	}
	cancel()
	select {
	case sum := <-returned:
		if sum.Success != 0 {
			t.Fatal("accepted work reported completed", sum)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnected waiter did not return")
	}
	close(release)
	api := etfAPI(t, svc)
	until := time.Now().Add(4 * time.Second)
	for {
		var response struct {
			Batches []ETFBatch `json:"batches"`
		}
		etfHTTP(t, "GET", api, "", 200, &response)
		if len(response.Batches) == 1 && response.Batches[0].State == "succeeded" {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("background work cancelled with waiter: %+v", response)
		}
		time.Sleep(10 * time.Millisecond)
	}
	sum := svc.Run(context.Background(), []string{"510880.SH"})
	if sum.Success != 1 || sum.Total != 1 || !sum.Results[0].Succeeded() {
		t.Fatalf("legacy summary not preserved: %+v", sum)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.calls["510880.SH/daily"] != 1 || source.calls["510880.SH/adj"] != 1 {
		t.Fatal("completed range was fetched again", source.calls)
	}
}

func TestETFHTTPRecoveryAndCancellationPreserveCompletedDailyStep(t *testing.T) {
	for _, mode := range []string{"incremental", "historical"} {
		t.Run(mode, func(t *testing.T) {
			for _, cancelBatch := range []bool{false, true} {
				t.Run(fmt.Sprint(cancelBatch), func(t *testing.T) {
					db := testDB(t)
					entered, release := make(chan struct{}), make(chan struct{})
					var once sync.Once
					source := &etfFixture{failed: map[string]bool{}, calls: map[string]int{}, before: func(ctx context.Context, code, stage string) error {
						if stage != "adj" {
							return nil
						}
						once.Do(func() { close(entered) })
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-release:
							return nil
						}
					}}
					svc := &ETFService{Store: NewETFStore(db), Source: source, DefaultStart: "20250101", ChunkDays: 31, Now: func() time.Time { return at("20250103") }}
					api := etfAPI(t, svc)
					var accepted struct {
						Batch ETFBatch `json:"batch"`
					}
					body := `{"codes":["510880.SH"]}`
					if mode == "historical" {
						body = `{"codes":["510880.SH"],"mode":"historical","startDate":"20250102","endDate":"20250103"}`
					}
					etfHTTP(t, "POST", api, body, 202, &accepted)
					ctx, stop := context.WithCancel(context.Background())
					done := make(chan struct{})
					go func() { defer close(done); svc.Serve(ctx) }()
					t.Cleanup(func() { stop(); <-done })
					select {
					case <-entered:
					case <-time.After(4 * time.Second):
						t.Fatal("factor step never started")
					}
					var mid struct {
						Batch ETFBatch `json:"batch"`
					}
					etfHTTP(t, "GET", api+"/"+accepted.Batch.ID, "", 200, &mid)
					if mid.Batch.Items[0].DailyCheckpoint != "20250103" || mid.Batch.Items[0].DailyRows != 1 {
						t.Fatal("daily checkpoint absent", mid)
					}
					if cancelBatch {
						etfHTTP(t, "POST", api+"/"+accepted.Batch.ID+"/cancel", "", 200, nil)
						close(release)
					} else {
						stop()
					}
					if !cancelBatch {
						<-done
					}
					expected := "queued"
					if cancelBatch {
						expected = "cancelled"
					}
					until := time.Now().Add(3 * time.Second)
					for {
						etfHTTP(t, "GET", api+"/"+accepted.Batch.ID, "", 200, &mid)
						if mid.Batch.State == expected {
							break
						}
						if time.Now().After(until) {
							t.Fatalf("expected %s: %+v", expected, mid)
						}
						time.Sleep(10 * time.Millisecond)
					}
					stop()
					<-done
					source.before = nil
					fresh := &ETFService{Store: NewETFStore(db), Source: source, DefaultStart: "20100101", ChunkDays: 1, Now: func() time.Time { return at("20250110") }}
					ctx2, stop2 := context.WithCancel(context.Background())
					done2 := make(chan struct{})
					go func() { defer close(done2); fresh.Serve(ctx2) }()
					t.Cleanup(func() { stop2(); <-done2 })
					if cancelBatch {
						time.Sleep(1100 * time.Millisecond)
						etfHTTP(t, "GET", api+"/"+accepted.Batch.ID, "", 200, &mid)
						if mid.Batch.State != "cancelled" || mid.Batch.Items[0].AdjRows != 0 {
							t.Fatal("cancelled batch resumed", mid)
						}
					} else {
						until = time.Now().Add(3 * time.Second)
						for {
							etfHTTP(t, "GET", api+"/"+accepted.Batch.ID, "", 200, &mid)
							if mid.Batch.State == "succeeded" {
								break
							}
							if time.Now().After(until) {
								t.Fatal("recovery failed", mid)
							}
							time.Sleep(10 * time.Millisecond)
						}

						var audit struct {
							Batch struct {
								Events []struct {
									Kind string `json:"kind"`
									Code string `json:"code"`
								} `json:"events"`
							} `json:"batch"`
						}
						etfHTTP(t, "GET", api+"/"+accepted.Batch.ID, "", 200, &audit)
						seen := map[string]bool{}
						for _, event := range audit.Batch.Events {
							if event.Code == "510880.SH" {
								seen[event.Kind] = true
							}
						}
						if !seen["interrupted"] || !seen["resumed"] {
							t.Fatalf("interruption and recovery not queryable: %+v", audit)
						}
						if mid.Batch.EndDate != "20250103" || mid.Batch.Items[0].ChunkDays != 31 || mid.Batch.Mode != mode || (mode == "historical" && mid.Batch.StartDate != "20250102") {
							t.Fatal("recovery changed frozen plan", mid)
						}
					}
					source.mu.Lock()
					defer source.mu.Unlock()
					if source.calls["510880.SH/daily"] != 1 {
						t.Fatal("recovery requested saved daily step", source.calls)
					}
				})
			}
		})
	}
}

func TestETFHTTPPreservesCurrentDayScopeBeforeEveningReport(t *testing.T) {
	db := testDB(t)
	service := &ETFService{Store: NewETFStore(db), DefaultStart: "20250101", ChunkDays: 31, Now: func() time.Time { return time.Date(2025, 1, 3, 7, 30, 0, 0, time.UTC) }}
	var result struct {
		Batch ETFBatch `json:"batch"`
	}
	etfHTTP(t, "POST", etfAPI(t, service), `{"codes":["510880.SH"]}`, 202, &result)
	if result.Batch.EndDate != "20250103" {
		t.Fatalf("15:30 ETF request excluded the current close: %+v", result.Batch)
	}
}
