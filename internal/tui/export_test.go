package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

func TestWriteExportsWritesReportAndTrades(t *testing.T) {
	res := &engine.Result{
		Cfg: engine.EngineConfig{
			Symbol:            "BTCUSDT",
			Timeframe:         engine.TF1d,
			BaselineType:      engine.BaselinePureDCA,
			InitialCapital:    1000,
			PeriodicInjection: 50,
			InjectionInterval: engine.InjWeekly,
			FeeRate:           0.001,
			SlippageRate:      0.0005,
		},
		StrategyID:   engine.IDMACross,
		StrategyName: "Dual MA Crossover",
		Report: &engine.BenchmarkReport{
			From:        time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			To:          time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			Candles:     1461,
			PriceStart:  7200,
			PriceEnd:    42000,
			SatsAlpha:   2.5,
			EquityAlpha: 3.5,
			Strat: engine.CurveStats{
				FinalEquity: 2200,
				SatsT:       5238095,
			},
			Base: engine.CurveStats{
				FinalEquity: 2100,
				SatsT:       5000000,
			},
		},
		Trades: []engine.Trade{{
			ExecTime: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC).UnixMilli(),
			Side:     engine.ActionBuy,
			Price:    7250,
			BTC:      0.137,
			Fiat:     1000,
			Fee:      1,
			Tag:      "MA cross up",
		}},
	}

	dir := t.TempDir()
	mdPath, csvPath, err := WriteExports(dir, "BTCUSDT", res, engine.Params{"fast": "21", "slow": "55"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(mdPath) != dir || filepath.Dir(csvPath) != dir {
		t.Fatalf("exports written outside %s: %s %s", dir, mdPath, csvPath)
	}
	if !strings.Contains(filepath.Base(mdPath), "report-BTCUSDT-") ||
		!strings.Contains(filepath.Base(csvPath), "trades-BTCUSDT-") {
		t.Fatalf("unexpected export names: %s %s", mdPath, csvPath)
	}

	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# pzz-bench backtest report",
		"**Pair:** BTCUSDT (1d)",
		"**Strategy:** Dual MA Crossover",
		"Fast MA: `21`",
		"Slow MA: `55`",
		"Verdict: sats alpha +2.50%",
	} {
		if !strings.Contains(string(md), want) {
			t.Errorf("report missing %q:\n%s", want, md)
		}
	}

	csv, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(csv), "exec_time,side,price,btc,fiat,fee,tag\n") {
		t.Fatalf("trades CSV header mismatch:\n%s", csv)
	}
	if !strings.Contains(string(csv), "BUY,") || !strings.Contains(string(csv), "MA cross up") {
		t.Fatalf("trades CSV missing the recorded trade:\n%s", csv)
	}
}

func TestWriteExportsPropagatesMkdirError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(dir, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteExports(dir, "BTCUSDT", &engine.Result{}, nil); err == nil {
		t.Fatal("WriteExports accepted a file as export directory")
	}
}
