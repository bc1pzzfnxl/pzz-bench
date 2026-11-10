package engine

import (
	"fmt"
	"time"
)

// Result is the full output of one simulation: aligned curves, trades,
// indicator series and the derived BenchmarkReport.
type Result struct {
	Cfg          EngineConfig
	StrategyID   string
	StrategyName string

	Times       []int64   // OpenTime (ms) of every simulated candle
	StratEquity []float64 // fiat equity of the strategy at each close
	BaseEquity  []float64 // fiat equity of the baseline at each close
	StratBTC    []float64 // BTC balance of the strategy at each close
	BaseBTC     []float64 // BTC balance of the baseline at each close
	Injected    []float64 // cumulative capital injected into the strategy
	BaseInjCum  []float64 // cumulative capital injected into the baseline

	Trades     []Trade
	BaseTrades []Trade

	TotalInjected     float64
	BaseInjectedTotal float64

	FeesPaid     float64
	BaseFeesPaid float64

	Series map[string][]float64 // strategy indicator series, aligned with candles
	Report *BenchmarkReport
}

// WarmupStart returns the earliest date data must be loaded from so that
// indicator warmup completes before cfg.StartDate.
func WarmupStart(cfg EngineConfig, strat Strategy) time.Time {
	call := time.Duration(int64(strat.WarmupCandles()) * TimeframeMS(cfg.Timeframe))
	return cfg.StartDate.UTC().Add(-call * time.Millisecond)
}

// Validate checks an EngineConfig for impossible values.
func Validate(cfg EngineConfig) error {
	if cfg.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if err := ValidateTimeframe(cfg.Timeframe); err != nil {
		return err
	}
	if err := ValidateInjectionInterval(cfg.InjectionInterval); err != nil {
		return err
	}
	if err := ValidateBaseline(cfg.BaselineType); err != nil {
		return err
	}
	if cfg.FeeRate < 0 || cfg.FeeRate >= 0.5 {
		return fmt.Errorf("fee rate %.4f out of range [0, 0.5)", cfg.FeeRate)
	}
	if cfg.SlippageRate < 0 || cfg.SlippageRate >= 0.5 {
		return fmt.Errorf("slippage rate %.4f out of range [0, 0.5)", cfg.SlippageRate)
	}
	if cfg.InitialCapital <= 0 {
		return fmt.Errorf("initial capital must be positive")
	}
	if cfg.PeriodicInjection < 0 {
		return fmt.Errorf("periodic injection must be >= 0")
	}
	if !cfg.StartDate.Before(cfg.EndDate) {
		return fmt.Errorf("start date (%s) must be before end date (%s)",
			cfg.StartDate.Format("2006-01-02"), cfg.EndDate.Format("2006-01-02"))
	}
	return nil
}

// windowSlice resolves the [start, end] indices of cfg inside candles.
func windowSlice(candles []Candle, cfg EngineConfig) (int, int, error) {
	if len(candles) < 2 {
		return 0, 0, fmt.Errorf("need at least 2 candles, got %d", len(candles))
	}
	startMs := cfg.StartDate.UTC().UnixMilli()
	endMs := cfg.EndDate.UTC().UnixMilli()
	iStart := lowerBound(candles, startMs)
	if iStart == len(candles) {
		return 0, 0, fmt.Errorf("no candles on or after start date %s", cfg.StartDate.Format("2006-01-02"))
	}
	iEnd := upperBound(candles, endMs) - 1 // last candle with OpenTime <= end
	if iEnd <= iStart {
		return 0, 0, fmt.Errorf("window %s..%s too narrow (%d candles)",
			cfg.StartDate.Format("2006-01-02"), cfg.EndDate.Format("2006-01-02"), len(candles))
	}
	return iStart, iEnd, nil
}

