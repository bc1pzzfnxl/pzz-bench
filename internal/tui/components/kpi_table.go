// Bottom pane: comparative KPI summary and trade ledger (bubbles tables).
package components

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

// BottomTab selects which table the bottom pane shows.
type BottomTab uint8

// Bottom-pane tabs, cycled with [ / ].
const (
	TabKPI BottomTab = iota
	TabLedger
)

// BottomTabName returns the display label of the active tab.
func BottomTabName(t BottomTab) string {
	switch t {
	case TabLedger:
		return "execution ledger"
	default:
		return "KPI summary"
	}
}

// KPIPane renders the two tabbed tables of the bottom pane.
type KPIPane struct {
	kpi    table.Model
	ledger table.Model
	tab    BottomTab
	height int
}

// NewKPIPane builds both tables at their default size.
func NewKPIPane() *KPIPane {
	kpi := table.New(
		table.WithColumns([]table.Column{
			{Title: "Metric", Width: 24},
			{Title: "Strategy", Width: 26},
			{Title: "Baseline", Width: 24},
			{Title: "Delta", Width: 18},
		}),
		table.WithFocused(true),
	)
	kpi.SetStyles(kpiStyles())

	ledger := table.New(
		table.WithColumns([]table.Column{
			{Title: "Exec Time", Width: 12},
			{Title: "Side", Width: 7},
			{Title: "Price", Width: 11},
			{Title: "BTC", Width: 10},
			{Title: "Fiat", Width: 13},
			{Title: "Fee", Width: 9},
			{Title: "Trigger", Width: 34},
		}),
		table.WithFocused(true),
	)
	ledger.SetStyles(ledgerStyles())

	return &KPIPane{kpi: kpi, ledger: ledger}
}

// kpiStyles paints the KPI table with the pzz palette.
func kpiStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#45475A")).
		Foreground(lipgloss.Color("#F9E2AF")).
		Bold(true).
		Padding(0, 1)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#1E1E2E")).
		Background(lipgloss.Color("#CBA6F7")).
		Bold(false)
	s.Cell = s.Cell.Foreground(lipgloss.Color("#CDD6F4")).Padding(0, 0)
	return s
}

// ledgerStyles paints the ledger table with the pzz palette.
func ledgerStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#45475A")).
		Foreground(lipgloss.Color("#F9E2AF")).
		Bold(true).
		Padding(0, 1)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#1E1E2E")).
		Background(lipgloss.Color("#45475A"))
	s.Cell = s.Cell.Foreground(lipgloss.Color("#CDD6F4")).Padding(0, 0)
	return s
}

// SetReport fills the KPI table from a benchmark report.
func (p *KPIPane) SetReport(rep *engine.BenchmarkReport) {
	if rep == nil {
		p.kpi.SetRows(nil)
		return
	}
	p.kpi.SetRows([]table.Row{
		{"final equity", money(rep.Strat.FinalEquity), money(rep.Base.FinalEquity),
			deltaStrat(rep.EquityAlpha, 2)},
		{"total sats", sats(rep.Strat.SatsT), sats(rep.Base.SatsT),
			deltaStrat(rep.SatsAlpha, 2)},
		{"sats alpha", pct(rep.SatsAlpha, 2), "-", "-"},
		{"equity alpha", pct(rep.EquityAlpha, 2), "-", "-"},
		{"TWRR ann.", pct(rep.Strat.TWRR, 2), pct(rep.Base.TWRR, 2), diffPts(rep.Strat.TWRR, rep.Base.TWRR)},
		{"XIRR ann.", pct(rep.Strat.XIRR, 2), pct(rep.Base.XIRR, 2), diffPts(rep.Strat.XIRR, rep.Base.XIRR)},
		{"max drawdown", mddStr(rep.Strat.MDD), mddStr(rep.Base.MDD),
			mddDelta(rep.Strat.MDD, rep.Base.MDD)},
		{"calmar", num(rep.Strat.Calmar, 2), num(rep.Base.Calmar, 2), diffRatio(rep.Strat.Calmar, rep.Base.Calmar)},
		{"sharpe", num(rep.Strat.Sharpe, 2), num(rep.Base.Sharpe, 2), diffRatio(rep.Strat.Sharpe, rep.Base.Sharpe)},
		{"sortino", num(rep.Strat.Sortino, 2), num(rep.Base.Sortino, 2), diffRatio(rep.Strat.Sortino, rep.Base.Sortino)},
		{"fees paid",
			money(rep.Strat.FeesPaid) + fmt.Sprintf(" (%.2f%%)", rep.FeePercentS*100),
			money(rep.Base.FeesPaid) + fmt.Sprintf(" (%.2f%%)", rep.FeePercentB*100),
			diffRatio(rep.Strat.FeesPaid, rep.Base.FeesPaid)},
		{"trades (b/s)",
			fmt.Sprintf("%d b / %d s", rep.Strat.BuyCount, rep.Strat.SellCount),
			fmt.Sprintf("%d b / %d s", rep.Base.BuyCount, rep.Base.SellCount),
			"-"},
		{"win rate", winRateStr(rep.Strat.WinRate), "-", "-"},
	})
}

