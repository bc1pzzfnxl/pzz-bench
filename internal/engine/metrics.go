package engine

import (
	"math"
	"time"
)

// CashFlow is a money movement at time `Time` (ms). Positive amounts are
// capital contributed by the investor; the final valuation is passed to XIRR
// as one negative flow (value taken back out).
type CashFlow struct {
	Time   int64
	Amount float64
}

// CurveStats is the metrics bundle computed for one portfolio curve
// (active strategy or baseline).
type CurveStats struct {
	FinalEquity float64
	SatsT       int64
	FeesPaid    float64
	TradeCount  int
	BuyCount    int
	SellCount   int
	WinRate     float64 // NaN when not meaningful (buy-only baseline)
	TWRR        float64 // time-weighted annualized return (injection adjusted)
	XIRR        float64 // money-weighted annualized return over cash flows
	MDD         float64 // max drawdown of the injection-adjusted NAV curve
	Sharpe      float64
	Sortino     float64
	Calmar      float64
}

// BenchmarkReport compares the active strategy against its baseline.
type BenchmarkReport struct {
	Strat          CurveStats
	Base           CurveStats
	SatsAlpha      float64 // (strat sats - base sats)/base sats * 100
	EquityAlpha    float64 // (strat equity - base equity)/base equity * 100
	TotalInjectedS float64
	TotalInjectedB float64
	FeePercentS    float64 // fees / injected capital (decimal fraction)
	FeePercentB    float64
	Candles        int
	PriceStart     float64
	PriceEnd       float64
	From, To       time.Time
}

const msPerYear = 365.0 * 24 * 60 * 60 * 1000

// MaxDD returns the minimum peak-to-trough return of a series
// (min over t of (v_t - max_{tau<=t} v_tau) / max_{tau<=t} v_tau).
func MaxDD(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	peak := v[0]
	mdd := 0.0
	for _, x := range v {
		if x > peak {
			peak = x
		}
		if peak != 0 {
			dd := (x - peak) / peak
			if dd < mdd {
				mdd = dd
			}
		}
	}
	return mdd
}

// AdjustedReturns converts an equity curve plus per-candle injected cash into
// per-candle time-weighted returns. Injections are removed before division so
// fresh capital does not count as performance.
// r_0 = E_0 / inj_0 - 1 ; r_i = (E_i - inj_i) / E_{i-1} - 1.
func AdjustedReturns(equity, injDelta []float64) []float64 {
	n := len(equity)
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		if i == 0 {
			if injDelta[0] > 0 {
				out[0] = equity[0]/injDelta[0] - 1
			} else {
				out[0] = 0
			}
			continue
		}
		prev := equity[i-1]
		if prev == 0 {
			out[i] = 0
			continue
		}
		out[i] = (equity[i]-injDelta[i])/prev - 1
	}
	return out
}

// NAVCurve builds the injection-adjusted net asset value series:
// NAV_t = E_t / cumInjected_t (portfolio value per injected dollar, seeded 1.0).
func NAVCurve(equity, injDelta []float64) []float64 {
	out := make([]float64, len(equity))
	var cum float64
	for i := range equity {
		cum += injDelta[i]
		if cum <= 0 {
			out[i] = math.NaN()
			continue
		}
		out[i] = equity[i] / cum
	}
	return out
}

// AnnualizedTWRR chains adjusted returns and annualizes them over the elapsed
// year fraction (uses curve candle timestamps, not flow timestamps).
func AnnualizedTWRR(returns []float64, times []int64) float64 {
	if len(returns) == 0 || len(returns) != len(times) {
		return math.NaN()
	}
	prod := 1.0
	for _, r := range returns {
		prod *= 1 + r
	}
	years := float64(times[len(times)-1]-times[0]) / msPerYear
	if years <= 1.0/365.0 {
		return math.NaN()
	}
	return math.Pow(prod, 1/years) - 1
}

// Sharpe computes mean(r)/sigma(r) * sqrt(periodsPerYear), risk free = 0.
func Sharpe(returns []float64, periodsPerYear float64) float64 {
	n := len(returns)
	if n < 2 {
		return math.NaN()
	}
	var mean float64
	for _, r := range returns {
		mean += r
	}
	mean /= float64(n)
	var variance float64
	for _, r := range returns {
		d := r - mean
		variance += d * d
	}
	variance /= float64(n)
	sd := math.Sqrt(variance)
	if sd == 0 {
		return math.NaN()
	}
	return mean / sd * math.Sqrt(periodsPerYear)
}

// Sortino computes mean(r)/downsideRMS(r) * sqrt(periodsPerYear); the
// downside deviation only penalizes negative candle returns (target = 0).
func Sortino(returns []float64, periodsPerYear float64) float64 {
	n := len(returns)
	if n < 2 {
		return math.NaN()
	}
	var mean float64
	for _, r := range returns {
		mean += r
	}
	mean /= float64(n)
	var sq float64
	for _, r := range returns {
		if r < 0 {
			sq += r * r
		}
	}
	dd := math.Sqrt(sq / float64(n))
	if dd == 0 {
		return math.NaN()
	}
	return mean / dd * math.Sqrt(periodsPerYear)
}

