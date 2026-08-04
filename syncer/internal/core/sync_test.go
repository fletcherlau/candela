package core

import (
	"context"
	"errors"
	"testing"
)

// --- 内存 fake ---

type fetchCall struct {
	tsCode, start, end string
}

type fakeQuoteSource struct {
	bars  map[string][]Bar // key: tsCode，返回落在请求区间内的部分
	errs  map[string]error // key: tsCode
	calls []fetchCall
}

func (f *fakeQuoteSource) FetchDaily(ctx context.Context, tsCode, startDate, endDate string) ([]Bar, error) {
	f.calls = append(f.calls, fetchCall{tsCode, startDate, endDate})
	if err := f.errs[tsCode]; err != nil {
		return nil, err
	}
	var out []Bar
	for _, b := range f.bars[tsCode] {
		if b.TradeDate >= startDate && b.TradeDate <= endDate {
			out = append(out, b)
		}
	}
	return out, nil
}

type upsertCall struct {
	n int
}

type fakeStore struct {
	instruments []Instrument
	latest      map[string]string // tsCode -> 最新交易日
	rows        map[string]Bar    // tsCode|tradeDate -> Bar，验证幂等
	upserts     []upsertCall
}

func newFakeStore() *fakeStore {
	return &fakeStore{latest: map[string]string{}, rows: map[string]Bar{}}
}

func (f *fakeStore) ListSyncEnabled(ctx context.Context) ([]Instrument, error) {
	return f.instruments, nil
}

func (f *fakeStore) LatestDailyDate(ctx context.Context, tsCode string) (string, error) {
	return f.latest[tsCode], nil
}

func (f *fakeStore) UpsertDaily(ctx context.Context, bars []Bar) (int, error) {
	f.upserts = append(f.upserts, upsertCall{len(bars)})
	for _, b := range bars {
		key := b.TsCode + "|" + b.TradeDate
		f.rows[key] = b
		if b.TradeDate > f.latest[b.TsCode] {
			f.latest[b.TsCode] = b.TradeDate
		}
	}
	return len(bars), nil
}

// --- 测试辅助 ---

func bar(tsCode, date string) Bar {
	return Bar{TsCode: tsCode, TradeDate: date, Close: 1.0}
}

func fixedToday(date string) func() string { return func() string { return date } }

// --- 行为覆盖 ---

