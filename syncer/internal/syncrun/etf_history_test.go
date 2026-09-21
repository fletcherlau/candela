package syncrun

import (
	"context"
	"syncer/internal/core"
	"syncer/internal/schema"
	"syncer/internal/store"
	"testing"
	"time"
)

// The source fixture represents corrected source records, including dates
// outside the requested window. Production still owns filtering and storage.
type etfHistorySource struct {
	bars    []core.Bar
	factors []core.AdjFactor
}

func (s *etfHistorySource) FetchDaily(_ context.Context, code, from, to string) ([]core.Bar, error) {
	var out []core.Bar
	for _, bar := range s.bars {
		if bar.TsCode == code && bar.TradeDate >= from && bar.TradeDate <= to {
			out = append(out, bar)
		}
	}
	return out, nil
}
func (s *etfHistorySource) FetchAdj(_ context.Context, code, from, to string) ([]core.AdjFactor, error) {
	var out []core.AdjFactor
	for _, factor := range s.factors {
		if factor.TsCode == code && factor.TradeDate >= from && factor.TradeDate <= to {
			out = append(out, factor)
		}
	}
	return out, nil
}
func waitETFHTTP(t *testing.T, api, id, state string) ETFBatch {
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
func serveETFForTest(t *testing.T, svc *ETFService) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); svc.Serve(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
}
func TestETFHistoricalHTTPRepairsOldHoleAndValuesWithoutChangingOutsideRange(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	raw := store.NewMySQLStore(db)
	bar := func(day string, price float64) core.Bar {
		return core.Bar{TsCode: "510880.SH", TradeDate: day, Open: price, High: price, Low: price, Close: price}
	}
	factor := func(day string, value float64) core.AdjFactor {
		return core.AdjFactor{TsCode: "510880.SH", TradeDate: day, AdjFactor: value}
	}
	// Newer records already exist, and Jan 3 is an old hole.
	_, err := raw.UpsertDaily(ctx, []core.Bar{bar("20250101", 10), bar("20250102", 20), bar("20250104", 40), bar("20250110", 100)})
	check(t, err)
	_, err = raw.UpsertAdjFactors(ctx, []core.AdjFactor{factor("20250101", 1), factor("20250102", 1), factor("20250104", 1), factor("20250110", 1)})
	check(t, err)
	source := &etfHistorySource{
		bars:    []core.Bar{bar("20250101", 999), bar("20250102", 22), bar("20250103", 33), bar("20250104", 999), bar("20250110", 999)},
		factors: []core.AdjFactor{factor("20250101", 9), factor("20250102", 2), factor("20250103", 3), factor("20250104", 9), factor("20250110", 9)},
	}
	svc := &ETFService{Store: NewETFStore(db), Source: source, DefaultStart: "20240101", ChunkDays: 1, Now: func() time.Time { return at("20250115") }}
	api := etfAPI(t, svc)
	var accepted struct {
		Batch ETFBatch `json:"batch"`
	}
	etfHTTP(t, "POST", api, `{"codes":["510880.SH"],"mode":"historical","startDate":"20250102","endDate":"20250103"}`, 202, &accepted)
	if len(accepted.Batch.Items) != 1 || accepted.Batch.Items[0].Mode != "historical" || accepted.Batch.Items[0].StartDate != "20250102" || accepted.Batch.EndDate != "20250103" {
		t.Fatalf("historical range not frozen: %+v", accepted.Batch)
	}
	serveETFForTest(t, svc)
	completed := waitETFHTTP(t, api, accepted.Batch.ID, "succeeded")
	if completed.Items[0].DailyCheckpoint != "20250103" || completed.Items[0].AdjCheckpoint != "20250103" || completed.Items[0].ProcessedRows != 4 {
		t.Fatalf("historical phases not completed: %+v", completed)
	}
	// Read through the existing Store seam: raw OHLC and aligned raw factors.
	rows, err := raw.RecentDaily(ctx, "510880.SH", 20)
	check(t, err)
	want := []struct {
		day           string
		price, factor float64
	}{{"20250101", 10, 1}, {"20250102", 22, 2}, {"20250103", 33, 3}, {"20250104", 40, 1}, {"20250110", 100, 1}}
	if len(rows) != len(want) {
		t.Fatalf("old hole not filled: %+v", rows)
	}
	for i, w := range want {
		got := rows[i]
		if got.TradeDate != w.day || got.Open != w.price || got.High != w.price || got.Low != w.price || got.Close != w.price || got.AdjFactor != w.factor {
			t.Fatalf("raw value mismatch on %s: %+v", w.day, got)
		}
	}
}

