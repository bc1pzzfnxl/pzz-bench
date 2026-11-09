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

// keyHints is the footer single-line help string.
const keyHints = "[1/2/3] ws · [r] run · [v] mode · [h/l] pan · [+/-] zoom · [0] reset · [[/]] tabs · [s] save · [e] export · [g] grid · [u] sync · [?] help · [q] quit"

// KeyBindingRow is the reference matrix shown by the help overlay.
var KeyBindingRows = [][3]string{
	{"Tab / Shift+Tab", "Global", "Cycle focus: sidebar -> chart -> results"},
	{"1 / 2 / 3", "Global", "Workspaces: studio / grid optimizer / data manager"},
	{"r / Ctrl+R", "Global", "Run / re-run the benchmark with current sidebar values"},
	{"v", "Chart", "Cycle chart mode: fiat equity -> sats stack -> drawdown"},
	{"h / l or ← / →", "Chart", "Pan the time window left / right"},
	{"+ / - or k / j", "Chart", "Zoom time window in / out"},
	{"0", "Chart", "Reset zoom to the full backtest range"},
	{"[ / ]", "Bottom pane", "Toggle KPI summary <-> trade ledger and scroll"},
	{"j / k / t / b", "Bottom pane", "Row navigation in the focused table"},
	{"s", "Global", "Save current parameters to presets.toml"},
	{"e", "Global", "Export report.md + trades CSV to ./exports/"},
	{"g", "Workspace 2", "Re-run the 2D parameter grid (auto on entry)"},
	{"m", "Workspace 2", "Cycle heatmap metric: Sats Alpha -> Sortino -> Equity Alpha"},
	{"← ↑ ↓ →", "Workspace 2", "Navigate heatmap cells"},
	{"u", "Workspace 3", "Update cache: pull missing candles from Binance/mempool"},
	{"?", "Global", "Toggle this help overlay"},
	{"q / Ctrl+C", "Global", "Quit (flushes SQLite WAL, restores the terminal)"},
}
