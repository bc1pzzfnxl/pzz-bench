// Rendering of the root model: workspace layouts, title/status bars and the
// help overlay.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
	"github.com/bc1pzzfnxl/pzz-bench/internal/tui/components"
)

// responsive layout limits (PRD 6.3).
const (
	minWidth  = 100
	minHeight = 30
	sidebarW  = 28
)

// View renders the full workspace screen.
func (m *Model) View() string {
	if m.W < minWidth || m.H < minHeight {
		return lipgloss.Place(m.W, m.H, lipgloss.Center, lipgloss.Center,
			styleBad.Render("terminal too small — min 100×30 (have "+
				fmt.Sprintf("%dx%d", m.W, m.H)+"), resize to enter the studio"))
	}

	top := m.titleBar(m.W)
	bottom := m.statusBar(m.W)
	body := m.content()
	help := ""
	if m.help {
		help = "\n" + m.helpOverlay()
	}
	return top + "\n" + body + help + "\n" + bottom
}

// titleBar draws the top chrome row.
func (m *Model) titleBar(w int) string {
	left := "⚡ pzz-bench — bitcoin accumulation studio"
	right := workspaceName(m.ws)
	if m.busy {
		right += "  ● working…"
	}
	fill := w - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if fill < 1 {
		return titleBarStyle.Render(truncate(left, w))
	}
	return titleBarStyle.Render(left + strings.Repeat(" ", fill) + right)
}

// statusBar draws the bottom chrome row: status chip + keyboard hints.
func (m *Model) statusBar(w int) string {
	st := m.status
	if st == "" {
		st = "ready"
	}
	chip := statusBarStyle
	if m.isErr {
		chip = chip.Foreground(lipgloss.Color("#1E1E2E")).Background(lipgloss.Color(SoftRed))
	}
	chipW := lipgloss.Width(st) + 2
	hintsW := w - chipW - 1
	hints := truncate(keyHints, maxI(hintsW, 0))
	line := chip.Render(st) + " " + lipgloss.NewStyle().
		Foreground(lipgloss.Color(Subtext)).Render(hints)
	pad := w - lipgloss.Width(line)
	if pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}

// truncate clips a string to at most w visible columns.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}

// content renders the workspace body (or the help overlay above it).
func (m *Model) content() string {
	contentH := m.H - 2 // title + status rows
	switch m.ws {
	case wsGrid:
		return m.gridPane(contentH)
	case wsData:
		return m.dataPane(contentH)
	default:
		return m.studioPane(contentH)
	}
}

// studioPane is workspace 1: sidebar + chart stack + bottom tables.
func (m *Model) studioPane(contentH int) string {
	mainW := m.W - sidebarW - 1
	if mainW < sideMinWidth {
		mainW = sideMinWidth
	}
	chartH := int(0.55 * float64(contentH))
	oscH := int(0.20 * float64(contentH))
	if oscH < 5 {
		oscH = 5
	}
	bottomH := contentH - chartH - oscH
	if bottomH < 5 {
		bottomH = 5
	}
	m.syncSizes(mainW, chartH, oscH, bottomH)

	// chart title
	mode := components.ChartModeName(m.chart.Mode())
	chartTitle := "● " + mode
	if m.chart.HasData() {
		chartTitle += " · " + m.chart.WindowRange() + " · " + m.chart.SpanPercent() + " view"
	}
	// oscillator title
	oscTitle := "◇ " + components.OscModeName(m.osc.Mode())
	// bottom title
	bottomTitle := "▦ " + m.kpi.TabLabel() + " — " + components.BottomTabName(m.kpi.Tab()) + " (j/k/t/b rows)"

	side := box("⌥ parameters", m.sb.View(), sidebarW, chartH+oscH, m.zone == zSidebar)
	chart := box(chartTitle, m.chart.Legend()+"\n"+m.chart.View(), mainW, chartH, m.zone == zChart)
	osc := box(oscTitle, m.osc.View(), mainW, oscH, m.zone == zChart)
	bottom := box(bottomTitle, m.kpi.View(), m.W, bottomH, m.zone == zBottom)

	stack := lipgloss.JoinVertical(lipgloss.Top, chart, osc)
	head := lipgloss.JoinHorizontal(lipgloss.Top, side, " ", stack)
	return lipgloss.JoinVertical(lipgloss.Left, head, bottom)
}

// sideMinWidth guards tiny terminals.
const sideMinWidth = 40

// gridPane is workspace 2: sidebar + optimizer heatmap.
func (m *Model) gridPane(contentH int) string {
	mainW := m.W - sidebarW - 1
	if mainW < sideMinWidth {
		mainW = sideMinWidth
	}
	m.heat.SetWidth(mainW - 4)
	title := "◆ 2D parameter grid — " + engine.MetricLabel(m.heat.Metric()) + " (m cycles, ←↑↓→ cells, g re-run)"
	grid := m.heat.Caption()
	if grid != "" {
		grid += "\n\n"
	}
	grid += trimLines(m.heat.View(), contentH-8)
	side := box("⌥ parameters", m.sb.View(), sidebarW, contentH, m.zone == zSidebar)
	main := box(title, grid, mainW, contentH, m.zone == zMain)
	return lipgloss.JoinHorizontal(lipgloss.Top, side, " ", main)
}

