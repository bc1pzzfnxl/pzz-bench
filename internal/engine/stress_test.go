package engine

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

func TestStress100(t *testing.T) {
	// 2000 daily candles, enough for the 200-week macro strategy.
	candles := make([]Candle, 0, 2000)
	t0 := int64(0)
	for i := 0; i < 2000; i++ {
		price := 100.0 + 50.0*math.Sin(float64(i)/30.0) + float64(i)*0.05
		if price < 1 {
			price = 1
		}
		candles = append(candles, Candle{
			OpenTime:  t0 + int64(i)*86400000,
			CloseTime: t0 + int64(i)*86400000 + 86399999,
			Open:      price, High: price * 1.01, Low: price * 0.99, Close: price * 1.001,
			Volume: 1000,
		})
	}

	strats := StrategyIDs()
	baselines := []string{BaselinePureDCA, BaselineLumpSum}
	intervals := []string{InjDaily, InjWeekly, InjBiweekly, InjMonthly}
	fees := []float64{0, 0.05, 0.1, 0.25}
	slips := []float64{0, 0.05, 0.1}
	capitals := []float64{100, 1000, 5000}
	dcas := []float64{0, 10, 50, 100}

	rng := rand.New(rand.NewSource(42))
	failures := 0
	run := 0
	for run < 100 {
		for _, sid := range strats {
			if run >= 100 {
				break
			}
			desc, _ := Description(sid)
			params := Params{}
			for _, spec := range desc.Specs {
				params[spec.Name] = spec.Default
			}
			cfg := EngineConfig{
				Symbol:            "BTCUSDT",
				Timeframe:         "1d",
				StartDate:         candles[250].Time(), // warmup room
				EndDate:           candles[len(candles)-1].Time().Add(24 * time.Hour),
				InitialCapital:    capitals[rng.Intn(len(capitals))],
				PeriodicInjection: dcas[rng.Intn(len(dcas))],
				InjectionInterval: intervals[rng.Intn(len(intervals))],
				BaselineType:      baselines[rng.Intn(len(baselines))],
				FeeRate:           fees[rng.Intn(len(fees))] / 100,
				SlippageRate:      slips[rng.Intn(len(slips))] / 100,
			}
			strat, err := BuildStrategy(sid, params)
			if err != nil {
				t.Errorf("run %d build strategy: %v", run, err)
				failures++
				run++
				continue
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("run %d PANIC: %v", run, r)
						failures++
					}
				}()
				if res, err := Run(candles, cfg, strat); err != nil {
					t.Errorf("run %d error: %v", run, err)
					failures++
				} else {
					checkRun(t, run, sid, res)
				}
			}()
			run++
		}
	}
	t.Logf("completed %d runs, %d failures", run, failures)
	if failures > 0 {
		t.Fail()
	}
}