// SetTrades fills the ledger table from the active strategy's trade list.
func (p *KPIPane) SetTrades(trades []engine.Trade) {
	rows := make([]table.Row, 0, len(trades))
	for _, t := range trades {
		rows = append(rows, table.Row{
			time.Unix(t.ExecTime/1000, 0).UTC().Format("02 Jan 06"),
			string(t.Side),
			fmt.Sprintf("%.2f", t.Price),
			fmt.Sprintf("%.6f", t.BTC),
			money(t.Fiat),
			fmt.Sprintf("%.2f", t.Fee),
			trunc(t.Tag, 34),
		})
	}
	p.ledger.SetRows(rows)
}

// Active returns the table currently displayed.
func (p *KPIPane) Active() *table.Model {
	if p.tab == TabLedger {
		return &p.ledger
	}
	return &p.kpi
}

// CycleTabs toggles KPI <-> ledger (`[` / `]`).
func (p *KPIPane) CycleTabs() {
	p.tab = (p.tab + 1) % 2
}

// ResetTabs pins the KPI tab as active.
func (p *KPIPane) ResetTabs() {
	p.tab = TabKPI
}

// Tab returns the active tab.
func (p *KPIPane) Tab() BottomTab { return p.tab }

// Row navigation for the focused table.
func (p *KPIPane) MoveUp(n int)   { p.Active().MoveUp(n) }
func (p *KPIPane) MoveDown(n int) { p.Active().MoveDown(n) }
func (p *KPIPane) GotoTop()       { p.Active().GotoTop() }
func (p *KPIPane) GotoBottom()    { p.Active().GotoBottom() }

// Resize re-applies the pane size when the window changes.
func (p *KPIPane) Resize(w, h int) {
	p.height = h
	p.kpi.SetWidth(w - 4)
	p.kpi.SetHeight(h - 4)
	p.ledger.SetWidth(w - 4)
	p.ledger.SetHeight(h - 4)
}

// PageUp / PageDown scroll the active table by roughly one page.
func (p *KPIPane) PageUp()   { p.Active().MoveUp(max(1, p.height-4)) }
func (p *KPIPane) PageDown() { p.Active().MoveDown(max(1, p.height-4)) }

// TabLabel renders the tab header with the active tab highlighted.
func (p *KPIPane) TabLabel() string {
	mk := func(name string, on bool) string {
		s := lipgloss.NewStyle().Foreground(lipgloss.Color("#A6ADC8"))
		if on {
			s = s.Foreground(lipgloss.Color("#F9E2AF")).Bold(true).Underline(true)
		}
		return s.Render("[" + name + "]")
	}
	return mk("KPI", p.tab == TabKPI) + mk("Ledger", p.tab == TabLedger)
}

// View renders the active table.
func (p *KPIPane) View() string {
	if p.tab == TabLedger {
		return p.ledger.View()
	}
	return p.kpi.View()
}

// Delta renders an alpha like the PRD pills (green when positive).
func deltaStrat(v float64, digits int) string {
	if -0.0005 < v && v < 0.0005 {
		return styleDim("±0.00%")
	}
	s := fmt.Sprintf("%+.*f%%", digits, v)
	if v >= 0 {
		return styleMint(s)
	}
	return styleRed(s)
}

// diffPts compares two percents: "+32.4 pts".
func diffPts(a, b float64) string {
	if isNaN(a) || isNaN(b) {
		return "n/a"
	}
	return deltaStrat((a-b)*100, 1)
}

// diffRatio compares two ratios, valid when b != 0.
func diffRatio(a, b float64) string {
	if isNaN(a) || isNaN(b) || b == 0 {
		return "n/a"
	}
	return deltaStrat((a-b)/vAbs(b)*100, 1)
}

// mddStr renders drawdown as "-22.4%".
func mddStr(v float64) string {
	if isNaN(v) || v >= 0 {
		return "0%"
	}
	return fmt.Sprintf("-%.1f%%", vAbs(v)*100)
}

// mddDelta compares drawdown pain: a shallower strategy shows green.
func mddDelta(a, b float64) string {
	if isNaN(a) || isNaN(b) {
		return "n/a"
	}
	pts := (vAbs(a) - vAbs(b)) * 100
	s := fmt.Sprintf("%+.1f pts", pts)
	if pts >= 0 {
		return styleRed(s)
	}
	return styleMint(s)
}

// winRateStr renders "63.2%".
func winRateStr(v float64) string {
	if isNaN(v) {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", v*100)
}

// trunc clips a string to n runes with an ellipsis.
func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 2 {
		return string(r[:1])
	}
	return string(r[:n-1]) + "…"
}

func styleMint(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1")).Bold(true).Render(s)
}

func styleRed(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8")).Bold(true).Render(s)
}

func styleDim(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#A6ADC8")).Render(s)
}