func TestETFHistoricalHTTPRangeIsIndependentOfIncrementalFloorAndRequestIdentity(t *testing.T) {
	db := testDB(t)
	svc := &ETFService{Store: NewETFStore(db), DefaultStart: "invalid-unused-floor", ChunkDays: 31, Now: func() time.Time { return at("20250115") }}
	api := etfAPI(t, svc)
	var first, same, different struct {
		Batch        ETFBatch `json:"batch"`
		Deduplicated bool     `json:"deduplicated"`
	}
	body := `{"codes":["518880.SH","510880.SH"],"mode":"historical","startDate":"20250102","endDate":"20250103"}`
	etfHTTP(t, "POST", api, body, 202, &first)
	etfHTTP(t, "POST", api, `{"codes":["510880.SH","518880.SH","510880.SH"],"mode":"historical","startDate":"20250102","endDate":"20250103"}`, 200, &same)
	etfHTTP(t, "POST", api, `{"codes":["510880.SH","518880.SH"],"mode":"historical","startDate":"20250101","endDate":"20250103"}`, 202, &different)
	if first.Batch.ID != same.Batch.ID || !same.Deduplicated || different.Batch.ID == first.Batch.ID {
		t.Fatalf("historical scopes conflated: %+v %+v %+v", first, same, different)
	}
	var listed struct {
		Batches []ETFBatch `json:"batches"`
	}
	etfHTTP(t, "GET", api, "", 200, &listed)
	if len(listed.Batches) != 2 {
		t.Fatalf("unexpected accepted scopes: %+v", listed)
	}
	for _, batch := range listed.Batches {
		if batch.Mode != "historical" || batch.StartDate == "" || batch.EndDate != "20250103" {
			t.Fatalf("list omitted fixed range: %+v", batch)
		}
	}
}

func TestETFHistoricalHTTPRejectsInvalidOrReplacementRanges(t *testing.T) {
	db := testDB(t)
	svc := &ETFService{Store: NewETFStore(db), DefaultStart: "20250101", Now: func() time.Time { return at("20250115") }}
	api := etfAPI(t, svc)
	for _, body := range []string{
		`{"codes":["510880.SH"],"mode":"historical"}`,
		`{"codes":["510880.SH"],"mode":"historical","startDate":"20250230","endDate":"20250301"}`,
		`{"codes":["510880.SH"],"mode":"historical","startDate":"20250104","endDate":"20250103"}`,
		`{"codes":["510880.SH"],"mode":"historical","startDate":"20250101","endDate":"20250116"}`,
		`{"codes":["510880.SH"],"mode":"historical","startDate":"00000101","endDate":"20250101"}`,
		`{"codes":["510880.SH"],"startDate":"20250101","endDate":"20250103"}`,
		`{"codes":["510880.SH"],"mode":"other"}`,
		`{"codes":["510880.SH"],"mode":"historical","startDate":"20250101","endDate":"20250103","sourceURL":"https://example.invalid"}`,
	} {
		etfHTTP(t, "POST", api, body, 400, nil)
	}
	var listed struct {
		Batches []ETFBatch `json:"batches"`
	}
	etfHTTP(t, "GET", api, "", 200, &listed)
	if len(listed.Batches) != 0 {
		t.Fatal("invalid requests accepted", listed)
	}
	var accepted struct {
		Batch ETFBatch `json:"batch"`
	}
	etfHTTP(t, "POST", api, `{"codes":["510880.SH"],"mode":"historical","startDate":"20250101","endDate":"20250103"}`, 202, &accepted)
	etfHTTP(t, "POST", api+"/"+accepted.Batch.ID+"/retry", `{"startDate":"20240101"}`, 400, nil)
	etfHTTP(t, "POST", api+"/"+accepted.Batch.ID+"/cancel", `{"endDate":"20250115"}`, 400, nil)
}

