// Package data streams OHLCV candles from public market APIs into a local
// SQLite cache and serves merged backtests ranges. Pure Go, zero CGo.
package data

import (
	"context"
	"net/http"
	"time"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

// Symbols supported end to end. BTCUSD exists only in the mempool.space
// mirror (daily/weekly granularity), BTCUSDT and BTCEUR are native Binance.
const (
	SymBTCUSDT = "BTCUSDT"
	SymBTCEUR  = "BTCEUR"
	SymBTCUSD  = "BTCUSD"
)

// Symbols is the TUI display list.
var Symbols = []string{SymBTCUSDT, SymBTCEUR, SymBTCUSD}

// CandleIntervalHTTPDelay is the polite pause between paginated API requests.
const CandleIntervalHTTPDelay = 150 * time.Millisecond

// Provider is a source of candles for backtests. Range returns candles whose
// OpenTime falls in [startMs, endMs], sorted ascending.
type Provider interface {
	ID() string
	// CanServe reports whether this provider can answer (symbol, timeframe).
	CanServe(symbol, timeframe string) bool
	Range(ctx context.Context, symbol, timeframe string, startMs, endMs int64) ([]engine.Candle, error)
}

// binanceEndpoints round-robins the public REST hosts (api.binance.com and its
// geo mirrors) to survive single-host throttling.
var binanceEndpoints = []string{
	"https://api.binance.com",
	"https://api1.binance.com",
	"https://api2.binance.com",
	"https://api3.binance.com",
	"https://api4.binance.com",
}

func sharedHTTPClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}
