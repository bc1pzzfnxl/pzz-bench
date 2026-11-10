package backtest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

type fakeLoader struct {
	startMs, endMs int64
	candles        []engine.Candle
	err            error
}

func (f *fakeLoader) Ensure(_ context.Context, _, _ string, startMs, endMs int64) ([]engine.Candle, error) {
	f.startMs, f.endMs = startMs, endMs
	return f.candles, f.err
}

func TestLoadCandlesUsesWarmupStart(t *testing.T) {
	strat, err := engine.BuildStrategy(engine.IDMACross, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := engine.EngineConfig{
		Symbol:    "BTCUSDT",
		Timeframe: engine.TF1d,
		StartDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	f := &fakeLoader{}
	if _, err := LoadCandles(context.Background(), f, cfg, strat); err != nil {
		t.Fatal(err)
	}
	if want := engine.WarmupStart(cfg, strat).UnixMilli(); f.startMs != want {
		t.Fatalf("start = %d, want warmup %d", f.startMs, want)
	}
	if want := cfg.EndDate.UnixMilli(); f.endMs != want {
		t.Fatalf("end = %d, want %d", f.endMs, want)
	}
}

func TestLoadCandlesWrapsLoaderError(t *testing.T) {
	strat, err := engine.BuildStrategy(engine.IDMACross, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := engine.EngineConfig{Symbol: "BTCEUR", Timeframe: engine.TF1d}
	_, err = LoadCandles(context.Background(), &fakeLoader{err: errors.New("boom")}, cfg, strat)
	if err == nil || !strings.Contains(err.Error(), "load BTCEUR/1d") {
		t.Fatalf("error = %v, want wrapped loader error", err)
	}
}
