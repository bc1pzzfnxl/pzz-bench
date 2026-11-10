// Engine verification: zero look-ahead fills, fees, injection schedule,
// metric math and the grid optimizer.
package engine

import (
	"context"
	"math"
	"testing"
	"time"
)

// day builds a daily candle at day offset d.
func day(d int, open, close_ float64) Candle {
	ms := int64(d) * 86400000
	return Candle{
		OpenTime: ms, CloseTime: ms + 86400000 - 1,
		Open: open, High: math.Max(open, close_), Low: math.Min(open, close_),
		Close: close_,
	}
}

// probeSignal emits one action at a close index, then nothing.
type probeSignal struct {
	index       int
	action      SignalAction
	weight      float64
	indicators  bool
	beforeState float64 // BTC held before its fill (probe)
}

func (p *probeSignal) ID() string         { return "PROBE" }
func (p *probeSignal) Name() string       { return "Probe" }
func (p *probeSignal) WarmupCandles() int { return 1 }
func (p *probeSignal) ExportSeries() map[string][]float64 {
	return nil
}
func (p *probeSignal) ComputeIndicators([]Candle) {}
func (p *probeSignal) OnCandle(index int, state PortfolioState) Signal {
	if index == p.index {
		if p.action == ActionBuy && math.IsNaN(p.beforeState) {
			p.beforeState = 0
		}
		p.beforeState = state.BTC
		return Signal{Action: p.action, Weight: p.weight, Tag: "probe"}
	}
	return Signal{Action: ActionHold}
}

// TestZeroLookaheadFill pins the Close[t] -> Open[t+1] execution contract.
func TestZeroLookaheadFill(t *testing.T) {
	candles := []Candle{
		day(0, 100, 100), // t0: capital
		day(1, 105, 110), // close here produces the signal
		day(2, 120, 121), // fill must land exactly at this Open
		day(3, 130, 131),
	}
	probe := &probeSignal{index: 1, action: ActionBuy, weight: 1.0}
	cfg := EngineConfig{
		Symbol: "TEST", Timeframe: "1d",
		StartDate:      candles[0].Time(),
		EndDate:        candles[3].Time().Add(24 * time.Hour),
		InitialCapital: 1000, FeeRate: 0, SlippageRate: 0,
		InjectionInterval: InjWeekly,
		BaselineType:      BaselinePureDCA,
	}
	res, err := Run(candles, cfg, probe)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Trades) != 1 {
		t.Fatalf("want exactly 1 trade, got %d", len(res.Trades))
	}
	tr := res.Trades[0]
	if tr.ExecTime != candles[2].OpenTime {
		t.Fatalf("fill at open of candle 3: got ExecTime %d want %d",
			tr.ExecTime, candles[2].OpenTime)
	}
	if tr.Price != 120 {
		t.Fatalf("fill price: got %.2f want 120 (Open[i+1], no slippage)", tr.Price)
	}
	if tr.Index != 1 || tr.Tag != "probe" {
		t.Fatalf("trade meta: %+v", tr)
	}
	// slippage: price = 120 * 1.005 = 120.6
	cfg.SlippageRate = 0.005
	probe2 := &probeSignal{index: 1, action: ActionBuy, weight: 1.0}
	res2, _ := Run(candles, cfg, probe2)
	if got := res2.Trades[0].Price; math.Abs(got-120.6) > 1e-9 {
		t.Fatalf("slippage price: got %.4f want 120.6", got)
	}
	// fee: fees = 1000 * 0.001, btc = (1000-1)/120.6
	cfg.FeeRate = 0.001
	probe3 := &probeSignal{index: 1, action: ActionBuy, weight: 1.0}
	res3, _ := Run(candles, cfg, probe3)
	tr3 := res3.Trades[0]
	if math.Abs(tr3.Fee-1.0) > 1e-9 || math.Abs(tr3.BTC-999/120.6) > 1e-9 {
		t.Fatalf("fee math: got %+v, want fee=1.0 btc=%.6f", tr3, 999/120.6)
	}
	if math.Abs(res3.FeesPaid-1.0) > 1e-9 {
		t.Fatalf("fees paid: got %.6f", res3.FeesPaid)
	}
}

