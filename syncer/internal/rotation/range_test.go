package rotation

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRangeHTTPPreservesInitialCostAndContinuousHoldings(t *testing.T) {
	db := dailyDatabase(t)
	publication := `{"version":"known-nav-fixture","start":"20240102","end":"20240105","costBps":10,"codes":["510880.SH","518880.SH","159915.SZ","513100.SH"],"names":["红利 ETF","黄金 ETF","创业板 ETF","纳指 ETF"],"days":[
 {"date":"20240102","nav":0.999,"holding":"510880.SH","weight":0.7,"cashWeight":0.3,"cost":0.001,"turnover":1,"benchmarks":[10,20,40,80]},
 {"date":"20240103","nav":1.1,"holding":"510880.SH","weight":0.7,"cashWeight":0.3,"cost":0,"turnover":0,"benchmarks":[11,20,32,80]},
 {"date":"20240104","nav":0.88,"holding":"510880.SH","weight":0.6,"cashWeight":0.4,"cost":0,"turnover":0,"benchmarks":[9.9,22,40,null]},
 {"date":"20240105","nav":1.21,"holding":"510880.SH","weight":0.6,"cashWeight":0.4,"cost":0,"turnover":0,"benchmarks":[12.1,24.2,44,88]}]}`
	if _, err := db.Exec("UPDATE rotation_result SET payload=?,status='ready' WHERE id=1", publication); err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db, Now: func() time.Time { return day("20250103") }}
	server := httptest.NewServer(svc.RangeHandler())
	defer server.Close()
	read := func(query string) struct {
		Range struct {
			Start, End  string
			Returns, DD []*float64
			Comparisons [][]*float64
			Gain, MaxDD *float64
		} `json:"range"`
		Result struct {
			Days []struct{ Weight, Cost float64 }
		} `json:"result"`
	} {
		t.Helper()
		var v struct {
			Range struct {
				Start, End  string
				Returns, DD []*float64
				Comparisons [][]*float64
				Gain, MaxDD *float64
			} `json:"range"`
			Result struct {
				Days []struct{ Weight, Cost float64 }
			} `json:"result"`
		}
		res, err := server.Client().Get(server.URL + query)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatal(res.StatusCode)
		}
		if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	closeTo := func(value *float64, expected float64) {
		t.Helper()
		if value == nil || math.Abs(*value-expected) > 1e-10 {
			t.Fatalf("got %v want %.5f", value, expected)
		}
	}
	all := read("?start=20240102&end=20240105")
	closeTo(all.Range.Returns[0], -0.001)
	closeTo(all.Range.Gain, 0.21)
	closeTo(all.Range.MaxDD, -0.2)
	slice := read("?start=20240103&end=20240105")
	if slice.Range.Start != "20240103" || slice.Range.End != "20240105" {
		t.Fatal("wrong effective range", slice.Range)
	}
	closeTo(slice.Range.Returns[0], 0)
	closeTo(slice.Range.Gain, 0.1)
	closeTo(slice.Range.MaxDD, -0.2)
	closeTo(slice.Range.Comparisons[0][1], -0.1)
	closeTo(slice.Range.Comparisons[1][2], 0.21)
	closeTo(slice.Range.Comparisons[2][2], 0.375)
	closeTo(slice.Range.Comparisons[3][2], 0.1)
	if slice.Range.Comparisons[3][1] != nil {
		t.Fatal("missing benchmark became zero")
	}
	if slice.Result.Days[2].Weight != 0.6 || slice.Result.Days[0].Cost != 0.001 {
		t.Fatal("range reset model or initial cost")
	}
}

func TestRangeHTTPDefaultYearTenYearLimitAndUnknownValues(t *testing.T) {
	db := dailyDatabase(t)
	publication := `{"version":"known-nav-fixture","start":"20140102","end":"20250102","codes":["510880.SH"],"names":["红利 ETF"],"days":[
 {"date":"20140102","nav":1,"holding":"510880.SH","weight":1,"cashWeight":0,"benchmarks":[1]},
 {"date":"20240102","nav":2,"holding":"510880.SH","weight":0.7,"cashWeight":0.3,"benchmarks":[2]},
 {"date":"20240603","nav":null,"holding":"510880.SH","weight":null,"cashWeight":null,"benchmarks":[null]},
 {"date":"20250102","nav":2.2,"holding":"510880.SH","weight":0.6,"cashWeight":0.4,"benchmarks":[2.4]}]}`
	if _, err := db.Exec("UPDATE rotation_result SET payload=?,status='ready' WHERE id=1", publication); err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db, Now: func() time.Time { return day("20250103") }}
	server := httptest.NewServer(svc.RangeHandler())
	defer server.Close()
	read := func(query string) RangeView {
		t.Helper()
		res, err := server.Client().Get(server.URL + query)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatal(res.StatusCode)
		}
		var v RangeView
		if json.NewDecoder(res.Body).Decode(&v) != nil {
			t.Fatal("invalid response")
		}
		return v
	}
	v := read("")
	if v.Range.Start != "20240102" || v.Range.End != "20250102" || len(v.Range.Returns) != 3 || v.Range.MaxDD != nil || v.Range.DD[2] != nil || v.Range.Comparisons[0][1] != nil || !v.Range.Missing {
		t.Fatalf("default/unknowns: %+v", v.Range)
	}
	if v.Range.Gain == nil || math.Abs(*v.Range.Gain-0.1) > 1e-10 {
		t.Fatal("known endpoint return lost")
	}
	one := read("?start=20250102&end=20250102")
	if one.Range.Gain != nil || one.Range.MaxDD != nil || len(one.Range.Returns) != 1 {
		t.Fatal("single day invents aggregate")
	}
	empty := read("?start=20250103&end=20250103")
	if empty.Range.Start != "" || len(empty.Range.Returns) != 0 || empty.Range.Gain != nil {
		t.Fatal("no overlap substituted another date")
	}
	ten := read("?start=20150102&end=20250102")
	if ten.Range.Start != "20240102" {
		t.Fatal("ten years did not clamp actual coverage")
	}
	for _, q := range []string{"?start=20150101&end=20250102", "?start=20250104&end=20250104", "?start=20240101", "?start=20250102&end=20240102", "?start=bad&end=20250102", "?start=20240102&start=20240103&end=20250102", "?start=%XX&end=20250102"} {
		res, err := server.Client().Get(server.URL + q)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Errorf("invalid query %s: %d", q, res.StatusCode)
		}
	}
}

func TestRangeReadReportsStaleCoverageWithoutFetching(t *testing.T) {
	db := dailyDatabase(t)
	if _, err := db.Exec(`UPDATE rotation_result SET status='ready',payload='{"start":"20250102","end":"20250102","days":[{"date":"20250102","nav":1}],"codes":[],"names":[]}' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO rotation_calendar(cal_date,is_open) VALUES ('20250102',1),('20250106',1)"); err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db, Calendar: failedDailyCalendar{}, Now: func() time.Time { return time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC) }}
	server := httptest.NewServer(svc.RangeHandler())
	defer server.Close()
	res, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var v RangeView
	if json.NewDecoder(res.Body).Decode(&v) != nil || res.StatusCode != 200 || v.Status != "stale" || v.Range.End != "20250102" {
		t.Fatalf("stale publication: %+v", v)
	}
}
