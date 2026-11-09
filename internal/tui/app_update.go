// Event handling of the root model: message routing, focus zones and the
// PRD 6.4 keymap.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
	"github.com/bc1pzzfnxl/pzz-bench/internal/tui/components"
)

// Update handles every bubbletea message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch t := msg.(type) {
	case tea.WindowSizeMsg:
		m.W, m.H = t.Width, t.Height
		return m, nil

	case runDoneMsg:
		m.busy = false
		if t.err != nil {
			m.status, m.isErr = "run failed: "+t.err.Error(), true
			return m, nil
		}
		m.applyResult(t.res)
		frm := isoDate(t.res.Times[0])
		to := isoDate(t.res.Times[len(t.res.Times)-1])
		alpha := ""
		if t.res.Report.SatsAlpha >= 0 {
			alpha = "alpha +"
		} else {
			alpha = "alpha "
		}
		m.status = fmt.Sprintf("%s ok · %d candles · %s→%s · sats %s%.2f%%",
			t.res.StrategyName, t.res.Report.Candles, frm, to,
			alpha, t.res.Report.SatsAlpha)
		return m, m.autoGridCmd()

	case gridDoneMsg:
		m.busy = false
		if t.err != nil {
			m.status, m.isErr = "sweep failed: "+t.err.Error(), true
			return m, nil
		}
		m.heat.SetAxes(m.lastTask.X, m.lastTask.Y)
		m.heat.SetResult(t.res)
		m.status = fmt.Sprintf("grid done: %d runs, %d skipped, %s · best %s = %.2f",
			t.res.RanCount, t.res.SkippedCount, t.res.Elapsed,
			engine.MetricLabel(t.res.Metric), t.res.BestVal)
		return m, nil

	case progressMsg:
		m.status, m.isErr = string(t), false
		return m, m.waitJob()

	case statusMsg:
		m.status, m.isErr = t.text, t.isErr
		if t.statuses != nil {
			m.statuses = t.statuses
		}
		if t.dbPath != "" {
			m.dbPath = t.dbPath
		}
		if t.dbSize > 0 {
			m.dbSize = t.dbSize
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(t)
	}
	// forward non-handled messages (huh cmd results, window size) to the
	// sidebar when it owns the focus so the form state machine advances.
	if m.zone == zSidebar {
		cmd, ev := m.sb.Update(msg)
		switch ev {
		case components.SidebarRun:
			return m, m.queueRun()
		case components.SidebarStrategy:
			m.status = "strategy changed to " + m.fv.Get("strategy") + " · press r to apply"
		}
		return m, cmd
	}
	return m, nil
}

// autoGridCmd launches the sweep when landing on workspace 2 without data.
func (m *Model) autoGridCmd() tea.Cmd {
	if m.ws == wsTools && m.toolTab == 0 && !m.heat.HasData() && !m.busy {
		return m.queueGrid()
	}
	return nil
}

// cycleZone advances the focus ring (PRD: sidebar -> chart -> bottom).
func (m *Model) cycleZone() {
	switch m.ws {
	case wsStudio:
		switch m.zone {
		case zSidebar:
			m.zone = zChart
		case zChart:
			m.zone = zBottom
		default:
			m.zone = zSidebar
		}
	default:
		if m.zone == zSidebar {
			m.zone = zMain
		} else {
			m.zone = zSidebar
		}
	}
}

// enterWorkspace switches panes and kicks the workspace-specific jobs.
func (m *Model) enterWorkspace(w workspace) tea.Cmd {
	m.ws = w
	switch w {
	case wsStudio:
		if m.zone == zMain {
			m.zone = zChart
		}
	case wsTools:
		if m.zone != zSidebar {
			m.zone = zMain
		}
	}
	m.kpi.ResetTabs()
	switch {
	case w == wsTools && m.toolTab == 0 && !m.heat.HasData():
		return m.queueGrid()
	case w == wsTools && m.toolTab == 1 && !m.busy:
		return m.queueRefreshStatuses()
	}
	return nil
}

