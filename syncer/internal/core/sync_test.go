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
	bars     map[string][]Bar       // key: tsCode，返回落在请求区间内的部分
	factors  map[string][]AdjFactor // key: tsCode
	errs     map[string]error       // key: tsCode
	dailyReq []fetchCall
	adjReq   []fetchCall
}

func (f *fakeQuoteSource) FetchDaily(ctx context.Context, tsCode, startDate, endDate string) ([]Bar, error) {
	f.dailyReq = append(f.dailyReq, fetchCall{tsCode, startDate, endDate})
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

func (f *fakeQuoteSource) FetchAdj(ctx context.Context, tsCode, startDate, endDate string) ([]AdjFactor, error) {
	f.adjReq = append(f.adjReq, fetchCall{tsCode, startDate, endDate})
	if err := f.errs[tsCode]; err != nil {
		return nil, err
	}
	var out []AdjFactor
	for _, a := range f.factors[tsCode] {
		if a.TradeDate >= startDate && a.TradeDate <= endDate {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeStore struct {
	instruments []Instrument
	latestDaily map[string]string
	latestAdj   map[string]string
	dailyRows   map[string]Bar       // tsCode|tradeDate -> Bar，验证幂等
	adjRows     map[string]AdjFactor // 同上
	recentDaily map[string][]DailyBarAdj    // key: tsCode，升序，供轮动信号读取
	snapshots   map[string]IntradaySnapshot // tsCode|tradeDate -> IntradaySnapshot，验证幂等
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		latestDaily: map[string]string{},
		latestAdj:   map[string]string{},
		dailyRows:   map[string]Bar{},
		adjRows:     map[string]AdjFactor{},
		recentDaily: map[string][]DailyBarAdj{},
		snapshots:   map[string]IntradaySnapshot{},
	}
}

func (f *fakeStore) ListSyncEnabled(ctx context.Context) ([]Instrument, error) {
	return f.instruments, nil
}

func (f *fakeStore) Statuses(ctx context.Context) ([]InstrumentStatus, error) {
	return nil, nil
}

func (f *fakeStore) LatestDailyDate(ctx context.Context, tsCode string) (string, error) {
	return f.latestDaily[tsCode], nil
}

func (f *fakeStore) LatestAdjDate(ctx context.Context, tsCode string) (string, error) {
	return f.latestAdj[tsCode], nil
}

func (f *fakeStore) UpsertDaily(ctx context.Context, bars []Bar) (int, error) {
	for _, b := range bars {
		f.dailyRows[b.TsCode+"|"+b.TradeDate] = b
		if b.TradeDate > f.latestDaily[b.TsCode] {
			f.latestDaily[b.TsCode] = b.TradeDate
		}
	}
	return len(bars), nil
}

func (f *fakeStore) UpsertAdjFactors(ctx context.Context, factors []AdjFactor) (int, error) {
	for _, a := range factors {
		f.adjRows[a.TsCode+"|"+a.TradeDate] = a
		if a.TradeDate > f.latestAdj[a.TsCode] {
			f.latestAdj[a.TsCode] = a.TradeDate
		}
	}
	return len(factors), nil
}

func (f *fakeStore) RecentDaily(ctx context.Context, tsCode string, limit int) ([]DailyBarAdj, error) {
	rows := f.recentDaily[tsCode]
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	return rows, nil
}

func (f *fakeStore) UpsertIntradaySnapshots(ctx context.Context, snaps []IntradaySnapshot) (int, error) {
	for _, s := range snaps {
		f.snapshots[s.TsCode+"|"+s.TradeDate] = s
	}
	return len(snaps), nil
}

// --- 测试辅助 ---

func bar(tsCode, date string) Bar {
	return Bar{TsCode: tsCode, TradeDate: date, Close: 1.0}
}

func adj(tsCode, date string) AdjFactor {
	return AdjFactor{TsCode: tsCode, TradeDate: date, AdjFactor: 1.5}
}

func fixedToday(date string) func() string { return func() string { return date } }

// --- 行为覆盖 ---

func TestIncrementalStartFromLatestPlusOne(t *testing.T) {
	src := &fakeQuoteSource{bars: map[string][]Bar{"510300.SH": {bar("510300.SH", "20240111")}}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latestDaily["510300.SH"] = "20240110"
	st.latestAdj["510300.SH"] = "20240110"

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if len(src.dailyReq) != 1 || len(src.adjReq) != 1 {
		t.Fatalf("expect 1 daily + 1 adj fetch, got %d/%d", len(src.dailyReq), len(src.adjReq))
	}
	if src.dailyReq[0].start != "20240111" || src.dailyReq[0].end != "20240120" {
		t.Fatalf("expect range [20240111,20240120], got [%s,%s]", src.dailyReq[0].start, src.dailyReq[0].end)
	}
	if src.adjReq[0].start != "20240111" {
		t.Fatalf("adj should use same range, got %s", src.adjReq[0].start)
	}
	if sum.Success != 1 || sum.Results[0].Fetched != 1 || sum.Results[0].Message != "ok" {
		t.Fatalf("unexpected summary: %+v", sum)
	}
}

func TestStartTakesOlderOfDailyAndAdj(t *testing.T) {
	src := &fakeQuoteSource{}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latestDaily["510300.SH"] = "20240110"
	st.latestAdj["510300.SH"] = "20240105"

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if got := src.dailyReq[0].start; got != "20240106" {
		t.Fatalf("expect start from older (adj) date +1 = 20240106, got %s", got)
	}
	if sum.Results[0].StartDate != "20240106" {
		t.Fatalf("result startDate wrong: %+v", sum.Results[0])
	}
}

func TestBackfillWhenAdjMissing(t *testing.T) {
	src := &fakeQuoteSource{}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latestDaily["510300.SH"] = "20240110" // 日线有历史，因子无 → 全量回填

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	s.Run(context.Background(), nil)

	if got := src.dailyReq[0].start; got != "20100101" {
		t.Fatalf("expect backfill from default start, got %s", got)
	}
	if got := src.adjReq[0].start; got != "20100101" {
		t.Fatalf("adj should also backfill from default start, got %s", got)
	}
}

func TestFullBackfillWhenNoHistory(t *testing.T) {
	src := &fakeQuoteSource{bars: map[string][]Bar{"510300.SH": {bar("510300.SH", "20100101")}}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if src.dailyReq[0].start != "20100101" {
		t.Fatalf("expect backfill from default start 20100101, got %s", src.dailyReq[0].start)
	}
	if sum.Results[0].StartDate != "20100101" {
		t.Fatalf("result startDate wrong: %+v", sum.Results[0])
	}
}

func TestChunkingSplitsLongRange(t *testing.T) {
	src := &fakeQuoteSource{}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latestDaily["510300.SH"] = "20240101"
	st.latestAdj["510300.SH"] = "20240101"

	s := NewSyncer(src, st, 10, "20100101", fixedToday("20240125"))
	s.Run(context.Background(), nil)

	// [20240102, 20240125] 共 24 天，每片 10 天 → 3 片
	want := []fetchCall{
		{"510300.SH", "20240102", "20240111"},
		{"510300.SH", "20240112", "20240121"},
		{"510300.SH", "20240122", "20240125"},
	}
	if len(src.dailyReq) != len(want) || len(src.adjReq) != len(want) {
		t.Fatalf("expect %d chunks per api, got daily=%d adj=%d", len(want), len(src.dailyReq), len(src.adjReq))
	}
	for i, w := range want {
		if src.dailyReq[i] != w || src.adjReq[i] != w {
			t.Fatalf("chunk %d: expect %+v, got daily=%+v adj=%+v", i, w, src.dailyReq[i], src.adjReq[i])
		}
	}
}

func TestShortCircuitWhenUpToDate(t *testing.T) {
	src := &fakeQuoteSource{}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latestDaily["510300.SH"] = "20240120"
	st.latestAdj["510300.SH"] = "20240120"

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if len(src.dailyReq) != 0 || len(src.adjReq) != 0 {
		t.Fatalf("expect no fetch when up to date, got %d/%d", len(src.dailyReq), len(src.adjReq))
	}
	if sum.Results[0].Message != "已是最新" {
		t.Fatalf("expect 已是最新, got %q", sum.Results[0].Message)
	}
}

func TestIdempotentRerun(t *testing.T) {
	src := &fakeQuoteSource{
		bars:    map[string][]Bar{"510300.SH": {bar("510300.SH", "20240111"), bar("510300.SH", "20240112")}},
		factors: map[string][]AdjFactor{"510300.SH": {adj("510300.SH", "20240111"), adj("510300.SH", "20240112")}},
	}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240112"))
	first := s.Run(context.Background(), nil)
	dailyAfterFirst, adjAfterFirst := len(st.dailyRows), len(st.adjRows)

	second := s.Run(context.Background(), nil)

	if len(st.dailyRows) != dailyAfterFirst || len(st.adjRows) != adjAfterFirst {
		t.Fatalf("rerun changed rows: daily %d->%d adj %d->%d",
			dailyAfterFirst, len(st.dailyRows), adjAfterFirst, len(st.adjRows))
	}
	if second.Results[0].Message != "已是最新" {
		t.Fatalf("second run should short-circuit, got %q", second.Results[0].Message)
	}
	// 2 条日线 + 2 条因子
	if first.Results[0].Fetched != 4 || first.Results[0].Upserted != 4 {
		t.Fatalf("first run counts wrong: %+v", first.Results[0])
	}
	if first.Results[0].DailyFetched != 2 || first.Results[0].AdjFetched != 2 {
		t.Fatalf("first run breakdown wrong: %+v", first.Results[0])
	}
}

func TestBackfillWhenDailyMissing(t *testing.T) {
	src := &fakeQuoteSource{}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latestAdj["510300.SH"] = "20240110" // 因子有历史，日线无 → 全量回填

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	s.Run(context.Background(), nil)

	if got := src.dailyReq[0].start; got != "20100101" {
		t.Fatalf("expect backfill from default start, got %s", got)
	}
}

func TestOneSidedGarbageDateFailsLoudly(t *testing.T) {
	src := &fakeQuoteSource{}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latestDaily["510300.SH"] = "20240110"
	st.latestAdj["510300.SH"] = "garbage" // 单边脏值：不能被字符串比较静默掩盖

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if sum.Success != 0 {
		t.Fatalf("expect failure on one-sided garbage date, got %+v", sum)
	}
	if len(src.dailyReq) != 0 {
		t.Fatalf("expect no fetch, got %d", len(src.dailyReq))
	}
}

func TestSingleFailureDoesNotAbortOthers(t *testing.T) {
	src := &fakeQuoteSource{
		bars: map[string][]Bar{"510500.SH": {bar("510500.SH", "20240111")}},
		errs: map[string]error{"BAD.SH": errors.New("tushare: 未知标的")},
	}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "BAD.SH"}, {TsCode: "510500.SH"}}

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if sum.Total != 2 || sum.Success != 1 {
		t.Fatalf("expect total=2 success=1, got %+v", sum)
	}
	if sum.Results[0].Message == "ok" || sum.Results[1].Message != "ok" {
		t.Fatalf("bad instrument should fail with message, good one ok: %+v", sum.Results)
	}
}

func TestSubsetSyncViaTsCodes(t *testing.T) {
	src := &fakeQuoteSource{}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "A"}, {TsCode: "B"}}

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	sum := s.Run(context.Background(), []string{"B"})

	if sum.Total != 1 || sum.Results[0].TsCode != "B" {
		t.Fatalf("expect only B synced, got %+v", sum)
	}
	if src.dailyReq[0].tsCode != "B" {
		t.Fatalf("fetch should target B, got %s", src.dailyReq[0].tsCode)
	}
}

func TestResultRangeAndCounts(t *testing.T) {
	src := &fakeQuoteSource{bars: map[string][]Bar{"510300.SH": {
		bar("510300.SH", "20240111"), bar("510300.SH", "20240112"), bar("510300.SH", "20240113"),
	}}}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latestDaily["510300.SH"] = "20240110"
	st.latestAdj["510300.SH"] = "20240110"

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	res := s.Run(context.Background(), nil).Results[0]

	if res.StartDate != "20240111" || res.EndDate != "20240120" {
		t.Fatalf("result range wrong: %+v", res)
	}
	if res.Fetched != 3 || res.Upserted != 3 {
		t.Fatalf("counts wrong: %+v", res)
	}
}

func TestInvalidStoredDateFailsLoudly(t *testing.T) {
	src := &fakeQuoteSource{}
	st := newFakeStore()
	st.instruments = []Instrument{{TsCode: "510300.SH"}}
	st.latestDaily["510300.SH"] = "garbage"
	st.latestAdj["510300.SH"] = "garbage"

	s := NewSyncer(src, st, 370, "20100101", fixedToday("20240120"))
	sum := s.Run(context.Background(), nil)

	if sum.Success != 0 {
		t.Fatalf("expect failure, got %+v", sum)
	}
	if len(src.dailyReq) != 0 {
		t.Fatalf("expect no fetch on invalid date, got %d", len(src.dailyReq))
	}
	if got := sum.Results[0].Message; got == "ok" || got == "" {
		t.Fatalf("expect explicit error message, got %q", got)
	}
}
