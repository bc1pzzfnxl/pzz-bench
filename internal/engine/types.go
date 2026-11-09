// Package engine contains the zero look-ahead backtesting engine: candle
// types, vectorized indicators, strategy implementations, the event-driven
// simulator, benchmark metrics and the concurrent parameter grid optimizer.
package engine

import (
	"fmt"
	"time"
)

// SatsPerBTC is the number of satoshis in one bitcoin.
const SatsPerBTC = 100_000_000

// Timeframes supported by the data layer and the engine.
const (
	TF15m = "15m"
	TF1h  = "1h"
	TF4h  = "4h"
	TF1d  = "1d"
	TF1w  = "1w"
)

// Timeframes lists every supported interval, ordered from finest to coarsest.
var Timeframes = []string{TF15m, TF1h, TF4h, TF1d, TF1w}

// TimeframeMS returns the duration of one candle in milliseconds for the
// given timeframe. Binance aligned intervals are used for every timeframe.
func TimeframeMS(tf string) int64 {
	switch tf {
	case TF15m:
		return 15 * 60 * 1000
	case TF1h:
		return 60 * 60 * 1000
	case TF4h:
		return 4 * 60 * 60 * 1000
	case TF1w:
		return 7 * 24 * 60 * 60 * 1000
	default: // TF1d
		return 24 * 60 * 60 * 1000
	}
}

// CandlesPerYear returns how many full candles of the given timeframe span
// one year (365 days convention, BTC trades 7/7).
func CandlesPerYear(tf string) float64 {
	switch tf {
	case TF15m:
		return 365 * 96
	case TF1h:
		return 365 * 24
	case TF4h:
		return 365 * 6
	case TF1w:
		return 52
	default: // TF1d
		return 365
	}
}

// Candle is a single OHLCV bar. OpenTime is a Unix timestamp in milliseconds
// (UTC), matching the SQLite cache and the Binance klines API.
type Candle struct {
	OpenTime  int64
	CloseTime int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

// Time returns the candle open time as a UTC time.Time.
func (c Candle) Time() time.Time {
	return time.Unix(c.OpenTime/1000, (c.OpenTime%1000)*1_000_000).UTC()
}

// BTCToSats converts a BTC float quantity into integer satoshis (floor).
func BTCToSats(btc float64) int64 {
	if btc <= 0 {
		return 0
	}
	return int64(btc * SatsPerBTC)
}

// ValidateTimeframe reports whether tf is supported.
func ValidateTimeframe(tf string) error {
	for _, t := range Timeframes {
		if t == tf {
			return nil
		}
	}
	return fmt.Errorf("unsupported timeframe %q (want one of %v)", tf, Timeframes)
}

// SignalAction is the direction demanded by a strategy at a candle close.
type SignalAction uint8

// Trading actions emitted by strategies.
const (
	ActionHold SignalAction = iota
	ActionBuy
	ActionSell
)

// String renders the action for logs and the trade ledger.
func (a SignalAction) String() string {
	switch a {
	case ActionBuy:
		return "BUY"
	case ActionSell:
		return "SELL"
	default:
		return "HOLD"
	}
}

// Signal is a strategy decision taken at the close of candle `index`.
// The simulator strictly executes it at the open of candle index+1
// (zero look-ahead) with slippage and fees applied.
type Signal struct {
	Action SignalAction
	Weight float64 // 0..1 fraction of available cash to spend or BTC to sell
	Tag    string  // short human label, e.g. "RSI(14)=24.1 < 30"
}

// PortfolioState is the mutable accounting state handed to strategies.
// Cash is dry powder in the quote currency, BTC is the coin balance.
// TotalInjected tracks cumulative capital added (cash injections).
type PortfolioState struct {
	Cash          float64
	BTC           float64
	TotalInjected float64
}

// Strategy is the contract every built-in or user strategy implements.
// ComputeIndicators must be called once before the simulation loop with the
// full candle slice; OnCandle is then called for each simulated index.
type Strategy interface {
	ID() string
	Name() string
	// WarmupCandles is the smallest index at which signals are meaningful.
	WarmupCandles() int
	// ComputeIndicators precomputes every indicator slice on the full candle set.
	ComputeIndicators(candles []Candle)
	// OnCandle returns the signal for candle `index` (state reflects fills
	// executed at the open of that same candle and injections landed at it).
	OnCandle(index int, state PortfolioState) Signal
	// ExportSeries exposes named indicator series aligned 1:1 with candles.
	ExportSeries() map[string][]float64
}

// Trade is one executed order. Index points at the candle whose close
// produced the signal, ExecTime is the open time of the fill candle (t+1).
type Trade struct {
	Index    int
	ExecTime int64
	Side     SignalAction
	Price    float64 // effective fill price (slippage applied)
	BTC      float64 // ΔBTC acquired (buy) or sold (sell)
	Fiat     float64 // fiat spent (buy, gross) or received (sell, net)
	Fee      float64
	Tag      string
}

// EngineConfig is the immutable configuration for one benchmark run.
type EngineConfig struct {
	Symbol            string // "BTCUSDT" | "BTCEUR" | "BTCUSD"
	Timeframe         string // see Timeframes
	StartDate         time.Time
	EndDate           time.Time
	InitialCapital    float64
	PeriodicInjection float64
	InjectionInterval string // "daily" | "weekly" | "biweekly" | "monthly"
	FeeRate           float64 // taker fee, decimal (0.001 == 0.10%)
	SlippageRate      float64 // slippage, decimal (0.0005 == 0.05%)
	BaselineType      string // "PURE_DCA" | "LUMP_SUM"
}

// Injection intervals.
const (
	InjDaily    = "daily"
	InjWeekly   = "weekly"
	InjBiweekly = "biweekly"
	InjMonthly  = "monthly"
)

// Baseline types.
const (
	BaselinePureDCA  = "PURE_DCA"
	BaselineLumpSum  = "LUMP_SUM"
)

// injectionPeriodIndex buckets a millisecond timestamp into sequential periods
// of the given injection interval. Crossing a boundary (index changes) is when
// a periodic cash injection is due.
func injectionPeriodIndex(t time.Time, interval string) int64 {
	utc := t.UTC()
	switch interval {
	case InjDaily:
		return utc.Unix() / 86400
	case InjBiweekly:
		return utc.Unix() / (14 * 86400)
	case InjMonthly:
		return int64(utc.Year())*12 + int64(utc.Month())
	default: // InjWeekly
		return utc.Unix() / (7 * 86400)
	}
}

// ValidateInjectionInterval reports whether interval is supported.
func ValidateInjectionInterval(interval string) error {
	for _, iv := range []string{InjDaily, InjWeekly, InjBiweekly, InjMonthly} {
		if iv == interval {
			return nil
		}
	}
	return fmt.Errorf("unsupported injection interval %q", interval)
}

// ValidateBaseline reports whether baseline is supported.
func ValidateBaseline(baseline string) error {
	if baseline == BaselinePureDCA || baseline == BaselineLumpSum {
		return nil
	}
	return fmt.Errorf("unsupported baseline type %q", baseline)
}