func TestIncrementalStartFromLatestPlusOne(t *testing.T) {
	src := &fakeQuoteSource{bars: map[string][]Bar{"510300.SH": {bar("510300.SH", "20240111")}}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latest["510300.SH"] = "20240110"

	s := NewSyncer(src, st, 370, "20100101", nil, fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if len(src.calls) != 1 {
		t.Fatalf("expect 1 fetch, got %d", len(src.calls))
	}
	if src.calls[0].start != "20240111" || src.calls[0].end != "20240120" {
		t.Fatalf("expect range [20240111,20240120], got [%s,%s]", src.calls[0].start, src.calls[0].end)
	}
	if sum.Success != 1 || sum.Results[0].Fetched != 1 || sum.Results[0].Message != "ok" {
		t.Fatalf("unexpected summary: %+v", sum)
	}
}

func TestFullBackfillWhenNoHistory(t *testing.T) {
	src := &fakeQuoteSource{bars: map[string][]Bar{"510300.SH": {bar("510300.SH", "20100101")}}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}

	s := NewSyncer(src, st, 370, "20100101", nil, fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if src.calls[0].start != "20100101" {
		t.Fatalf("expect backfill from default start 20100101, got %s", src.calls[0].start)
	}
	if sum.Results[0].StartDate != "20100101" {
		t.Fatalf("result startDate wrong: %+v", sum.Results[0])
	}
}

func TestChunkingSplitsLongRange(t *testing.T) {
	src := &fakeQuoteSource{bars: map[string][]Bar{}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latest["510300.SH"] = "20240101"

	s := NewSyncer(src, st, 10, "20100101", nil, fixedToday("20240125"))
	s.Run(context.Background(), nil)

	// [20240102, 20240125] 共 24 天，每片 10 天 → 3 片
	want := []fetchCall{
		{"510300.SH", "20240102", "20240111"},
		{"510300.SH", "20240112", "20240121"},
		{"510300.SH", "20240122", "20240125"},
	}
	if len(src.calls) != len(want) {
		t.Fatalf("expect %d chunks, got %d: %+v", len(want), len(src.calls), src.calls)
	}
	for i, w := range want {
		if src.calls[i] != w {
			t.Fatalf("chunk %d: expect %+v, got %+v", i, w, src.calls[i])
		}
	}
}

func TestShortCircuitWhenUpToDate(t *testing.T) {
	src := &fakeQuoteSource{bars: map[string][]Bar{}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latest["510300.SH"] = "20240120"

	s := NewSyncer(src, st, 370, "20100101", nil, fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if len(src.calls) != 0 {
		t.Fatalf("expect no fetch when up to date, got %d", len(src.calls))
	}
	if sum.Results[0].Message != "已是最新" {
		t.Fatalf("expect 已是最新, got %q", sum.Results[0].Message)
	}
}

func TestIdempotentRerun(t *testing.T) {
	bars := []Bar{bar("510300.SH", "20240111"), bar("510300.SH", "20240112")}
	src := &fakeQuoteSource{bars: map[string][]Bar{"510300.SH": bars}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}

	s := NewSyncer(src, st, 370, "20100101", nil, fixedToday("20240112"))
	first := s.Run(context.Background(), nil)
	rowsAfterFirst := len(st.rows)

	second := s.Run(context.Background(), nil)

	if len(st.rows) != rowsAfterFirst {
		t.Fatalf("rerun changed row count: %d -> %d", rowsAfterFirst, len(st.rows))
	}
	if second.Results[0].Message != "已是最新" {
		t.Fatalf("second run should short-circuit, got %q", second.Results[0].Message)
	}
	if first.Results[0].Fetched != 2 || first.Results[0].Upserted != 2 {
		t.Fatalf("first run counts wrong: %+v", first.Results[0])
	}
}

func TestSingleFailureDoesNotAbortOthers(t *testing.T) {
	src := &fakeQuoteSource{
		bars: map[string][]Bar{"510500.SH": {bar("510500.SH", "20240111")}},
		errs: map[string]error{"BAD.SH": errors.New("tushare: 未知标的")},
	}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "BAD.SH"}, {TsCode: "510500.SH"}}

	s := NewSyncer(src, st, 370, "20100101", nil, fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if sum.Total != 2 || sum.Success != 1 {
		t.Fatalf("expect total=2 success=1, got %+v", sum)
	}
	if sum.Results[0].Message == "ok" || sum.Results[1].Message != "ok" {
		t.Fatalf("bad instrument should fail with message, good one ok: %+v", sum.Results)
	}
}

func TestThrottleBeforeEveryFetch(t *testing.T) {
	src := &fakeQuoteSource{bars: map[string][]Bar{}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latest["510300.SH"] = "20240101"

	var waits int
	wait := func(ctx context.Context) error { waits++; return nil }

	s := NewSyncer(src, st, 10, "20100101", wait, fixedToday("20240125"))
	s.Run(context.Background(), nil)

	if waits != len(src.calls) || waits != 3 {
		t.Fatalf("expect one wait per fetch (3), got waits=%d fetches=%d", waits, len(src.calls))
	}
}

func TestSubsetSyncViaTsCodes(t *testing.T) {
	src := &fakeQuoteSource{bars: map[string][]Bar{}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "A"}, {TsCode: "B"}}

	s := NewSyncer(src, st, 370, "20100101", nil, fixedToday("20240120"))
	sum := s.Run(context.Background(), []string{"B"})

	if sum.Total != 1 || sum.Results[0].TsCode != "B" {
		t.Fatalf("expect only B synced, got %+v", sum)
	}
	if src.calls[0].tsCode != "B" {
		t.Fatalf("fetch should target B, got %s", src.calls[0].tsCode)
	}
}

func TestResultRangeAndCounts(t *testing.T) {
	src := &fakeQuoteSource{bars: map[string][]Bar{"510300.SH": {
		bar("510300.SH", "20240111"), bar("510300.SH", "20240112"), bar("510300.SH", "20240113"),
	}}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latest["510300.SH"] = "20240110"

	s := NewSyncer(src, st, 370, "20100101", nil, fixedToday("20240120"))
	res := s.Run(context.Background(), nil).Results[0]

	if res.StartDate != "20240111" || res.EndDate != "20240120" {
		t.Fatalf("result range wrong: %+v", res)
	}
	if res.Fetched != 3 || res.Upserted != 3 {
		t.Fatalf("counts wrong: %+v", res)
	}
}
