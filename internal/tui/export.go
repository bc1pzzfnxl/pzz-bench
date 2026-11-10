// Export pipeline: PRD section 7 — report.md + trades.csv into ./exports.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
	"github.com/bc1pzzfnxl/pzz-bench/internal/tui/components"
)

// WriteExports writes report.md and trades.csv into dir and returns both paths.
// The TUI export key and the CLI wizard share this pipeline.
func WriteExports(dir, symbol string, res *engine.Result, params engine.Params) (string, string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	mdPath := filepath.Join(dir, fmt.Sprintf("report-%s-%s.md", symbol, stamp))
	csvPath := filepath.Join(dir, fmt.Sprintf("trades-%s-%s.csv", symbol, stamp))
	if err := os.WriteFile(mdPath, []byte(BuildMarkdown(res, params)), 0o644); err != nil {
		return "", "", fmt.Errorf("write report: %w", err)
	}
	if err := os.WriteFile(csvPath, []byte(BuildTradesCSV(res.Trades)), 0o644); err != nil {
		return "", "", fmt.Errorf("write trades: %w", err)
	}
	return mdPath, csvPath, nil
}

// BuildMarkdown renders the full comparative report for one run.
func BuildMarkdown(res *engine.Result, params engine.Params) string {
	rep := res.Report
	c := res.Cfg
	var b strings.Builder

	fmt.Fprintf(&b, "# pzz-bench backtest report\n\n")
	fmt.Fprintf(&b, "- **Pair:** %s (%s)\n", c.Symbol, c.Timeframe)
	fmt.Fprintf(&b, "- **Window:** %s → %s (%d candles)\n",
		rep.From.Format("2006-01-02"), rep.To.Format("2006-01-02"), rep.Candles)
	fmt.Fprintf(&b, "- **Strategy:** %s\n", res.StrategyName)
	if desc, ok := engine.Description(res.StrategyID); ok {
		for _, spec := range desc.Specs {
			if v, ok := params[spec.Name]; ok && v != "" {
				fmt.Fprintf(&b, "  - %s: `%s`\n", spec.Display, v)
			}
		}
	}
	fmt.Fprintf(&b, "- **Baseline:** %s\n", components.BaselineLabels[c.BaselineType])
	fmt.Fprintf(&b, "- **Capital:** %.2f initial, %.2f %s injections\n",
		c.InitialCapital, c.PeriodicInjection, c.InjectionInterval)
	fmt.Fprintf(&b, "- **Costs:** fee %.3f%%, slippage %.3f%%\n",
		c.FeeRate*100, c.SlippageRate*100)
	fmt.Fprintf(&b, "- **Price:** %.2f → %.2f\n\n", rep.PriceStart, rep.PriceEnd)

	fmt.Fprintf(&b, "| Metric | Strategy | Baseline | Delta |\n")
	fmt.Fprintf(&b, "|---|---|---|---|\n")
	fmt.Fprintf(&b, "| Final equity | %.2f | %.2f | %+.2f%% |\n",
		rep.Strat.FinalEquity, rep.Base.FinalEquity, rep.EquityAlpha)
	fmt.Fprintf(&b, "| Total sats | %s | %s | %+.2f%% |\n",
		satsFmt(rep.Strat.SatsT), satsFmt(rep.Base.SatsT), rep.SatsAlpha)
	fmt.Fprintf(&b, "| TWRR (ann.) | %.2f%% | %.2f%% | %+0.2f pts |\n",
		pct100f(rep.Strat.TWRR), pct100f(rep.Base.TWRR), (rep.Strat.TWRR-rep.Base.TWRR)*100)
	fmt.Fprintf(&b, "| XIRR (ann.) | %.2f%% | %.2f%% | %+0.2f pts |\n",
		pct100f(rep.Strat.XIRR), pct100f(rep.Base.XIRR), (rep.Strat.XIRR-rep.Base.XIRR)*100)
	fmt.Fprintf(&b, "| Max drawdown | %.2f%% | %.2f%% | %+.2f pts |\n",
		pct100f(rep.Strat.MDD), pct100f(rep.Base.MDD),
		(abs(rep.Strat.MDD)-abs(rep.Base.MDD))*100)
	fmt.Fprintf(&b, "| Calmar | %s | %s | — |\n", f2(rep.Strat.Calmar), f2(rep.Base.Calmar))
	fmt.Fprintf(&b, "| Sharpe | %s | %s | — |\n", f2(rep.Strat.Sharpe), f2(rep.Base.Sharpe))
	fmt.Fprintf(&b, "| Sortino | %s | %s | — |\n", f2(rep.Strat.Sortino), f2(rep.Base.Sortino))
	fmt.Fprintf(&b, "| Fees paid | %.2f (%.2f%% of injected) | %.2f (%.2f%%) | — |\n",
		rep.Strat.FeesPaid, rep.FeePercentS*100, rep.Base.FeesPaid, rep.FeePercentB*100)
	fmt.Fprintf(&b, "| Trades | %d b / %d s | %d b / %d s | — |\n",
		rep.Strat.BuyCount, rep.Strat.SellCount, rep.Base.BuyCount, rep.Base.SellCount)
	fmt.Fprintf(&b, "| Win rate | %s | — | — |\n", winPct(rep.Strat.WinRate))

	fmt.Fprintf(&b, "\n## Cash flows\n\n")
	fmt.Fprintf(&b, "- Total injected (strategy): %.2f\n- Total injected (baseline): %.2f\n",
		rep.TotalInjectedS, rep.TotalInjectedB)

	fmt.Fprintf(&b, "\n> Verdict: sats alpha %+.2f%% — the active strategy "+
		"%s passive %s over this window (zero look-ahead fills, fees and slippage simulated).\n",
		rep.SatsAlpha, verdictWord(rep.SatsAlpha), components.BaselineLabels[c.BaselineType])
	return b.String()
}

// abs returns the absolute value of a float.
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func f2(v float64) string {
	if v != v {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", v)
}

func pct100f(v float64) float64 {
	if v != v {
		return 0
	}
	return v * 100
}

func winPct(v float64) string {
	if v != v {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", v*100)
}

func satsFmt(n int64) string {
	s := fmt.Sprintf("%d", n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, " ")
}

func verdictWord(v float64) string {
	if v >= 0 {
		return "beats"
	}
	return "trails"
}

// buildTradesCSV renders the strategy trade list as CSV.
func BuildTradesCSV(trades []engine.Trade) string {
	var b strings.Builder
	b.WriteString("exec_time,side,price,btc,fiat,fee,tag\n")
	for _, t := range trades {
		b.WriteString(fmt.Sprintf("%d,%s,%.8f,%.8f,%.8f,%.8f,%q\n",
			t.ExecTime, t.Side, t.Price, t.BTC, t.Fiat, t.Fee, t.Tag))
	}
	return b.String()
}
