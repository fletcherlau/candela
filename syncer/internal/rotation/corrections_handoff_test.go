package rotation

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCorrectionHTTPNewInputCannotBeFailedByEarlierSyncStateRead(t *testing.T) {
	f := seedContinuousCorrection(t)
	ctx := context.Background()
	// Approved isolated database fault boundary: pause the ETF execution-state
	// read after the publisher observed the old syncing revision, then commit the
	// new pending revision. This models the synchronizer's atomic completion.
	gate, err := f.service.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	if _, err := gate.ExecContext(ctx, "LOCK TABLES etf_sync_run WRITE"); err != nil {
		t.Fatal(err)
	}
	unlocked := false
	defer func() {
		if !unlocked {
			gate.ExecContext(ctx, "UNLOCK TABLES")
		}
	}()
	if _, err := f.service.DB.ExecContext(ctx, "UPDATE rotation_result SET status='syncing',revision=revision+1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- f.service.Refresh(ctx) }()
	until := time.Now().Add(5 * time.Second)
	for {
		var blocked int
		err := f.service.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST
   WHERE ID=IS_USED_LOCK('candela_etf_publication')
   AND STATE='Waiting for table metadata lock' AND INFO LIKE '%etf_sync_run%'`).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked == 1 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("publisher did not reach controlled execution-state read")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := f.service.DB.ExecContext(ctx, "UPDATE rotation_result SET status='pending',revision=revision+1,message='raw input completed' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.ExecContext(ctx, "UNLOCK TABLES"); err != nil {
		t.Fatal(err)
	}
	unlocked = true
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var view RangeView
	captureHTTP(t, "GET", f.rangeURL, "", 200, &view)
	if view.Status != "pending" || strings.Contains(view.Message, "中断") {
		t.Fatalf("old syncing observation replaced newer pending input: %+v", view)
	}
	if err := f.service.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	captureHTTP(t, "GET", f.rangeURL, "", 200, &view)
	if view.Status != "ready" {
		t.Fatalf("new revision no longer publishes automatically: %+v", view)
	}
}