// XIRR solves the money-weighted annualized rate of return: the rate r for
// which sum(cf_i / (1+r)^(dt_i/365)) == 0, positive amounts = contributions.
// Solved by widening bisection; NaN when no sign change exists.
func XIRR(flows []CashFlow) float64 {
	if len(flows) < 2 {
		return math.NaN()
	}
	t0 := flows[0].Time
	npv := func(r float64) float64 {
		s := 0.0
		for _, cf := range flows {
			dt := float64(cf.Time-t0) / msPerYear
			s += cf.Amount / math.Pow(1+r, dt)
		}
		return s
	}
	lo, hi := -0.9999, 8.0
	fLo, fHi := npv(lo), npv(hi)
	for fLo*fHi > 0 && hi < 1e6 {
		hi *= 4
		fHi = npv(hi)
	}
	if fLo*fHi > 0 {
		return math.NaN()
	}
	for i := 0; i < 200; i++ {
		mid := (lo + hi) / 2
		fMid := npv(mid)
		if fMid == 0 {
			return mid
		}
		if fMid*fLo < 0 {
			hi = mid
		} else {
			lo = mid
			fLo = fMid
		}
	}
	return (lo + hi) / 2
}

// averageCostWinRate pairs every SELL against the volume-weighted average
// cost basis of the BTC accumulated before it. A sell is a "win" when its
// effective price beats that cost basis.
func averageCostWinRate(trades []Trade) float64 {
	var coin, cost, sold, proceeds float64
	sells, wins := 0, 0
	for _, t := range trades {
		switch t.Side {
		case ActionBuy:
			coin += t.BTC
			cost += t.Fiat
		case ActionSell:
			if coin <= 0 {
				continue
			}
			basis := cost * (t.BTC / coin)
			if t.Fiat > basis {
				wins++
			}
			sells++
			sold += t.BTC
			proceeds += t.Fiat
			coin -= t.BTC
			cost -= basis
			if coin <= 0 {
				coin, cost = 0, 0
			}
		}
	}
	_ = sold
	_ = proceeds
	if sells == 0 {
		return math.NaN()
	}
	return float64(wins) / float64(sells)
}

// curveStats computes one curve's metrics bundle. `times` are the candle open
// times of the curve window, `injDelta` the per-candle injected cash, `flows`
// the chronological injection cash flows (final valuation appended internally
// for XIRR), `trades` the curve's own executed orders.
func curveStats(times []int64, curve, injDelta []float64, flows []CashFlow, tf string,
	fees float64, trades []Trade, isBaseline bool) CurveStats {

	st := CurveStats{FeesPaid: fees}
	if len(curve) == 0 {
		return st
	}
	st.FinalEquity = curve[len(curve)-1]
	py := CandlesPerYear(tf)
	r := AdjustedReturns(curve, injDelta)
	st.TWRR = AnnualizedTWRR(r, times)
	if len(flows) > 0 {
		flows := append(append([]CashFlow(nil), flows...), CashFlow{Time: times[len(times)-1], Amount: -st.FinalEquity})
		st.XIRR = XIRR(flows)
	}
	st.MDD = MaxDD(NAVCurve(curve, injDelta))
	st.Sharpe = Sharpe(r, py)
	st.Sortino = Sortino(r, py)
	if !math.IsNaN(st.TWRR) && st.MDD < 0 {
		st.Calmar = st.TWRR / math.Abs(st.MDD)
	} else {
		st.Calmar = math.NaN()
	}
	buys, sells := 0, 0
	for _, t := range trades {
		switch t.Side {
		case ActionBuy:
			buys++
		case ActionSell:
			sells++
		}
	}
	st.BuyCount, st.SellCount, st.TradeCount = buys, sells, buys+sells
	switch {
	case isBaseline:
		st.WinRate = math.NaN() // baseline is 100% buys
	default:
		st.WinRate = averageCostWinRate(trades)
	}
	return st
}

// ComposeReport builds the comparative report from simulator outputs.
func ComposeReport(tf string, times []int64, stratE, baseE []float64,
	stratBTC, baseBTC []float64, injDeltaS, injDeltaB []float64,
	flowsS, flowsB []CashFlow, tradesS, tradesB []Trade,
	prices []float64, feesS, feesB float64, injectedS, injectedB float64,
	candles int) *BenchmarkReport {

	rep := &BenchmarkReport{
		TotalInjectedS: injectedS,
		TotalInjectedB: injectedB,
		Candles:        candles,
	}
	rep.Strat = curveStats(times, stratE, injDeltaS, flowsS, tf, feesS, tradesS, false)
	rep.Base = curveStats(times, baseE, injDeltaB, flowsB, tf, feesB, tradesB, true)
	rep.Strat.SatsT = finalSats(stratBTC)
	rep.Base.SatsT = finalSats(baseBTC)
	if injectedS > 0 {
		rep.FeePercentS = feesS / injectedS
	}
	if injectedB > 0 {
		rep.FeePercentB = feesB / injectedB
	}
	if rep.Base.SatsT > 0 {
		rep.SatsAlpha = (float64(rep.Strat.SatsT-rep.Base.SatsT) / float64(rep.Base.SatsT)) * 100
	} else {
		rep.SatsAlpha = math.NaN()
	}
	if rep.Base.FinalEquity > 0 {
		rep.EquityAlpha = (rep.Strat.FinalEquity - rep.Base.FinalEquity) / rep.Base.FinalEquity * 100
	}
	if len(prices) > 0 {
		rep.PriceStart = prices[0]
		rep.PriceEnd = prices[len(prices)-1]
	}
	if len(times) > 0 {
		rep.From = time.Unix(times[0]/1000, 0).UTC()
		rep.To = time.Unix(times[len(times)-1]/1000, 0).UTC()
	}
	return rep
}

func finalSats(btc []float64) int64 {
	if len(btc) == 0 {
		return 0
	}
	return BTCToSats(btc[len(btc)-1])
}