func TestETFHistoricalHTTPFailedFactorRetryRetainsOriginalRangeAndCompletedSteps(t *testing.T) {
	db := testDB(t)
	source := &etfFixture{failed: map[string]bool{"518880.SH": true}, calls: map[string]int{}}
	svc := &ETFService{Store: NewETFStore(db), Source: source, DefaultStart: "20200101", ChunkDays: 1, Now: func() time.Time { return at("20250115") }}
	api := etfAPI(t, svc)
	var accepted, child, repeated struct {
		Batch ETFBatch `json:"batch"`
	}
	etfHTTP(t, "POST", api, `{"codes":["510880.SH","518880.SH"],"mode":"historical","startDate":"20250102","endDate":"20250103"}`, 202, &accepted)
	serveETFForTest(t, svc)
	original := waitETFHTTP(t, api, accepted.Batch.ID, "partial")
	source.mu.Lock()
	source.failed = map[string]bool{}
	source.mu.Unlock()
	svc.Now = func() time.Time { return at("20250215") }
	etfHTTP(t, "POST", api+"/"+original.ID+"/retry", "", 202, &child)
	etfHTTP(t, "POST", api+"/"+original.ID+"/retry", "", 200, &repeated)
	if child.Batch.ID != repeated.Batch.ID || child.Batch.ParentID != original.ID || child.Batch.Mode != "historical" || child.Batch.StartDate != "20250102" || child.Batch.EndDate != "20250103" || child.Batch.Total != 1 {
		t.Fatalf("retry changed original scope: %+v", child)
	}
	final := waitETFHTTP(t, api, child.Batch.ID, "succeeded")
	item := final.Items[0]
	if item.Code != "518880.SH" || item.DailyRows != 2 || item.AdjRows != 2 || item.DailyStart != "20250102" || item.AdjStart != "20250102" || item.ParentRun != original.Items[1].ID || item.CompletedSegments != 4 {
		t.Fatalf("retry did not preserve completed work: %+v", item)
	}
}

func TestETFHistoricalHTTPAcceptedScopesSurviveMigrationReplay(t *testing.T) {
	db := testDB(t)
	svc := &ETFService{Store: NewETFStore(db), DefaultStart: "20240101", Now: func() time.Time { return at("20250115") }}
	api := etfAPI(t, svc)
	var historical, incremental struct {
		Batch ETFBatch `json:"batch"`
	}
	etfHTTP(t, "POST", api, `{"codes":["510880.SH"],"mode":"historical","startDate":"20250102","endDate":"20250103"}`, 202, &historical)
	etfHTTP(t, "POST", api, `{"codes":["510880.SH"]}`, 202, &incremental)
	// Fault injection: DDL completed but its version record was not committed.
	_, err := db.Exec("DELETE FROM schema_migration WHERE version=10")
	check(t, err)
	check(t, schema.Ensure(context.Background(), db))
	for _, original := range []ETFBatch{historical.Batch, incremental.Batch} {
		var reread struct {
			Batch ETFBatch `json:"batch"`
		}
		etfHTTP(t, "GET", api+"/"+original.ID, "", 200, &reread)
		if reread.Batch.Mode != original.Mode || reread.Batch.StartDate != original.StartDate || reread.Batch.EndDate != original.EndDate || reread.Batch.State != "queued" || reread.Batch.Items[0].ID != original.Items[0].ID {
			t.Fatalf("migration replay altered accepted work: %+v", reread)
		}
	}
}
