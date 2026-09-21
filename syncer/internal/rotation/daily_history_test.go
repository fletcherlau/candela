package rotation

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDailyHistoryHTTPListsSavedDatesAndPreservesManualSelection(t *testing.T) {
	db, svc, source := seedReferenceScenario(t, "20250103")
	if err := svc.PublishClose(context.Background(), "20250102"); err != nil {
		t.Fatal(err)
	}
	api := captureAPI(t, svc)
	captureHTTP(t, "POST", api, `{"tradeDate":"20250103"}`, 202, nil)
	startCaptureWorker(t, svc)
	startReferenceWorker(t, svc)
	waitCapture(t, api, "20250103", "captured")
	daily := strings.TrimSuffix(api, capturePath) + "/api/v1/rotation/daily"
	// Publication readiness is observed through the same endpoint the page reads.
	until := time.Now().Add(3 * time.Second)
	for {
		var v DailyView
		captureHTTP(t, "GET", daily+"?tradeDate=20250103", "", 200, &v)
		if v.Reference != nil {
			break
		}
		if time.Now().After(until) {
			t.Fatal("reference was not published")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A capture-only older record is an archive entry even without a publication.
	// It precedes the backtest's ten-year window and must not be silently pruned.
	if _, err := db.Exec(`INSERT INTO rotation_capture_run(trade_date,target_at,deadline_at,state,stage,params,created_at,updated_at)
 VALUES ('20100104','2010-01-04T14:45:00+08:00','2010-01-04T14:46:00+08:00','missing','missing','{}',NOW(6),NOW(6))`); err != nil {
		t.Fatal(err)
	}
	var first, second, third struct {
		Status       string `json:"status"`
		EarliestDate string `json:"earliestDate"`
		LatestDate   string `json:"latestDate"`
		NextBefore   string `json:"nextBefore"`
		Dates        []struct {
			TradeDate string     `json:"tradeDate"`
			Reference DailyStage `json:"reference"`
			Close     DailyStage `json:"close"`
		} `json:"dates"`
	}
	captureHTTP(t, "GET", daily+"/dates?limit=1", "", 200, &first)
	if first.EarliestDate != "20100104" || first.LatestDate != "20250103" || len(first.Dates) != 1 || first.Dates[0].TradeDate != "20250103" || first.Dates[0].Reference.Status != "ready" || first.NextBefore != "20250103" {
		t.Fatalf("first page: %+v", first)
	}
	captureHTTP(t, "GET", daily+"/dates?limit=1&before="+first.NextBefore, "", 200, &second)
	if len(second.Dates) != 1 || second.Dates[0].TradeDate != "20250102" || second.Dates[0].Close.Status != "ready" || second.Dates[0].Reference.Status != "missing" {
		t.Fatalf("second page: %+v", second)
	}
	captureHTTP(t, "GET", daily+"/dates?limit=1&before="+second.NextBefore, "", 200, &third)
	if len(third.Dates) != 1 || third.Dates[0].TradeDate != "20100104" || third.NextBefore != "" {
		t.Fatalf("final page: %+v", third)
	}
	for _, date := range []string{"20100104", "20241231"} {
		var view DailyView
		captureHTTP(t, "GET", daily+"?tradeDate="+date, "", 200, &view)
		if view.TradeDate != date || view.SelectionMode != "manual" || view.Reference != nil || view.Close != nil || view.Fallback {
			t.Fatalf("manual missing date replaced: %+v", view)
		}
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=x", "?limit=1&limit=2", "?before=20250230", "?before=20250104", "?before=", "?other=x"} {
		captureHTTP(t, "GET", daily+"/dates"+query, "", 400, nil)
	}
	for _, date := range []string{"20250230", "20250104", "2025-01-02"} {
		captureHTTP(t, "GET", daily+"?tradeDate="+date, "", 400, nil)
	}
	if source.count() != 4 {
		t.Fatal("history reads fetched quotes")
	}
}

func TestDailyHistoryHTTPEmptyArchiveDoesNotInventCalendarRecords(t *testing.T) {
	_, svc, source := seedReferenceScenario(t, "20250103")
	api := captureAPI(t, svc)
	var index DailyDates
	captureHTTP(t, "GET", strings.TrimSuffix(api, capturePath)+"/api/v1/rotation/daily/dates", "", 200, &index)
	if index.Status != "ready" || len(index.Dates) != 0 || index.EarliestDate != "" || index.LatestDate != "" || index.NextBefore != "" {
		t.Fatalf("calendar inferred as archive: %+v", index)
	}
	if source.count() != 0 {
		t.Fatal("archive read fetched source")
	}
}
