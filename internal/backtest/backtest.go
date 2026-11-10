// Package backtest composes the data loader and the engine into the single
// benchmark path shared by the TUI and the CLI wizard.
package backtest

import (
	"context"
	"fmt"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

// Loader is the slice of data.DataLoader a benchmark needs. Keeping it small
// makes the composition testable without a SQLite store.
type Loader interface {
	Ensure(ctx context.Context, symbol, timeframe string, startMs, endMs int64) ([]engine.Candle, error)
}

// LoadCandles ensures the cache covers the strategy warmup and returns the
// candles for the configured window.
func LoadCandles(ctx context.Context, loader Loader, cfg engine.EngineConfig, strat engine.Strategy) ([]engine.Candle, error) {
	from := engine.WarmupStart(cfg, strat)
	candles, err := loader.Ensure(ctx, cfg.Symbol, cfg.Timeframe, from.UnixMilli(), cfg.EndDate.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("load %s/%s: %w", cfg.Symbol, cfg.Timeframe, err)
	}
	return candles, nil
}

// Run loads the candles and executes one benchmark.
func Run(ctx context.Context, loader Loader, cfg engine.EngineConfig, strat engine.Strategy) (*engine.Result, error) {
	candles, err := LoadCandles(ctx, loader, cfg, strat)
	if err != nil {
		return nil, err
	}
	return engine.Run(candles, cfg, strat)
}
