package core

import (
	"context"
	"errors"
	"testing"
)

type fakeSWSource struct {
	dict      []SWIndustry
	dictErr   error
	members   map[string][]SWMember // key: l1Code+"|"+isNew
	memberErr error
	daily     map[string][]SWIndexBar // key: tsCode
	dailyErr  error
}

func (f *fakeSWSource) FetchSWDictionary(ctx context.Context, src string) ([]SWIndustry, error) {
	return f.dict, f.dictErr
}

func (f *fakeSWSource) FetchSWMembership(ctx context.Context, l1Code, isNew string) ([]SWMember, error) {
	if f.memberErr != nil {
		return nil, f.memberErr
	}
	return f.members[l1Code+"|"+isNew], nil
}

func (f *fakeSWSource) FetchSWIndexDaily(ctx context.Context, tsCode, startDate, endDate string) ([]SWIndexBar, error) {
	if f.dailyErr != nil {
		return nil, f.dailyErr
	}
	var out []SWIndexBar
	for _, b := range f.daily[tsCode] {
		if b.TradeDate >= startDate && b.TradeDate <= endDate {
			out = append(out, b)
		}
	}
	return out, nil
}

type fakeSWStore struct {
	industries   []SWIndustry
	members      []SWMember
	daily        map[string][]SWIndexBar
	upsertDailyN int
}

func newFakeSWStore() *fakeSWStore {
	return &fakeSWStore{daily: make(map[string][]SWIndexBar)}
}

func (f *fakeSWStore) UpsertSWIndustries(ctx context.Context, items []SWIndustry) (int, error) {
	f.industries = append(f.industries, items...)
	return len(items), nil
}

