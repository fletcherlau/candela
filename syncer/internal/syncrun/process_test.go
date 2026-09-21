package syncrun

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

// This child is a real, killable service process, using the production worker
// and store. Only its market-data Source is replaced with a loopback fixture.
func TestRecoveryWorkerProcess(t *testing.T) {
	if os.Getenv("CANDELA_RECOVERY_CHILD") != "1" {
		t.Skip("subprocess fixture only")
	}
	db, err := sql.Open("mysql", os.Getenv("SYNC_RUN_TEST_DSN"))
	check(t, err)
	defer db.Close()
	var name string
	check(t, db.QueryRow("SELECT DATABASE()").Scan(&name))
	if name != "candela_sync_test" {
		t.Fatal("refusing non-test database")
	}
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Minute)
	defer stop()
	(&Worker{NewStore(db), &processSource{url: os.Getenv("CANDELA_RECOVERY_SOURCE")}}).Serve(ctx)
}

type processSource struct{ url string }

func (s *processSource) Earliest(ctx context.Context, end string) (string, string, error) {
	var result struct{ Date string }
	err := s.read(ctx, "/earliest?end="+end, &result)
	return result.Date, "isolated process source fixture", err
}
func (s *processSource) Window(ctx context.Context, from, to string) ([]Bar, error) {
	var result []Bar
	err := s.read(ctx, "/window?from="+from+"&to="+to, &result)
	return result, err
}
func (s *processSource) read(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.url+path, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return json.NewDecoder(res.Body).Decode(out)
}

type childWorker struct {
	cmd    *exec.Cmd
	done   chan struct{}
	err    error
	output bytes.Buffer
}

func startChildWorker(t *testing.T, sourceURL string) *childWorker {
	t.Helper()
	p := &childWorker{cmd: exec.Command(os.Args[0], "-test.run=^TestRecoveryWorkerProcess$"), done: make(chan struct{})}
	p.cmd.Env = append(os.Environ(), "CANDELA_RECOVERY_CHILD=1", "CANDELA_RECOVERY_SOURCE="+sourceURL)
	p.cmd.Stdout = &p.output
	p.cmd.Stderr = &p.output
	check(t, p.cmd.Start())
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() { p.stop(t) })
	return p
}
func (p *childWorker) stop(t *testing.T) {
	t.Helper()
	_ = p.cmd.Process.Kill()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("child worker did not exit")
	}
}
func TestMySQLHTTPRealProcessRestart(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "resume"
		if cancelled {
			name = "cancel_intent_survives_crash"
		}
		t.Run(name, func(t *testing.T) {
			db := testDB(t)
			st := NewStore(db)
			api := maintenanceAPI(t, st)
			run, _, err := st.Submit(context.Background(), "backfill", at("20260914"))
			check(t, err)
			calls := &heldSource{calls: make(chan sourceCall, 4)}
			var discoveries atomic.Int32
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/earliest" {
					discoveries.Add(1)
					_ = json.NewEncoder(w).Encode(map[string]string{"Date": "20240909"})
					return
				}
				c := sourceCall{r.URL.Query().Get("from"), r.URL.Query().Get("to"), make(chan []Bar, 1)}
				select {
				case calls.calls <- c:
				case <-r.Context().Done():
					return
				}
				select {
				case bars := <-c.reply:
					_ = json.NewEncoder(w).Encode(bars)
				case <-r.Context().Done():
					return
				}
			}))
			t.Cleanup(source.Close)
			old := startChildWorker(t, source.URL)
			first := nextSourceCall(t, calls)
			first.reply <- fixtureBars(first.from, first.to)
			pending := nextSourceCall(t, calls)
			if cancelled {
				maintenanceRequest(t, "POST", api+"/"+run.ID+"/cancel", "", 200)
			}
			old.stop(t)
			_, err = db.Exec("UPDATE sync_run SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", run.ID)
			check(t, err)
			fresh := startChildWorker(t, source.URL)
			if !cancelled {
				for {
					c := nextSourceCall(t, calls)
					if c.from != pending.from || c.to > run.EndDate {
						t.Fatalf("replayed or extended segment: %+v", c)
					}
					c.reply <- fixtureBars(c.from, c.to)
					if c.to == run.EndDate {
						break
					}
					pending.from = date(c.to).AddDate(0, 0, 1).Format("20060102")
				}
			}
			want := "succeeded"
			if cancelled {
				want = "cancelled"
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				got := maintenanceRequest(t, "GET", api+"/"+run.ID, "", 200)
				if got.State == want {
					if got.EndDate != "20260914" || (cancelled && got.Checkpoint != first.to) || (!cancelled && (got.CompletedSegments != 3 || got.ProcessedRows != 526)) {
						t.Fatalf("reconstructed process result: %+v", got)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("process restart result: %+v", got)
				}
				time.Sleep(20 * time.Millisecond)
			}
			fresh.stop(t)
			if discoveries.Load() != 1 {
				t.Fatalf("rediscovered frozen history %d times", discoveries.Load())
			}
			if cancelled {
				assertMaintenanceEvents(t, api+"/"+run.ID, "cancel_requested", "cancelled")
			} else {
				assertMaintenanceEvents(t, api+"/"+run.ID, "interrupted", "resumed")
			}
		})
	}
}
