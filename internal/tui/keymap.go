package tui

import (
	"time"
)

// compactDate renders "22' Jan 24" style labels for chart axes.
func compactDate(unixSec int64) string {
	t := time.Unix(unixSec, 0).UTC()
	return t.Format("02 Jan 06")
}

// isoDate renders YYYY-MM-DD.
func isoDate(unixMs int64) string {
	return time.Unix(unixMs/1000, 0).UTC().Format("2006-01-02")
}

// KeyBindingRow is the reference matrix shown by the help overlay.
var KeyBindingRows = [][3]string{
	{"Tab / Shift+Tab", "Global", "Cycle focus: sidebar -> chart -> results (studio) or sidebar -> main (tools)"},
	{"1 / 2", "Global", "Workspaces: studio / tools"},
	{"r / Ctrl+R", "Global", "Run / re-run the benchmark with current sidebar values"},
	{"v", "Chart", "Cycle chart mode: fiat equity -> sats stack -> drawdown"},
	{"h / l or ← / →", "Chart", "Pan the time window left / right"},
	{"+ / - or k / j", "Chart", "Zoom time window in / out"},
	{"0", "Chart", "Reset zoom to the full backtest range"},
	{"[ / ]", "Studio results", "Toggle KPI summary <-> trade ledger"},
	{"↑ / ↓ / PgUp / PgDn / Home / End", "Results table", "Row navigation in the focused table"},
	{"[ / ]", "Tools", "Switch optimizer <-> market cache sub-tab"},
	{"g", "Tools · optimizer", "Re-run the 2D parameter grid (auto on entry)"},
	{"m", "Tools · optimizer", "Cycle heatmap metric: Sats Alpha -> Sortino -> Equity Alpha"},
	{"← ↑ ↓ →", "Tools · optimizer", "Navigate heatmap cells"},
	{"u", "Tools · cache", "Update cache: pull missing candles from Binance/mempool"},
	{"s", "Global", "Save current parameters to presets.toml"},
	{"e", "Global", "Export report.md + trades CSV to ./exports/"},
	{"?", "Global", "Toggle this help overlay"},
	{"q / Ctrl+C", "Global", "Quit (flushes SQLite WAL, restores the terminal)"},
}