// handleKey implements the PRD 6.4 keymap with zone-scoped routing.
func (m *Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	ks := k.String()

	switch ks {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help = !m.help
		return m, nil
	case "esc":
		if m.help {
			m.help = false
		}
		return m, nil
	}
	if m.help {
		return m, nil // swallow keys while the overlay is open
	}

	// global shortcuts
	switch ks {
	case "tab", "shift+tab":
		m.cycleZone()
		return m, nil
	case "r", "R", "ctrl+r":
		return m, m.queueRun()
	case "s":
		m.savePreset()
		return m, nil
	case "e":
		m.exportReport()
		return m, nil
	case "g":
		if m.ws == wsTools && m.toolTab == 0 && !m.busy {
			return m, m.queueGrid()
		}
	case "m":
		if m.ws == wsTools && m.toolTab == 0 && !m.busy {
			return m, m.cycleMetric()
		}
	case "u":
		if m.ws == wsTools && m.toolTab == 1 && !m.busy {
			m.busy = true
			m.status, m.isErr = "syncing cache from providers…", false
			return m, m.queueSync()
		}
	}

	// digits switch workspaces only when the form does not eat them
	if m.zone != zSidebar {
		switch ks {
		case "1", "2":
			return m, m.enterWorkspace(workspace(ks[0] - '1'))
		}
	}

	// tools sub-tabs: optimizer <-> data manager
	if m.ws == wsTools {
		switch ks {
		case "[", "]":
			if m.toolTab == 0 {
				m.toolTab = 1
			} else {
				m.toolTab = 0
			}
			m.kpi.ResetTabs()
			if m.toolTab == 0 && !m.heat.HasData() && !m.busy {
				return m, m.queueGrid()
			}
			if m.toolTab == 1 && !m.busy {
				m.busy = true
				m.status, m.isErr = "loading cache inventory…", false
				return m, m.queueRefreshStatuses()
			}
			return m, nil
		}
	}
	if m.zone == zSidebar {
		cmd, ev := m.sb.Update(k)
		switch ev {
		case components.SidebarRun:
			return m, m.queueRun()
		case components.SidebarStrategy:
			m.status = "strategy changed to " + m.fv.Get("strategy") + " · press r to apply"
		}
		return m, cmd
	}

	// bottom pane: tabs + row navigation (arrows only)
	if m.ws == wsStudio && (m.zone == zBottom || ks == "[" || ks == "]") {
		switch ks {
		case "[", "]":
			m.kpi.CycleTabs()
			return m, nil
		case "up":
			m.kpi.MoveUp(1)
			return m, nil
		case "down":
			m.kpi.MoveDown(1)
			return m, nil
		case "pgup":
			m.kpi.PageUp()
			return m, nil
		case "pgdown":
			m.kpi.PageDown()
			return m, nil
		case "home":
			m.kpi.GotoTop()
			return m, nil
		case "end":
			m.kpi.GotoBottom()
			return m, nil
		}
	}

	// chart controls (workspace 1, chart focus)
	if m.ws == wsStudio && m.zone == zChart {
		switch ks {
		case "v":
			m.chart.CycleMode()
		case "h", "left":
			m.chart.PanLeft()
		case "l", "right":
			m.chart.PanRight()
		case "+", "=", "k":
			m.chart.ZoomIn()
		case "-", "j":
			m.chart.ZoomOut()
		case "0":
			m.chart.ResetZoom()
		default:
			return m, nil
		}
		m.syncOscWindow()
		return m, nil
	}

	// heatmap navigation (workspace tools, grid tab, main focus)
	if m.ws == wsTools && m.toolTab == 0 && m.zone == zMain {
		switch ks {
		case "up":
			m.heat.MoveUp()
		case "down":
			m.heat.MoveDown()
		case "left":
			m.heat.MoveLeft()
		case "right":
			m.heat.MoveRight()
		}
		return m, nil
	}

	return m, nil
}

// syncOscWindow mirrors the chart's visible window into the oscillator.
func (m *Model) syncOscWindow() {
	if !m.chart.HasData() {
		return
	}
	a, b := m.chart.WindowTimes()
	m.osc.SetWindow(a, b)
}