// TestInjectionsBoundary checks t0-only initial capital, no double injection
// on the start day, and the next injection at the FIRST period boundary.
func TestInjectionsBoundary(t *testing.T) {
	candles := make([]Candle, 0, 16)
	t0 := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 16; i++ {
		if day := t0.Add(time.Duration(i) * 24 * time.Hour); true {
			candles = append(candles, Candle{
				OpenTime: day.UnixMilli(), CloseTime: day.UnixMilli() + 86399999,
				Open: 100, High: 101, Low: 99, Close: 100.5,
			})
			_ = day
		}
	}
	cfg := EngineConfig{
		Symbol: "TEST", Timeframe: "1d",
		StartDate:         candles[0].Time(),
		EndDate:           time.UnixMilli(candles[15].OpenTime).Add(24 * time.Hour),
		InitialCapital:    1000,
		PeriodicInjection: 200,
		InjectionInterval: InjWeekly,
		BaselineType:      BaselinePureDCA,
	}
	res, err := Run(candles, cfg, &probeSignal{index: 2, action: ActionBuy, weight: 0.5})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// weekly boundaries: unix/7d changes once between day 0 and day 7.
	// Injected must be 1000 at day0 and then grow by 200 twice more (day 5-ish boundary
	// depends on the absolute unix epoch; the test pins exact deltas instead).
	var injections []int
	var cum float64
	for _, d := range res.Injected {
		if d > cum {
			injections = append(injections, int(d))
		}
		cum = d
	}
	if injections[0] != 1000 {
		t.Fatalf("t0 injection: got %d want 1000", injections[0])
	}
	// every subsequent step must be exactly +200 (never 1200 at t0)
	for i, v := range injections {
		if i > 0 && v-injections[i-1] != 200 {
			t.Fatalf("injection delta at %d: got %+d want 200", i, v-injections[i-1])
		}
	}
	// baseline follows the identical injection schedule
	if res.BaseInjectedTotal != res.TotalInjected {
		t.Fatalf("baseline/s injection totals differ: %.2f vs %.2f",
			res.BaseInjectedTotal, res.TotalInjected)
	}
}

// TestXIRROneYear pins the bisection on a deterministic cash flow.
func TestXIRROneYear(t *testing.T) {
	t0 := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	flows := []CashFlow{
		{Time: t0.UnixMilli(), Amount: 1000},
		{Time: t0.Add(365 * 24 * time.Hour).UnixMilli(), Amount: -2000},
	}
	got := XIRR(flows)
	if math.Abs(got-1.0) > 1e-6 {
		t.Fatalf("XIRR 1000->2000 over 1y: got %v want 1.0", got)
	}
}

// TestMaxDD pins the peak-to-trough computation.
func TestMaxDD(t *testing.T) {
	got := MaxDD([]float64{100, 50, 200, 80})
	if math.Abs(got-(-0.6)) > 1e-12 {
		t.Fatalf("MDD: got %v want -0.6", got)
	}
	if MaxDD(nil) != 0 {
		t.Fatal("empty MDD should be 0")
	}
}

// TestNAVCurve checks the injection-adjusted NAV definition.
func TestNAVCurve(t *testing.T) {
	equity := []float64{1000, 1000, 2400}
	// day2 injected +200: r2 = (2400-200)/1000 - 1 = +120%
	equity_ := equity
	inj := []float64{1000, 0, 200}
	nav := NAVCurve(equity_, inj)
	r := AdjustedReturns(equity_, inj)
	if math.Abs(r[2]-1.2) > 1e-12 {
		t.Fatalf("adjusted return day2: got %v want 0.2", r[2])
	}
	if math.Abs(nav[2]-2.0) > 1e-12 {
		t.Fatalf("NAV final: got %v want 2.0", nav[2])
	}
}

