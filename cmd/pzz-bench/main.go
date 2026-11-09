// pzz-bench — bitcoin accumulation studio: a zero look-ahead backtesting TUI.
//
// Flags mirror the sidebar parameters; any omitted value falls back to the
// last-run preset (or built-in defaults). The SQLite cache lives under the
// XDG data dir; Binance + mempool.space fill the missing delta on demand.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bc1pzzfnxl/pzz-bench/internal/config"
	"github.com/bc1pzzfnxl/pzz-bench/internal/data"
	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
	"github.com/bc1pzzfnxl/pzz-bench/internal/tui"
	"github.com/bc1pzzfnxl/pzz-bench/internal/tui/components"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "v0.1.0"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		wizard      = flag.Bool("wizard", false, "run the guided interactive wizard (no TUI)")
		symbol      = flag.String("symbol", "", "BTCUSDT | BTCEUR | BTCUSD")
		timeframe   = flag.String("timeframe", "", "15m | 1h | 4h | 1d | 1w")
		start       = flag.String("start", "", "backtest window start (YYYY-MM-DD)")
		end         = flag.String("end", "", "backtest window end (YYYY-MM-DD)")
		capital     = flag.String("capital", "", "initial capital")
		dca         = flag.String("dca", "", "periodic injection amount")
		interval    = flag.String("interval", "", "daily | weekly | biweekly | monthly")
		fee         = flag.String("fee", "", "taker fee in percent (0.10)")
		slippage    = flag.String("slippage", "", "slippage in percent (0.05)")
		baseline    = flag.String("baseline", "", "PURE_DCA | LUMP_SUM")
		strategy    = flag.String("strategy", "", "MA_CROSS | RSI_DCA | MACD_TREND | MACRO_200W")
		paramList   = flag.String("param", "", "strategy params as k=v pairs (comma separated)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("pzz-bench " + version)
		return
	}

	if *wizard {
		runWizard(version)
		return
	}

	paths := config.Resolve()
	if err := paths.EnsureDirs(); err != nil {
		fatalf("creating config dirs: %v", err)
	}
	store, err := data.OpenStore(paths.DatabasePath())
	if err != nil {
		fatalf("opening sqlite store at %s: %v", paths.DatabasePath(), err)
	}
	defer func() { _ = store.Close() }()

	loader := data.NewDataLoader(store)
	model := tui.New(paths, loader)
	applyFlags(model, flagSet{
		symbol: *symbol, timeframe: *timeframe, start: *start, end: *end,
		capital: *capital, dca: *dca, interval: *interval, fee: *fee,
		slippage: *slippage, baseline: *baseline, strategy: *strategy,
		params: *paramList,
	})

	fmt.Fprintln(os.Stderr, "⚡ pzz-bench", version, "— press ? for the key bindings")
	if _, err := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	).Run(); err != nil {
		fatalf("terminal error: %v", err)
	}
}

// flagSet bundles the CLI overrides of one launch.
type flagSet struct {
	symbol, timeframe, start, end   string
	capital, dca, interval          string
	fee, slippage, baseline         string
	strategy, params                string
}

// applyFlags seeds the sidebar from CLI flags (preset values stay put for
// omitted flags).
func applyFlags(model *tui.Model, fs flagSet) {
	set := model.SidebarValues().Set
	set("symbol", fs.symbol)
	set("timeframe", fs.timeframe)
	set("start", fs.start)
	set("end", fs.end)
	set("capital", fs.capital)
	set("dca", fs.dca)
	set("interval", fs.interval)
	set("fee", fs.fee)
	set("slippage", fs.slippage)
	set("baseline", fs.baseline)
	set("strategy", fs.strategy)
	if fs.params != "" {
		for _, kv := range strings.Split(fs.params, ",") {
			k, v, ok := strings.Cut(kv, "=")
			if ok && strings.TrimSpace(k) != "" {
				set(strings.TrimSpace(k), strings.TrimSpace(v))
			}
		}
	}
	model.SyncDefaults()
}

// runWizard is the guided CLI flow: collect parameters with huh, run the
// benchmark, and write report.md + trades.csv to ./exports.
func runWizard(version string) {
	fmt.Println("⚡ pzz-bench", version, "— guided wizard")
	fv := &components.FormValues{}
	fv.InstallDefaults(true)
	form := components.BuildForm(fv, components.CatppuccinTheme())
	if err := form.Run(); err != nil {
		fatalf("wizard cancelled: %v", err)
	}

	fmt.Printf("\nselected strategy: %s\n", fv.Get("strategy"))
	if desc, ok := engine.Description(strings.TrimSpace(fv.Get("strategy"))); ok {
		for _, spec := range desc.Specs {
			if v := fv.Get(spec.Name); v != "" {
				fmt.Printf("  %s: %s\n", spec.Display, v)
			}
		}
	}

	cfg, err := fv.EngineConfig()
	if err != nil {
		fatalf("invalid parameters: %v", err)
	}
	strat, err := fv.BuildStrategy()
	if err != nil {
		fatalf("strategy: %v", err)
	}
	if err := engine.Validate(cfg); err != nil {
		fatalf("invalid config: %v", err)
	}

	paths := config.Resolve()
	if err := paths.EnsureDirs(); err != nil {
		fatalf("config dirs: %v", err)
	}
	store, err := data.OpenStore(paths.DatabasePath())
	if err != nil {
		fatalf("sqlite: %v", err)
	}
	defer func() { _ = store.Close() }()

	loader := data.NewDataLoader(store)
	loader.Progress = func(label string, n int) {
		fmt.Printf("\r%s (+%d)", label, n)
	}

	fmt.Println("ensuring candles…")
	from := engine.WarmupStart(cfg, strat)
	candles, err := loader.Ensure(context.Background(), cfg.Symbol, cfg.Timeframe,
		from.UnixMilli(), cfg.EndDate.UnixMilli())
	if err != nil {
		fatalf("data: %v", err)
	}
	fmt.Printf("\nrunning %d candles…\n", len(candles))

	res, err := engine.Run(candles, cfg, strat)
	if err != nil {
		fatalf("run: %v", err)
	}
	rep := res.Report

	// export
	if err := os.MkdirAll(paths.ExportsDir, 0o755); err != nil {
		fatalf("exports dir: %v", err)
	}
	stamp := time.Now().Format("20060102-150405")
	mdPath := filepath.Join(paths.ExportsDir, fmt.Sprintf("report-%s-%s.md", cfg.Symbol, stamp))
	csvPath := filepath.Join(paths.ExportsDir, fmt.Sprintf("trades-%s-%s.csv", cfg.Symbol, stamp))
	if err := os.WriteFile(mdPath, []byte(tui.BuildMarkdown(res, fv.ParamsSnapshot())), 0o644); err != nil {
		fatalf("write report: %v", err)
	}
	if err := os.WriteFile(csvPath, []byte(tui.BuildTradesCSV(res.Trades)), 0o644); err != nil {
		fatalf("write trades: %v", err)
	}

	fmt.Printf("strategy: %s  baseline: %s\n", res.StrategyName, components.BaselineLabels[cfg.BaselineType])
	fmt.Printf("sats alpha: %+.2f%%   equity alpha: %+.2f%%   MDD: %.2f%%\n",
		rep.SatsAlpha, rep.EquityAlpha, rep.Strat.MDD*100)
	fmt.Printf("exported: %s\n         %s\n", mdPath, csvPath)
}
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pzz-bench: "+format+"\n", args...)
	os.Exit(1)
}