// checkRun asserts the structural invariants every simulation must satisfy:
// aligned finite curves, non-negative balances, and trade counters that match
// the recorded trade lists.
func checkRun(t *testing.T, run int, sid string, res *Result) {
	t.Helper()
	if res == nil || res.Report == nil {
		t.Errorf("run %d (%s): nil result/report", run, sid)
		return
	}
	rep := res.Report
	n := len(res.Times)
	if n == 0 {
		t.Errorf("run %d (%s): no simulated candles", run, sid)
		return
	}
	if rep.Candles != n {
		t.Errorf("run %d (%s): report candles = %d, curves = %d", run, sid, rep.Candles, n)
	}
	if res.StrategyID != sid {
		t.Errorf("run %d: strategy id = %q, want %q", run, res.StrategyID, sid)
	}
	if res.StrategyName == "" {
		t.Errorf("run %d (%s): empty strategy name", run, sid)
	}
	if rep.PriceStart <= 0 || rep.PriceEnd <= 0 {
		t.Errorf("run %d (%s): non-positive prices %v → %v", run, sid, rep.PriceStart, rep.PriceEnd)
	}

	curves := map[string][]float64{
		"strat equity": res.StratEquity,
		"base equity":  res.BaseEquity,
		"strat btc":    res.StratBTC,
		"base btc":     res.BaseBTC,
		"injected":     res.Injected,
		"base inj":     res.BaseInjCum,
	}
	for name, curve := range curves {
		if len(curve) != n {
			t.Errorf("run %d (%s): %s length = %d, want %d", run, sid, name, len(curve), n)
			continue
		}
		for i, v := range curve {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Errorf("run %d (%s): %s[%d] = %v", run, sid, name, i, v)
				break
			}
			if v < -1e-9 {
				t.Errorf("run %d (%s): %s[%d] is negative: %v", run, sid, name, i, v)
				break
			}
		}
	}

	if !closeEnough(rep.Strat.FinalEquity, res.StratEquity[n-1]) {
		t.Errorf("run %d (%s): final equity = %v, last curve point = %v",
			run, sid, rep.Strat.FinalEquity, res.StratEquity[n-1])
	}
	if !closeEnough(rep.Base.FinalEquity, res.BaseEquity[n-1]) {
		t.Errorf("run %d (%s): baseline final equity = %v, last curve point = %v",
			run, sid, rep.Base.FinalEquity, res.BaseEquity[n-1])
	}
	if want := finalSats(res.StratBTC); rep.Strat.SatsT != want {
		t.Errorf("run %d (%s): sats = %d, want %d", run, sid, rep.Strat.SatsT, want)
	}
	if want := finalSats(res.BaseBTC); rep.Base.SatsT != want {
		t.Errorf("run %d (%s): baseline sats = %d, want %d", run, sid, rep.Base.SatsT, want)
	}

	for name, v := range map[string]float64{
		"equity alpha": rep.EquityAlpha,
		"sats alpha":   rep.SatsAlpha,
		"strat MDD":    rep.Strat.MDD,
		"base MDD":     rep.Base.MDD,
		"strat TWRR":   rep.Strat.TWRR,
		"strat XIRR":   rep.Strat.XIRR,
	} {
		if math.IsInf(v, 0) {
			t.Errorf("run %d (%s): %s is infinite", run, sid, name)
		}
	}
	if math.IsNaN(rep.EquityAlpha) {
		t.Errorf("run %d (%s): equity alpha is NaN", run, sid)
	}
	for name, mdd := range map[string]float64{"strat": rep.Strat.MDD, "base": rep.Base.MDD} {
		if !math.IsNaN(mdd) && (mdd > 1e-9 || mdd < -1) {
			t.Errorf("run %d (%s): %s MDD out of [-1,0]: %v", run, sid, name, mdd)
		}
	}

	if rep.Strat.TradeCount != len(res.Trades) {
		t.Errorf("run %d (%s): strategy trades = %d, recorded = %d",
			run, sid, rep.Strat.TradeCount, len(res.Trades))
	}
	if rep.Base.TradeCount != len(res.BaseTrades) {
		t.Errorf("run %d (%s): baseline trades = %d, recorded = %d",
			run, sid, rep.Base.TradeCount, len(res.BaseTrades))
	}
	if rep.Strat.BuyCount+rep.Strat.SellCount != rep.Strat.TradeCount {
		t.Errorf("run %d (%s): buy+sell = %d, trade count = %d",
			run, sid, rep.Strat.BuyCount+rep.Strat.SellCount, rep.Strat.TradeCount)
	}
	if rep.Base.BuyCount+rep.Base.SellCount != rep.Base.TradeCount {
		t.Errorf("run %d (%s): baseline buy+sell mismatches trade count", run, sid)
	}
	for _, tr := range res.Trades {
		if tr.Price <= 0 || tr.BTC < 0 || tr.Fiat < 0 || tr.Fee < 0 {
			t.Errorf("run %d (%s): invalid trade %+v", run, sid, tr)
			break
		}
	}
	for _, tr := range res.BaseTrades {
		if tr.Price <= 0 || tr.BTC < 0 || tr.Fiat < 0 || tr.Fee < 0 {
			t.Errorf("run %d (%s): invalid baseline trade %+v", run, sid, tr)
			break
		}
	}
}

func closeEnough(a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return false
	}
	scale := math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
	return math.Abs(a-b) <= 1e-9*scale
}