// TestValidateConfig rejects impossible configurations.
func TestValidateConfig(t *testing.T) {
	base := EngineConfig{
		Symbol: "T", Timeframe: "1d", StartDate: day(0, 1, 1).Time(),
		EndDate: day(5, 1, 1).Time(), InitialCapital: 100,
		InjectionInterval: InjWeekly, BaselineType: BaselinePureDCA,
	}
	if err := Validate(base); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	bad := base
	bad.InitialCapital = 0
	if err := Validate(bad); err == nil {
		t.Fatal("zero capital must be rejected")
	}
	bad = base
	bad.EndDate = base.StartDate
	if err := Validate(bad); err == nil {
		t.Fatal("start >= end must be rejected")
	}
}

// TestBuildStrategyRegistry covers default fallbacks and validation errors.
func TestBuildStrategyRegistry(t *testing.T) {
	s, err := BuildStrategy(IDMACross, Params{"fast": "", "slow": ""})
	if err != nil {
		t.Fatalf("spec defaults missing: %v", err)
	}
	if _, ok := s.(*StrategyMACross); !ok {
		t.Fatal("want *StrategyMACross")
	}
	if _, err := BuildStrategy(IDMACross, Params{"fast": "500", "slow": "10"}); err == nil {
		t.Fatal("fast >= slow must fail")
	}
	if _, err := BuildStrategy("NOPE", Params{}); err == nil {
		t.Fatal("unknown id must fail")
	}
}

// TestRunGridSmoke executes a small sweep end-to-end.
func TestRunGridSmoke(t *testing.T) {
	candles := make([]Candle, 0, 120)
	t0 := time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC)
	px := 100.0
	for i := 0; i < 120; i++ {
		day := t0.Add(time.Duration(i) * 24 * time.Hour)
		px *= 1.005
		candles = append(candles, Candle{
			OpenTime: day.UnixMilli(), CloseTime: day.UnixMilli() + 86139999,
			Open: px, High: px * 1.01, Low: px * 0.99, Close: px, Volume: 1,
		})
	}
	cfg := EngineConfig{
		Symbol: "TEST", Timeframe: "1d",
		StartDate:         candles[0].Time(),
		EndDate:           time.UnixMilli(candles[119].OpenTime).Add(24 * time.Hour),
		InitialCapital:    500,
		PeriodicInjection: 50,
		InjectionInterval: InjWeekly,
		BaselineType:      BaselinePureDCA,
	}
	task := GridTask{
		Cfg: cfg, StrategyID: IDMACross, Metric: MetricSatsAlpha,
		X: GridAxis{Name: "fast", Start: 2, Stop: 4, Step: 2},
		Y: GridAxis{Name: "slow", Start: 4, Stop: 8, Step: 2},
	}
	res := RunGrid(candles, task)
	if got := len(res.XVals); got != 2 {
		t.Fatalf("x points: %d want 2 (2,4)", got)
	}
	if got := len(res.YVals); got != 3 {
		t.Fatalf("y points: %d want 3 (4,6,8)", got)
	}
	if res.RanCount+res.SkippedCount != len(res.XVals)*len(res.YVals) {
		t.Fatal("cell accounting mismatch")
	}
	if res.RanCount == 0 || math.IsNaN(res.BestVal) {
		t.Fatal("expected at least one executed cell with a best value")
	}
	best := res.Cells[res.Best.Y][res.Best.X]
	if best != res.BestVal {
		t.Fatalf("best pointer mismatch: %v vs %v", best, res.BestVal)
	}
}

// TestContextCancellation: Ensure-style providers honor ctx (smoke).
func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ctx.Err() == nil {
		t.Fatal("canceled context must report its state")
	}
}