// dataPane is workspace 3: sidebar + sqlite cache inventory.
func (m *Model) dataPane(contentH int) string {
	mainW := m.W - sidebarW - 1
	if mainW < sideMinWidth {
		mainW = sideMinWidth
	}
	title := "▤ market cache — press u to pull missing candles from Binance/mempool"
	body := m.dataBody(mainW)
	if lines := strings.Count(body, "\n") + 1; lines > contentH-4 {
		body = trimLines(body, contentH-4)
	}
	side := box("⌥ parameters", m.sb.View(), sidebarW, contentH, m.zone == zSidebar)
	main := box(title, body, mainW, contentH, m.zone == zMain)
	return lipgloss.JoinHorizontal(lipgloss.Top, side, " ", main)
}

// dataBody builds the inventory lines of workspace 3.
func (m *Model) dataBody(w int) string {
	var b strings.Builder
	dim := styleSubtext
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(Teal))
	fmt.Fprintf(&b, "symbol %s   ·   mirror: Binance klines (1500/page) + mempool.space\n",
		styleAccent.Render(m.fv.Get("symbol")))
	fmt.Fprintf(&b, "%s\n\n", pathStyle.Render("db "+m.paths.DatabasePath()))
	if m.statuses == nil {
		b.WriteString(dim.Render("inventory loading…"))
		return b.String()
	}
	head := fmt.Sprintf("%-6s %10s  %-10s %-10s", "tf", "candles", "first", "last")
	b.WriteString(headStyle.Render(head) + "\n")
	for _, st := range m.statuses {
		first, last := "-", "-"
		if st.Count > 0 && st.First > 0 {
			first, last = isoDate(st.First), isoDate(st.Last)
		}
		fmt.Fprintf(&b, "%-6s %10s  %-10s %-10s\n",
			st.Timeframe, commas(int(st.Count)), first, last)
	}
	fmt.Fprintf(&b, "\n%s\n", dim.Render("offline-safe: every run reads SQLite first and fetches only the missing delta"))
	if m.dbSize > 0 {
		fmt.Fprintf(&b, "sqlite size %s\n", humanSize(m.dbSize))
	}
	return b.String()
}

// headStyle highlights the data inventory header.
var headStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(Yellow)).Bold(true)

// syncSizes refreshes component geometry only when it changed.
func (m *Model) syncSizes(chartW, chartH, oscH, bottomH int) {
	if cur := m.sizes["chart"]; cur != [2]int{chartW, chartH} {
		m.chart.Resize(chartW-2, chartH-3)
		m.sizes["chart"] = [2]int{chartW, chartH}
	}
	if cur := m.sizes["osc"]; cur != [2]int{chartW, oscH} {
		m.osc.Resize(chartW-2, oscH-2)
		m.sizes["osc"] = [2]int{chartW, oscH}
	}
	if cur := m.sizes["kpi"]; cur != [2]int{chartW, bottomH} {
		m.kpi.Resize(m.W, bottomH)
		m.sizes["kpi"] = [2]int{chartW, bottomH}
	}
}

// box draws a titled, focus-aware bordered pane.
func box(title, content string, w, h int, active bool) string {
	clr := lipgloss.Color(Surface0)
	if active {
		clr = lipgloss.Color(Mauve)
	}
	b := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(clr).
		Width(w - 2).
		Height(h - 2)
	t := lipgloss.NewStyle().Foreground(lipgloss.Color(Yellow)).Bold(true).Render(" " + title)
	inner := t + "\n" + content
	if lines := strings.Count(inner, "\n") + 1; lines > h-2 {
		inner = trimLines(inner, h-2)
	}
	return b.Render(inner)
}

// trimLines clamps a rendered block to n rows.
func trimLines(s string, n int) string {
	if n < 0 {
		return ""
	}
	rows := strings.Split(s, "\n")
	if len(rows) <= n {
		return s
	}
	return strings.Join(rows[:n], "\n")
}

// helpOverlay renders the reference table centered.
func (m *Model) helpOverlay() string {
	var b strings.Builder
	b.WriteString(headStyle.Render("key bindings") + "  " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(Subtext)).Render("? closes") + "\n\n")
	for _, r := range KeyBindingRows {
		fmt.Fprintf(&b, "%-14s %-13s %s\n",
			headStyle.Render(r[0]),
			styleSubtext.Render(r[1]),
			r[2])
	}
	body := trimLines(b.String(), m.H-6)
	inner := box("pzz-bench — reference", body, maxI(m.W-20, 60), maxI(m.H-4, 20), true)
	return lipgloss.Place(m.W, m.H-2, lipgloss.Center, lipgloss.Center, inner)
}

// maxI returns the larger int.
func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// commas renders 12345 as "12,345".
func commas(n int) string {
	s := fmt.Sprintf("%d", n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}