// lowerBound returns the first index with OpenTime >= ms (sort.Search).
func lowerBound(candles []Candle, ms int64) int {
	lo, hi := 0, len(candles)
	for lo < hi {
		mid := (lo + hi) / 2
		if candles[mid].OpenTime < ms {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// upperBound returns the first index with OpenTime > ms.
func upperBound(candles []Candle, ms int64) int {
	lo, hi := 0, len(candles)
	for lo < hi {
		mid := (lo + hi) / 2
		if candles[mid].OpenTime <= ms {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// Run executes the zero look-ahead simulation:
//
//	for each candle i:
//	  1. land any periodic cash injection (open of candle i)
//	  2. execute the signal emitted at close[i-1] at Open[i] (+ slippage, fees)
//	  3. record the portfolio equity at Close[i]
//	  4. ask the strategy for its signal at Close[i] (for candle i+1's open)
//
// The baseline (PURE_DCA or LUMP_SUM) runs through the exact same loop with
// buy-on-arrival fills at the open of each injection candle.
func Run(candles []Candle, cfg EngineConfig, strat Strategy) (*Result, error) {
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	if strat == nil {
		return nil, fmt.Errorf("strategy is nil")
	}
	iStart, iEnd, err := windowSlice(candles, cfg)
	if err != nil {
		return nil, err
	}
	strat.ComputeIndicators(candles)
	if strat.WarmupCandles() > len(candles) {
		return nil, fmt.Errorf("data shorter than indicator warmup (%d < %d)", len(candles), strat.WarmupCandles())
	}

	n := iEnd - iStart + 1
	res := &Result{
		Cfg:          cfg,
		StrategyID:   strat.ID(),
		StrategyName: strat.Name(),
		Times:        make([]int64, n),
		StratEquity:  make([]float64, n),
		BaseEquity:   make([]float64, n),
		StratBTC:     make([]float64, n),
		BaseBTC:      make([]float64, n),
		Injected:     make([]float64, n),
		BaseInjCum:   make([]float64, n),
		Series:       strat.ExportSeries(),
	}

	// Strategy portfolio: receives initial capital + periodic injections.
	var sCash, sBTC, sInjCum, sFees float64
	// Baseline portfolio: only injects according to its own type.
	var bCash, bBTC, bInjCum, bFees float64

	var flowsS, flowsB []CashFlow
	var injDeltaS, injDeltaB []float64
	var baseTrades, stratTrades []Trade
	type pendingSig struct {
		sig   Signal
		index int
	}
	var pending pendingSig // signal emitted at close[i-1], fills at open[i]

	isDCA := cfg.BaselineType == BaselinePureDCA

	for k, i := 0, iStart; i <= iEnd; k, i = k+1, i+1 {
		c := candles[i]
		res.Times[k] = c.OpenTime
		var deltaS, deltaB float64

		// 1) injections -----------------------------------------------------
		if i == iStart {
			// t0: initial capital. Strategy banks it; DCA baseline buys at
			// this open ("immediately upon arrival"); LUMP_SUM baseline buys
			// the whole capital and never receives periodic injections.
			sInjCum += cfg.InitialCapital
			sCash += cfg.InitialCapital
			deltaS += cfg.InitialCapital
			flowsS = append(flowsS, CashFlow{Time: c.OpenTime, Amount: cfg.InitialCapital})
			if isDCA {
				bInjCum += cfg.InitialCapital
				bCash += cfg.InitialCapital
				deltaB += cfg.InitialCapital
				flowsB = append(flowsB, CashFlow{Time: c.OpenTime, Amount: cfg.InitialCapital})
			}
		} else if isDCA && cfg.PeriodicInjection > 0 {
			prev := candles[i-1]
			if injectionPeriodIndex(prev.Time(), cfg.InjectionInterval) != injectionPeriodIndex(c.Time(), cfg.InjectionInterval) {
				sInjCum += cfg.PeriodicInjection
				sCash += cfg.PeriodicInjection
				deltaS += cfg.PeriodicInjection
				flowsS = append(flowsS, CashFlow{Time: c.OpenTime, Amount: cfg.PeriodicInjection})
				bInjCum += cfg.PeriodicInjection
				bCash += cfg.PeriodicInjection
				deltaB += cfg.PeriodicInjection
				flowsB = append(flowsB, CashFlow{Time: c.OpenTime, Amount: cfg.PeriodicInjection})
			}
		}

		// Baseline always buys everything on arrival (DCA), or exactly once at
		// t0 (Lump Sum, whose periodic injections are disabled).
		if isDCA && deltaB > 0 && bCash > 0 {
			bBTC, bCash, bFees, baseTrades = baselineBuy(candles, i, baseTrades, bCash, 1.0, cfg, c.OpenTime)
		} else if !isDCA && i == iStart && bCash >= cfg.InitialCapital {
			bBTC, bCash, bFees, baseTrades = baselineBuy(candles, i, baseTrades, bCash, 1.0, cfg, c.OpenTime)
		}

		// 2) pending strategy signal -> fill at this candle's open -----------
		if pending.sig.Action != ActionHold {
			if pending.sig.Action == ActionBuy && sCash > 0 && pending.sig.Weight > 0 {
				spent := sCash * pending.sig.Weight
				fee := spent * cfg.FeeRate
				price := c.Open * (1 + cfg.SlippageRate)
				got := (spent - fee) / price
				sCash -= spent
				sBTC += got
				sFees += fee
				stratTrades = append(stratTrades, Trade{
					Index: pending.index, ExecTime: c.OpenTime, Side: ActionBuy,
					Price: price, BTC: got, Fiat: spent, Fee: fee, Tag: pending.sig.Tag,
				})
			} else if pending.sig.Action == ActionSell && sBTC > 0 && pending.sig.Weight > 0 {
				sold := sBTC * pending.sig.Weight
				price := c.Open * (1 - cfg.SlippageRate)
				gross := sold * price
				fee := gross * cfg.FeeRate
				net := gross - fee
				sBTC -= sold
				sCash += net
				sFees += fee
				stratTrades = append(stratTrades, Trade{
					Index: pending.index, ExecTime: c.OpenTime, Side: ActionSell,
					Price: price, BTC: sold, Fiat: net, Fee: fee, Tag: pending.sig.Tag,
				})
			}
		}

		// 3) record equity at close ------------------------------------------
		stratState := PortfolioState{Cash: sCash, BTC: sBTC, TotalInjected: sInjCum}
		res.StratEquity[k] = stratState.Cash + stratState.BTC*c.Close
		res.StratBTC[k] = stratState.BTC
		res.Injected[k] = stratState.TotalInjected
		res.BaseEquity[k] = bCash + bBTC*c.Close
		res.BaseBTC[k] = bBTC
		res.BaseInjCum[k] = bInjCum
		injDeltaS = append(injDeltaS, deltaS)
		injDeltaB = append(injDeltaB, deltaB)

		// 4) signal at close[i], executed at open[i+1] -----------------------
		sig := strat.OnCandle(i, stratState)
		pending.sig, pending.index = sig, i
	}

	// The signal of the very last candle can never be executed (needs t+1).
	// Zero look-ahead means it is dropped, mirroring live behavior.

	res.Trades = stratTrades
	res.BaseTrades = baseTrades
	res.TotalInjected, res.BaseInjectedTotal = sInjCum, bInjCum
	res.FeesPaid, res.BaseFeesPaid = sFees, bFees

	prices := make([]float64, n)
	for k := 0; k < n; k++ {
		prices[k] = candles[iStart+k].Close
	}
	res.Report = ComposeReport(cfg.Timeframe, res.Times,
		res.StratEquity, res.BaseEquity, res.StratBTC, res.BaseBTC,
		injDeltaS, injDeltaB, flowsS, flowsB, stratTrades, baseTrades,
		prices, sFees, bFees, sInjCum, bInjCum, n)
	return res, nil
}

// baselineBuy fills the baseline's buy-at-arrival order at candle i's open.
func baselineBuy(candles []Candle, i int, trades []Trade, cash, weight float64,
	cfg EngineConfig, signalTime int64) (btc, cashLeft, fees float64, out []Trade) {
	spent := cash * weight
	if spent <= 0 {
		return 0, cash, 0, trades
	}
	fee := spent * cfg.FeeRate
	price := candles[i].Open * (1 + cfg.SlippageRate)
	got := (spent - fee) / price
	trades = append(trades, Trade{
		Index: i, ExecTime: candles[i].OpenTime, Side: ActionBuy,
		Price: price, BTC: got, Fiat: spent, Fee: fee, Tag: "baseline DCA arrival",
	})
	_ = signalTime
	return got, cash - spent, fee, trades
}
