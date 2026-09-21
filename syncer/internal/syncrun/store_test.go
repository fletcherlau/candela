package syncrun

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"syncer/internal/schema"
	"syncer/internal/store"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SYNC_RUN_TEST_DSN")
	if dsn == "" {
		t.Skip("SYNC_RUN_TEST_DSN must name an isolated candela_sync_test database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var name string
	if err = db.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "candela_sync_test" {
		t.Fatal("refusing non-test database")
	}
	if err = schema.Ensure(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DROP TRIGGER IF EXISTS reject_checkpoint", "UPDATE index_series SET active_run=NULL", "DELETE FROM sync_run", "DELETE FROM index_daily", "DELETE FROM instrument", "DELETE FROM etf_daily", "DELETE FROM etf_adj_factor", "DELETE FROM intraday_snapshot"} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func at(s string) time.Time { t, _ := time.Parse("20060102", s); return t.Add(12 * time.Hour) }
func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	check(t, db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&n))
	return n
}

func TestMySQLFullBackfillAndIncremental(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	st := NewStore(db)
	f := &sourceFixture{bars: fixtureBars("20041231", "20070108")}
	worker := Worker{st, sourceFor(t, f)}
	request, cancel := context.WithCancel(ctx)
	accepted, duplicate, err := st.Submit(request, "backfill", at("20070108"))
	check(t, err)
	cancel()
	if duplicate || accepted.State != "queued" || accepted.StartDate != HistoryFloor || accepted.EndDate != "20070108" || count(t, db, "index_daily") != 0 || f.calls != 0 {
		t.Fatalf("acceptance: %+v", accepted)
	}
	versionsBeforeReplay := count(t, db, "schema_migration")
	// Simulate DDL applied before the migration version could be recorded.
	_, err = db.Exec("DELETE FROM schema_migration WHERE version=1")
	check(t, err)
	// Replay preserves accepted work and doesn't seed an ETF Instrument.
	check(t, schema.Ensure(ctx, db))
	if count(t, db, "schema_migration") != versionsBeforeReplay {
		t.Fatal("migration not versioned")
	}
	claimed, err := st.Claim(ctx)
	check(t, err)
	worker.Execute(ctx, claimed)
	done, err := NewStore(db).Get(ctx, accepted.ID)
	check(t, err)
	if done.State != "succeeded" || done.EffectiveStart != "20041231" || done.Checkpoint != "20070108" || done.CompletedSegments != done.TotalSegments || done.ProcessedRows != int64(len(f.bars)) || done.HistoryEvidence == "" {
		t.Fatalf("backfill: %+v", done)
	}
	for _, table := range []string{"instrument", "etf_daily", "etf_adj_factor", "intraday_snapshot"} {
		if count(t, db, table) != 0 {
			t.Fatalf("index polluted %s", table)
		}
	}
	catalog := store.NewMySQLStore(db).Catalog(ctx)
	item := catalog.Groups[0].Items[0]
	if item.StartDate != "20041231" || item.EndDate != "20070108" || *item.Rows != int64(len(f.bars)) {
		t.Fatalf("catalog %+v", item)
	}
	// Reprocessing an unchanged last day counts one processed row, not zero changed rows.
	for i := 0; i < 2; i++ {
		r, _, err := st.Submit(ctx, "incremental", at("20070108"))
		check(t, err)
		if r.StartDate != "20070108" {
			t.Fatalf("frozen incremental %+v", r)
		}
		claim, err := st.Claim(ctx)
		check(t, err)
		worker.Execute(ctx, claim)
		r, err = st.Get(ctx, r.ID)
		check(t, err)
		if r.State != "succeeded" || r.ProcessedRows != 1 || count(t, db, "index_daily") != len(f.bars) {
			t.Fatalf("idempotence: %+v", r)
		}
	}
	// A later request adds new source data while preserving the original fixed cutoff.
	f.bars = fixtureBars("20041231", "20070110")
	r, _, err := st.Submit(ctx, "incremental", at("20070110"))
	check(t, err)
	claim, err := st.Claim(ctx)
	check(t, err)
	worker.Execute(ctx, claim)
	r, err = st.Get(ctx, r.ID)
	check(t, err)
	if r.State != "succeeded" || r.StartDate != "20070108" || r.Checkpoint != "20070110" || r.ProcessedRows != 3 || count(t, db, "index_daily") != len(f.bars) {
		t.Fatalf("incremental %+v", r)
	}
}
func TestMySQLConcurrentDedupQueueAndFencing(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	st := NewStore(db)
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _, err := st.Submit(ctx, "backfill", at("20240909"))
			ids <- r.ID
			errs <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		check(t, err)
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("concurrent duplicate accepted twice")
		}
	}
	old, err := st.Claim(ctx)
	check(t, err)
	same, dup, err := st.Submit(ctx, "backfill", at("20240909"))
	check(t, err)
	if !dup || same.ID != old.ID {
		t.Fatal("running request not deduplicated")
	}
	queued, dup, err := st.Submit(ctx, "incremental", at("20240909"))
	check(t, err)
	if dup || queued.State != "queued" {
		t.Fatal("different request not queued")
	}
	_, err = st.Claim(ctx)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("two owners for one object", err)
	}
	check(t, st.Plan(ctx, old, "20240909", "fixture", 1))
	_, err = db.Exec("UPDATE sync_run SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", old.ID)
	check(t, err)
	// Even before another worker claims, an expired writer is rejected.
	if !errors.Is(st.CommitWindow(ctx, old, "20240909", "20240909", fixtureBars("20240909", "20240909")), ErrOwnership) {
		t.Fatal("expired write accepted")
	}
	next, err := st.Claim(ctx)
	check(t, err)
	if next.ID != queued.ID || next.Owner <= old.Owner {
		t.Fatal("wrong queue/fence")
	}
	for _, err := range []error{st.Heartbeat(ctx, old), st.Finish(ctx, old, "", ""), st.Plan(ctx, old, "20240909", "", 1)} {
		if !errors.Is(err, ErrOwnership) {
			t.Fatal("stale worker accepted", err)
		}
	}
	failed, err := st.Get(ctx, old.ID)
	check(t, err)
	if failed.State != "failed" || failed.ErrorCode != "interrupted" || count(t, db, "index_daily") != 0 {
		t.Fatalf("lost owner %+v", failed)
	}
}
func TestMySQLDataAndCheckpointRollbackTogether(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	st := NewStore(db)
	_, _, err := st.Submit(ctx, "backfill", at("20240909"))
	check(t, err)
	r, err := st.Claim(ctx)
	check(t, err)
	check(t, st.Plan(ctx, r, "20240909", "fixture", 1))
	_, err = db.Exec(`CREATE TRIGGER reject_checkpoint BEFORE UPDATE ON sync_run FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture checkpoint failure'`)
	check(t, err)
	err = st.CommitWindow(ctx, r, "20240909", "20240909", fixtureBars("20240909", "20240909"))
	if err == nil {
		t.Fatal("expected write failure")
	}
	_, err = db.Exec("DROP TRIGGER reject_checkpoint")
	check(t, err)
	saved, err := st.Get(ctx, r.ID)
	check(t, err)
	if count(t, db, "index_daily") != 0 || saved.Checkpoint != "" || saved.ProcessedRows != 0 {
		t.Fatalf("partial commit %+v", saved)
	}
	check(t, st.CommitWindow(ctx, r, "20240909", "20240909", fixtureBars("20240909", "20240909")))
	if st.CommitWindow(ctx, r, "20240909", "20240909", fixtureBars("20240909", "20240909")) == nil {
		t.Fatal("repeated checkpoint counted twice")
	}
}
func TestMySQLPartialFailureRetainsCommittedWindows(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	st := NewStore(db)
	f := &sourceFixture{bars: fixtureBars("20041231", "20070108"), gap: "20060103"}
	w := Worker{st, sourceFor(t, f)}
	r, _, err := st.Submit(ctx, "backfill", at("20070108"))
	check(t, err)
	claimed, err := st.Claim(ctx)
	check(t, err)
	w.Execute(ctx, claimed)
	r, err = st.Get(ctx, r.ID)
	check(t, err)
	if r.State != "failed" || r.ErrorCode != "source_gap" || r.CompletedSegments != 1 || r.Checkpoint != "20051231" || count(t, db, "index_daily") != 261 || r.ProcessedRows != 261 {
		t.Fatalf("partial failure %+v", r)
	}
}
func TestMySQLPermissionFailureIsPersisted(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	st := NewStore(db)
	src := sourceFor(t, &sourceFixture{code: 2002, message: "权限不足"})
	r, _, err := st.Submit(ctx, "backfill", at("20240909"))
	check(t, err)
	claimed, err := st.Claim(ctx)
	check(t, err)
	(&Worker{st, src}).Execute(ctx, claimed)
	r, err = st.Get(ctx, r.ID)
	check(t, err)
	if r.State != "failed" || r.ErrorCode != "permission_denied" || r.ProcessedRows != 0 {
		t.Fatalf("permission %+v", r)
	}
}

