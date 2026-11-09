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
				res, err := Run(candles, cfg, strat)
				if err != nil {
					t.Errorf("run %d error: %v", run, err)
					failures++
					return
				}
				if res == nil || res.Report == nil {
					t.Errorf("run %d nil result/report", run)
					failures++
					return
				}
				if math.IsNaN(res.Report.Strat.XIRR) && res.Report.Strat.XIRR != res.Report.Strat.XIRR {
					// NaN acceptable for some; just ensure no crash
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