func (f *fakeSWStore) ListSWIndustries(ctx context.Context, level string) ([]SWIndustry, error) {
	var out []SWIndustry
	for _, it := range f.industries {
		if level == "" || it.Level == level {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeSWStore) CountSWIndustries(ctx context.Context) (int, error) {
	return len(f.industries), nil
}

func (f *fakeSWStore) UpsertSWMembers(ctx context.Context, items []SWMember) (int, error) {
	f.members = append(f.members, items...)
	return len(items), nil
}

func (f *fakeSWStore) CountSWMembers(ctx context.Context) (int, error) {
	return len(f.members), nil
}

func (f *fakeSWStore) LatestSWIndexDailyDate(ctx context.Context, tsCode string) (string, error) {
	latest := ""
	for _, b := range f.daily[tsCode] {
		if b.TradeDate > latest {
			latest = b.TradeDate
		}
	}
	return latest, nil
}

func (f *fakeSWStore) UpsertSWIndexDaily(ctx context.Context, bars []SWIndexBar) (int, error) {
	for _, b := range bars {
		f.daily[b.TsCode] = append(f.daily[b.TsCode], b)
	}
	f.upsertDailyN += len(bars)
	return len(bars), nil
}

func (f *fakeSWStore) SWIndexStatuses(ctx context.Context) ([]SWIndexStatus, error) {
	return nil, nil
}

func TestSWSyncerRunIndustry(t *testing.T) {
	src := &fakeSWSource{
		dict: []SWIndustry{
			{IndexCode: "801010.SI", IndustryName: "农林牧渔", Level: "L1"},
			{IndexCode: "801020.SI", IndustryName: "基础化工", Level: "L1"},
			{IndexCode: "801011.SI", IndustryName: "种植业", Level: "L2"},
		},
		members: map[string][]SWMember{
			"801010.SI|Y": {{TsCode: "000001.SZ", L1Code: "801010.SI", InDate: "20200101", IsNew: "Y"}},
			"801010.SI|N": {{TsCode: "000002.SZ", L1Code: "801010.SI", InDate: "20100101", OutDate: "20190101", IsNew: "N"}},
			"801020.SI|Y": {{TsCode: "000003.SZ", L1Code: "801020.SI", InDate: "20210101", IsNew: "Y"}},
		},
	}
	st := newFakeSWStore()
	s := NewSWSyncer(src, st, 370, "20100101", "", func() string { return "20260318" })

	sum := s.RunIndustry(context.Background())
	if sum.Message != "ok" {
		t.Fatalf("expected ok, got %q", sum.Message)
	}
	if sum.DictFetched != 3 || sum.DictUpserted != 3 {
		t.Fatalf("dict counts wrong: %+v", sum)
	}
	// 只按 L1 拉成分，每个 L1 拉 Y/N 两次；L2 不单独拉。
	if sum.MemberFetched != 3 || sum.MemberUpserted != 3 {
		t.Fatalf("member counts wrong: %+v", sum)
	}
	if len(st.members) != 3 {
		t.Fatalf("store should hold 3 members, got %d", len(st.members))
	}
}

func TestSWSyncerRunIndustryDictError(t *testing.T) {
	src := &fakeSWSource{dictErr: errors.New("boom")}
	st := newFakeSWStore()
	s := NewSWSyncer(src, st, 370, "20100101", "", nil)

	sum := s.RunIndustry(context.Background())
	if sum.Message == "ok" || sum.Message == "" {
		t.Fatalf("expected error message, got %q", sum.Message)
	}
}

func TestSWSyncerRunDailyFullBackfillAndIncremental(t *testing.T) {
	src := &fakeSWSource{
		daily: map[string][]SWIndexBar{
			"801010.SI": {
				{TsCode: "801010.SI", TradeDate: "20260316", Close: 1000},
				{TsCode: "801010.SI", TradeDate: "20260317", Close: 1010},
				{TsCode: "801010.SI", TradeDate: "20260318", Close: 1020},
			},
		},
	}
	st := newFakeSWStore()
	st.industries = []SWIndustry{
		{IndexCode: "801010.SI", IndustryName: "农林牧渔", Level: "L1"},
		{IndexCode: "801020.SI", IndustryName: "基础化工", Level: "L1"},
	}
	s := NewSWSyncer(src, st, 370, "20100101", "", func() string { return "20260318" })

	// 全量回填：无历史时从默认起始日期开始。
	sum := s.RunDaily(context.Background(), nil)
	if sum.Total != 2 || sum.Success != 2 {
		t.Fatalf("expected 2/2 success, got %+v", sum)
	}
	for _, r := range sum.Results {
		if r.StartDate != "20100101" || r.EndDate != "20260318" {
			t.Fatalf("unexpected date range: %+v", r)
		}
	}
	if got := len(st.daily["801010.SI"]); got != 3 {
		t.Fatalf("expected 3 bars stored, got %d", got)
	}

	// 增量：已是最新时不再拉取（today 不变，latest=20260318 → start=20260319 > today）。
	sum = s.RunDaily(context.Background(), []string{"801010.SI"})
	if sum.Total != 1 || sum.Success != 1 {
		t.Fatalf("expected 1/1 success, got %+v", sum)
	}
	r := sum.Results[0]
	if r.Message != "已是最新" {
		t.Fatalf("expected 已是最新, got %q (start=%s)", r.Message, r.StartDate)
	}
	if got := len(st.daily["801010.SI"]); got != 3 {
		t.Fatalf("incremental should not duplicate bars, got %d", got)
	}
}

func TestSWSyncerRunDailyFailureIsolation(t *testing.T) {
	src := &fakeSWSource{
		daily: map[string][]SWIndexBar{
			"801020.SI": {{TsCode: "801020.SI", TradeDate: "20260318", Close: 2000}},
		},
		dailyErr: errors.New("upstream down"),
	}
	st := newFakeSWStore()
	st.industries = []SWIndustry{
		{IndexCode: "801010.SI", Level: "L1"},
	}
	s := NewSWSyncer(src, st, 370, "20100101", "", func() string { return "20260318" })

	sum := s.RunDaily(context.Background(), nil)
	if sum.Success != 0 || sum.Total != 1 {
		t.Fatalf("expected 0/1 success, got %+v", sum)
	}
	if sum.Results[0].Message == "" {
		t.Fatal("failure should be recorded in result message")
	}
}
