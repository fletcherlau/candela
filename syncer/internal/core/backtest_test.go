package core

import (
	"math"
	"testing"
	"time"
)

func rotationFixture(n int) [][]DailyBarAdj {
	out := make([][]DailyBarAdj, 4)
	for j := range out {
		for i := 0; i < n; i++ {
			price := 100.0
			out[j] = append(out[j], DailyBarAdj{TradeDate: time.Date(2010, 1, 1+i, 0, 0, 0, 0, time.UTC).Format("20060102"), Open: price, High: price, Low: price, Close: price})
		}
	}
	return out
}
func near(t *testing.T, a, b float64) {
	t.Helper()
	if math.Abs(a-b) > 1e-10 {
		t.Fatalf("got %.15f want %.15f", a, b)
	}
}
func TestBacktestFlatAndFirstPurchase(t *testing.T) {
	p := rotationFixture(1222)
	r, e := Backtest(p)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Days) != 3 {
		t.Fatal(len(r.Days))
	}
	for _, d := range r.Days {
		near(t, d.NAV, 1/1.001)
		near(t, d.Weight, 1)
		near(t, d.CashWeight, 0)
		if d.Holding != RotationCodes[0] {
			t.Fatal(d.Holding)
		}
	}
	near(t, r.Days[0].Cost, 1-1/1.001)
	near(t, r.Days[1].Cost, 0)
	if _, e = Backtest(rotationFixture(1219)); e == nil {
		t.Fatal("short warmup accepted")
	}
}
func TestBacktestNoFutureReturnsAndWindowIndependence(t *testing.T) {
	p := rotationFixture(1222)
	// On day 1221 a new winner jumps by 20%. The old holding earns zero that day.
	b := &p[1][1220]
	b.Open = 120
	b.High = 120
	b.Low = 120
	b.Close = 120
	b = &p[1][1221]
	b.Open = 132
	b.High = 132
	b.Low = 132
	b.Close = 132
	r, e := Backtest(p)
	if e != nil {
		t.Fatal(e)
	}
	if r.Days[1].Holding != RotationCodes[1] || r.Days[1].NAV >= r.Days[0].NAV {
		t.Fatalf("new holding earned its pre-trade jump: %+v", r.Days)
	}
	shorter := make([][]DailyBarAdj, 4)
	for j := range p {
		shorter[j] = p[j][:1221]
	}
	before, e := Backtest(shorter)
	if e != nil {
		t.Fatal(e)
	}
	near(t, before.Days[1].NAV, r.Days[1].NAV)
	// Known mark-to-market return on the position held overnight, before today's fees.
	wantBefore := r.Days[1].NAV * (1 + r.Days[1].Weight*.1)
	near(t, r.Days[2].NAV+r.Days[2].Cost, wantBefore)
	p[2][123].Low = 0
	if _, e = Backtest(p); e == nil {
		t.Fatal("invalid bar accepted")
	}
}
func TestRotationDecisionAndRebalance(t *testing.T) {
	if rotationTarget([]float64{-.01, -.006, -.02, -.02}, 0) != 0 {
		t.Fatal("old safety valve leaked")
	}
	if rotationTarget([]float64{0, .005, 0, 0}, 0) != 0 {
		t.Fatal("delta boundary")
	}
	if rotationTarget([]float64{0, .00501, 0, 0}, 0) != 1 {
		t.Fatal("delta exceeded")
	}
	if rotationTarget([]float64{1, 1, 0, 0}, 1) != 1 {
		t.Fatal("tie changed holding")
	}
	if needsRotationAdjustment(.7, .7499) || !needsRotationAdjustment(.7, .75) {
		t.Fatal("5pp boundary")
	}
	for _, w := range []float64{.4, .8, 1} {
		a, c, fee, turn := rebalanceCash(30, 70, w, 0, 0)
		near(t, a/(a+c), w)
		near(t, a+c+fee, 100)
		near(t, fee, turn*.001)
		if c < -1e-12 {
			t.Fatal("borrowed cash")
		}
	}
	a, c, fee, turn := rebalanceCash(80, 20, .4, 0, 0)
	near(t, a/(a+c), .4)
	near(t, a+c+fee, 100)
	near(t, turn, 40/(1-.0004))
}

func TestVerifiedSuspensionCannotTradeOrAdvanceIndicators(t *testing.T) {
	p := rotationFixture(1223)
	// The selected holding is suspended. Its prior adjusted valuation is carried,
	// while the other leg jumps and would otherwise trigger a switch.
	p[0][1220] = DailyBarAdj{TradeDate: p[0][1220].TradeDate, Close: 100, Suspended: true}
	p[1][1220].Open = 120
	p[1][1220].High = 120
	p[1][1220].Low = 120
	p[1][1220].Close = 120
	r, e := Backtest(p)
	if e != nil {
		t.Fatal(e)
	}
	if r.Days[1].Holding != RotationCodes[0] || r.Days[1].Cost != 0 || len(r.Days[1].Suspended) != 1 {
		t.Fatal("traded suspended holding")
	}
	near(t, r.Days[1].NAV, r.Days[0].NAV)
	// A non-held suspended leg is excluded, even if its previous momentum led.
	p[0][1220].Suspended = false
	p[0][1220].Open = 100
	p[0][1220].High = 100
	p[0][1220].Low = 100
	p[1][1220] = DailyBarAdj{TradeDate: p[1][1220].TradeDate, Close: 100, Suspended: true}
	r, e = Backtest(p)
	if e != nil {
		t.Fatal(e)
	}
	if r.Days[1].Holding == RotationCodes[1] {
		t.Fatal("bought suspended instrument")
	}
}
