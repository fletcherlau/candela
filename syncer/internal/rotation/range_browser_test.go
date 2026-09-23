package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/go-sql-driver/mysql"
	"math"
	"net/http"
	"os"
	"syncer/internal/schema"
	"testing"
	"time"
)

// This fixture host is compiled only for tests, never into the service binary.
func TestRangeBrowserHarness(t *testing.T) {
	if os.Getenv("CANDELA_RANGE_BROWSER_TEST") != "1" {
		t.Skip("range browser harness is opt-in")
	}
	dsn := os.Getenv("ROTATION_RANGE_TEST_DSN")
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.DBName != "candela_range_test" {
		t.Fatal("isolated candela_range_test required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := schema.Ensure(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	codes := []string{"510880.SH", "518880.SH", "159915.SZ", "513100.SH"}
	rows := []map[string]any{}
	for i := 0; i < 700; i++ {
		rows = append(rows, map[string]any{"date": time.Date(2024, 10, 1+i, 0, 0, 0, 0, time.UTC).Format("20060102"), "nav": 1 + float64(i)*.0005 + math.Sin(float64(i)/25)*.025, "holding": codes[(i/80)%4], "weight": .7, "cashWeight": .3, "cost": 0, "turnover": 0, "benchmarks": []float64{1 + float64(i)*.0002, 1 + float64(i)*.0006, 1 + math.Sin(float64(i)/40)*.08, 1 + float64(i)*.0004}})
	}
	initial, _ := json.Marshal(map[string]any{"version": "fixture", "start": rows[0]["date"], "end": rows[len(rows)-1]["date"], "costBps": 10, "codes": codes, "names": []string{"红利 ETF", "黄金 ETF", "创业板 ETF", "纳指 ETF"}, "days": rows})
	save := func(payload []byte) error {
		_, err := db.Exec("UPDATE rotation_result SET payload=?,status='ready',message='',revision=revision+1,updated_at=CURRENT_TIMESTAMP(6) WHERE id=1", payload)
		return err
	}
	if err := save(initial); err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db, Now: func() time.Time { return time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC) }}
	t.Fatal(http.ListenAndServe("127.0.0.1:18085", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "fixture-only" {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/fixture" && r.Method == http.MethodPost {
			var body json.RawMessage
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&body) != nil {
				http.Error(w, "bad fixture", 400)
				return
			}
			if string(body) == "null" {
				body = initial
			}
			if err := save(body); err != nil {
				http.Error(w, "fixture unavailable", 500)
				return
			}
			w.WriteHeader(204)
			return
		}
		if r.URL.Path != "/api/v1/rotation/backtest/range" {
			http.NotFound(w, r)
			return
		}
		svc.RangeHandler().ServeHTTP(w, r)
	})))
}