func TestMySQLIncrementalDedupKeepsOriginalStartAsCoverageAdvances(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	st := NewStore(db)
	_, err := db.Exec("INSERT INTO index_daily(ts_code,trade_date,open,high,low,close,pre_close,change_amt,pct_chg,vol,amount) VALUES (?,'20240909',100,102,99,101,100,1,1,1000,10000)", Code)
	check(t, err)
	accepted, _, err := st.Submit(ctx, "incremental", at("20240910"))
	check(t, err)
	r, err := st.Claim(ctx)
	check(t, err)
	check(t, st.Plan(ctx, r, r.StartDate, "fixture", 1))
	if st.Finish(ctx, r, "", "") == nil {
		t.Fatal("unfinished task marked successful")
	}
	check(t, st.CommitWindow(ctx, r, "20240909", "20240910", fixtureBars("20240909", "20240910")))
	again, dup, err := st.Submit(ctx, "incremental", at("20240910"))
	check(t, err)
	if !dup || again.ID != accepted.ID || again.StartDate != "20240909" {
		t.Fatalf("moving request boundary %+v", again)
	}
	check(t, st.Finish(ctx, r, "", "done"))
}

func TestMySQLInceptionNullsAndMigrationFromVersionOne(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	st := NewStore(db)
	// Restore the deployed v1 column constraints and seed a non-null existing row.
	_, err := db.Exec(`ALTER TABLE index_daily MODIFY pre_close DECIMAL(16,4) NOT NULL, MODIFY change_amt DECIMAL(16,4) NOT NULL, MODIFY pct_chg DECIMAL(16,4) NOT NULL, MODIFY vol DECIMAL(24,4) NOT NULL, MODIFY amount DECIMAL(24,4) NOT NULL`)
	check(t, err)
	_, err = db.Exec("DELETE FROM schema_migration WHERE version=2")
	check(t, err)
	_, err = db.Exec("INSERT INTO index_daily(ts_code,trade_date,open,high,low,close,pre_close,change_amt,pct_chg,vol,amount) VALUES (?,'20041231',1000,1000,1000,1000,999,1,0.1,50,500)", Code)
	check(t, err)
	check(t, schema.Ensure(ctx, db))
	var before float64
	check(t, db.QueryRow("SELECT vol FROM index_daily WHERE trade_date='20041231'").Scan(&before))
	if before != 50 {
		t.Fatal("migration changed existing value")
	}
	// Simulate ALTER committed but migration version was not yet recorded.
	_, err = db.Exec("DELETE FROM schema_migration WHERE version=2")
	check(t, err)
	check(t, schema.Ensure(ctx, db))
	accepted, _, err := st.Submit(ctx, "backfill", at("20041231"))
	check(t, err)
	r, err := st.Claim(ctx)
	check(t, err)
	(&Worker{st, inceptionSource(t)}).Execute(ctx, r)
	saved, err := st.Get(ctx, accepted.ID)
	check(t, err)
	if saved.State != "succeeded" || saved.ProcessedRows != 1 || saved.EffectiveStart != "20041231" {
		t.Fatalf("inception backfill failed: %+v", saved)
	}
	var price float64
	var prev, change, pct, vol, amount sql.NullFloat64
	check(t, db.QueryRow("SELECT close,pre_close,change_amt,pct_chg,vol,amount FROM index_daily WHERE ts_code=? AND trade_date='20041231'", Code).Scan(&price, &prev, &change, &pct, &vol, &amount))
	if price != 1000 || prev.Valid || change.Valid || pct.Valid || vol.Valid || amount.Valid {
		t.Fatal("source nulls were lost during upsert")
	}
	var required int
	check(t, db.QueryRow("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='index_daily' AND column_name IN ('open','high','low','close') AND is_nullable='NO'").Scan(&required))
	if required != 4 {
		t.Fatal("required price constraints weakened")
	}
}
