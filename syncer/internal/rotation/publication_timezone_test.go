package rotation

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestDailyHTTPPublicationTimesWithShanghaiDatabaseSession(t *testing.T) {
	dsn := os.Getenv("ROTATION_DAILY_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated MySQL required")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Loc = time.FixedZone("Asia/Shanghai", 8*3600)
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+08:00'"
	t.Setenv("ROTATION_DAILY_TEST_DSN", cfg.FormatDSN())
	_, svc, _ := seedReferenceScenario(t, "20250103")
	api := recoveryAPI(t, svc)
	started := time.Now().Add(-2 * time.Second)
	checkTime := func(label, value string) {
		t.Helper()
		stamp, err := time.Parse(time.RFC3339, value)
		if err != nil || stamp.Before(started) || stamp.After(time.Now().Add(2*time.Second)) {
			t.Errorf("%s HTTP state time %q does not describe this publication; database session is +08:00", label, value)
		}
	}
	if err := svc.PublishClose(context.Background(), "20250102"); err != nil {
		t.Fatal(err)
	}
	close := dailyFromHTTP(t, api, "20250102")
	if close.Close == nil {
		t.Fatal("complete close missing")
	}
	checkTime("close", close.CloseState.UpdatedAt)
	captureHTTP(t, "POST", api+capturePath, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, svc)
	startReferenceWorker(t, svc)
	waitCapture(t, api+capturePath, "20250103", "captured")
	deadline := time.Now().Add(4 * time.Second)
	for {
		reference := dailyFromHTTP(t, api, "20250103")
		if reference.Reference != nil {
			checkTime("reference", reference.ReferenceState.UpdatedAt)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reference publication timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
