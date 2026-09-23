package rotation

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"syncer/internal/syncrun"
	"testing"
	"time"
)

func TestDurableETFQueueIsNotMistakenForInterruptedLegacySync(t *testing.T) {
	db, svc, _ := seedReferenceScenario(t, "20250103")
	jobs := &syncrun.ETFService{Store: syncrun.NewETFStore(db), DefaultStart: "20250101", ChunkDays: 31, Now: func() time.Time { return time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC) }}
	if _, _, err := jobs.Submit(context.Background(), []string{"510880.SH"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/rotation/backtest", nil))
	var v View
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Status != "syncing" {
		t.Fatalf("accepted durable job misreported as abandoned: %+v", v)
	}
}
