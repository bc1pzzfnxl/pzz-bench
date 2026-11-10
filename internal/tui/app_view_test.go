package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
	"github.com/bc1pzzfnxl/pzz-bench/internal/tui/components"
)

// newTestModel wires enough of the root model for rendering and key routing.
func newTestModel() *Model {
	fv := &components.FormValues{}
	fv.InstallDefaults(true)
	return &Model{
		W:       120,
		H:       40,
		ws:      wsStudio,
		zone:    zChart,
		toolTab: tabOptimizer,
		fv:      fv,
		sb:      components.NewSidebar(fv),
		chart:   components.NewChart(80, 20),
		osc:     components.NewOscillator(80, 8),
		kpi:     components.NewKPIPane(),
		heat:    components.NewHeatmap(),
		sizes:   map[string][2]int{},
		jobs:    make(chan tea.Msg, 1),
	}
}

func TestTitleBarShowsStatusAndWorkspace(t *testing.T) {
	m := &Model{ws: wsStudio, status: "run ok · 500 candles", isErr: false}
	out := m.titleBar(120)
	if !strings.Contains(out, "run ok · 500 candles") {
		t.Fatalf("status missing from title bar: %q", out)
	}
	if !strings.Contains(out, "[1] studio") {
		t.Fatalf("workspace missing from title bar: %q", out)
	}
	m.ws = wsTools
	if out := m.titleBar(120); !strings.Contains(out, "[2] tools") {
		t.Fatalf("tools workspace missing from title bar: %q", out)
	}
}

func TestTitleBarStatusIsRedOnError(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	m := &Model{status: "run failed", isErr: true}
	// SoftRed (#F38BA8) as a truecolor foreground sequence.
	if out := m.titleBar(120); !strings.Contains(out, "243;139;168") {
		t.Fatalf("error status is not red: %q", out)
	}
}

func TestTitleBarWidth(t *testing.T) {
	m := &Model{status: strings.Repeat("long status ", 20)}
	lipgloss.SetColorProfile(termenv.Ascii)
	if got := lipgloss.Width(m.titleBar(120)); got != 120 {
		t.Fatalf("title bar width = %d, want 120", got)
	}
	if got := lipgloss.Width(m.titleBar(20)); got > 20 {
		t.Fatalf("narrow title bar width = %d, want <= 20", got)
	}
}

func TestContextualHintsRespectFocusedZone(t *testing.T) {
	m := &Model{ws: wsStudio, zone: zSidebar}
	if hints := m.contextualHints(); strings.Contains(hints, "[1/2]") ||
		strings.Contains(hints, "[v] mode") {
		t.Fatalf("sidebar hints advertise keys the form owns: %q", hints)
	}
	if hints := m.contextualHints(); !strings.Contains(hints, "[Enter] next") {
		t.Fatalf("sidebar hints miss the form navigation key: %q", hints)
	}

	m.zone = zChart
	hints := m.contextualHints()
	if !strings.Contains(hints, "[v] mode") || !strings.Contains(hints, "[1/2]") {
		t.Fatalf("chart hints missing chart/workspace keys: %q", hints)
	}
	if strings.Contains(hints, "[[/]]") {
		t.Fatalf("chart hints advertise result-tab keys: %q", hints)
	}

	m.zone = zBottom
	hints = m.contextualHints()
	if !strings.Contains(hints, "[[/]] tabs") || !strings.Contains(hints, "[↑/↓] rows") {
		t.Fatalf("result hints missing table keys: %q", hints)
	}
	if strings.Contains(hints, "[v] mode") {
		t.Fatalf("result hints advertise chart keys: %q", hints)
	}

	m.ws = wsTools
	m.zone = zMain
	m.toolTab = tabOptimizer
	hints = m.contextualHints()
	if !strings.Contains(hints, "[g] grid") || !strings.Contains(hints, "[m] metric") {
		t.Fatalf("optimizer hints missing grid keys: %q", hints)
	}
	if strings.Contains(hints, "[u] sync") {
		t.Fatalf("optimizer hints advertise the cache key: %q", hints)
	}

	m.toolTab = tabData
	hints = m.contextualHints()
	if !strings.Contains(hints, "[u] sync") || strings.Contains(hints, "[g] grid") {
		t.Fatalf("cache hints wrong: %q", hints)
	}
}

func TestExportHintOnlyAfterAResult(t *testing.T) {
	m := &Model{ws: wsStudio, zone: zChart}
	if hints := m.contextualHints(); strings.Contains(hints, "[e] export") {
		t.Fatalf("export advertised before a run: %q", hints)
	}
	m.result = &engine.Result{}
	if hints := m.contextualHints(); !strings.Contains(hints, "[e] export") {
		t.Fatalf("export hint missing after a run: %q", hints)
	}
}

func TestHintBarUsesFullWidth(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := &Model{ws: wsStudio, zone: zChart}
	if got := lipgloss.Width(m.hintBar(120)); got != 120 {
		t.Fatalf("hint bar width = %d, want 120", got)
	}
	if got := lipgloss.Width(m.hintBar(40)); got != 40 {
		t.Fatalf("narrow hint bar width = %d, want 40", got)
	}
}

func TestBottomTitleUsesArrowKeys(t *testing.T) {
	m := newTestModel()
	out := m.studioPane(m.H - 2)
	if !strings.Contains(out, "(↑/↓ rows)") {
		t.Fatalf("bottom title does not advertise arrow navigation: %q", out)
	}
	if strings.Contains(out, "(j/k/t/b rows)") {
		t.Fatalf("bottom title still advertises the removed vim keys: %q", out)
	}
}

func TestBracketReachesSidebarBeforeToolTabs(t *testing.T) {
	m := newTestModel()
	m.ws, m.zone, m.toolTab, m.busy = wsTools, zSidebar, tabOptimizer, true
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	if m.toolTab != tabOptimizer {
		t.Fatalf("sidebar bracket switched tools sub-tab to %v", m.toolTab)
	}
}

func TestBracketSwitchesToolTabsOutsideSidebar(t *testing.T) {
	m := newTestModel()
	m.ws, m.zone, m.toolTab, m.busy = wsTools, zMain, tabOptimizer, true
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	if m.toolTab != tabData {
		t.Fatalf("']' did not open the cache tab: %v", m.toolTab)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	if m.toolTab != tabOptimizer {
		t.Fatalf("'[' did not return to the optimizer: %v", m.toolTab)
	}
}
