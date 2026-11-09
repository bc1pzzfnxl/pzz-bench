// Main chart pane: braille dual-equity / sats / drawdown canvas.
package components

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

// ChartMode is one render mode of the top chart, cycled with `v`.
type ChartMode uint8

// Chart modes (PRD 6.3).
const (
	ModeFiat ChartMode = iota // strat vs baseline equity + injected cash
	ModeSats                  // sats held by strategy vs baseline
	ModeDrawdown              // underwater depth from all-time highs
)

// ChartModeName returns the display label of a chart mode.
func ChartModeName(m ChartMode) string {
	switch m {
	case ModeSats:
		return "sats stack"
	case ModeDrawdown:
		return "drawdown"
	default:
		return "fiat equity"
	}
}

// dataset names (PRD contract).
const (
	dsStrat = "strat"
	dsBase  = "base"
	dsInj   = "inj"
)

// ChartData is the immutable series bundle of one benchmark run. Aligned
// slices share the Times index (candle open times, ms).
type ChartData struct {
	Times       []int64
	StratEquity []float64
	BaseEquity  []float64
	StratBTC    []float64
	BaseBTC     []float64
	Injected    []float64 // cumulative injected capital into the strategy
	StratName   string
	BaseName    string
	SatsAlpha   float64 // report.SatsAlpha (>=0 beats DCA in BTC)
	EquityAlpha float64 // report.EquityAlpha
	MDDStrat    float64 // negative fractions
	MDDBase     float64
}

// Catppuccin curve colors.
const (
	injColor  = "#9399B2" // faint injected-cash overlay
	baseColor = "#F7931A" // bitcoin orange baseline
	stratWin  = "#A6E3A1" // mint: strategy leading the baseline
	stratLos  = "#89B4FA" // blue: strategy trailing the baseline
	textDim   = "#A6ADC8"
	txtColor  = "#CDD6F4"
)

// Chart wraps timeserieslinechart with pzz-bench modes, pan/zoom state and
// brand colors. The visible window is fully managed here; the app calls the
// exported navigation methods from its key handler.
type Chart struct {
	ts   timeserieslinechart.Model
	data ChartData
	mode ChartMode

	fullMin, fullMax time.Time // whole run window (world range)
	winMin, winMax   time.Time // visible window (view range)

	stratColor string // mint when the active curve wins, blue otherwise
}

// NewChart builds an empty chart with the given terminal size.
func NewChart(w, h int) *Chart {
	c := &Chart{stratColor: stratWin}
	c.ts = timeserieslinechart.New(w, h,
		timeserieslinechart.WithXYSteps(8, 2))
	c.ts.XLabelFormatter = func(_ int, v float64) string {
		return compactDateSec(int64(v))
	}
	c.applyYFormatter()
	c.applyColors()
	return c
}
func compactDateSec(unixSec int64) string {
	return time.Unix(unixSec, 0).UTC().Format("02 Jan 06")
}

func compactDateMs(ms int64) string {
	return time.Unix(ms/1000, 0).UTC().Format("02 Jan 06")
}

// applyColors pushes per-dataset styles for the current evaluation.
func (c *Chart) applyColors() {
	set := func(name, color string, bold bool) {
		s := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		if bold {
			s = s.Bold(true)
		}
		c.ts.SetDataSetStyle(name, s)
	}
	set(dsBase, baseColor, false)
	set(dsInj, injColor, false)
	set(dsStrat, c.stratColor, true)
}

// applyYFormatter swaps the Y tick rendering for the current mode.
func (c *Chart) applyYFormatter() {
	switch c.mode {
	case ModeSats:
		c.ts.YLabelFormatter = func(_ int, v float64) string {
			if v >= 1e6 {
				return fmt.Sprintf("%.0fM", v/1e6)
			}
			if v >= 1e3 {
				return fmt.Sprintf("%.0fk", v/1e3)
			}
			return fmt.Sprintf("%.0f", v)
		}
	case ModeDrawdown:
		// depth is plotted positive; ticks render as negative percents
		c.ts.YLabelFormatter = func(_ int, v float64) string {
			if v < 0.05 {
				return "0%"
			}
			return fmt.Sprintf("-%.0f%%", v)
		}
	default:
		c.ts.YLabelFormatter = func(_ int, v float64) string {
			if v >= 1e6 {
				return fmt.Sprintf("$%.1fM", v/1e6)
			}
			if v >= 1e3 {
				return fmt.Sprintf("$%.1fk", v/1e3)
			}
			return fmt.Sprintf("$%.0f", v)
		}
	}
}

// SetData installs a benchmark result and resets the view to 100%.
func (c *Chart) SetData(d ChartData) *Chart {
	c.data = d
	n := len(d.Times)
	if n < 2 {
		return c
	}
	c.fullMin = time.Unix(d.Times[0]/1000, 0).UTC()
	c.fullMax = time.Unix(d.Times[n-1]/1000, 0).UTC()
	c.winMin, c.winMax = c.fullMin, c.fullMax
	c.resetEvalColor()
	c.refresh()
	return c
}

// resetEvalColor picks mint/blue for the strategy curve depending on the
// alpha of the current mode.
func (c *Chart) resetEvalColor() {
	good := true
	switch c.mode {
	case ModeSats:
		good = c.data.SatsAlpha >= 0
	case ModeDrawdown:
		good = c.data.MDDStrat >= c.data.MDDBase // smaller |MDD| wins
	default:
		good = c.data.EquityAlpha >= 0
	}
	if good {
		c.stratColor = stratWin
	} else {
		c.stratColor = stratLos
	}
	c.applyColors()
}

// CycleMode advances Fiat -> Sats -> Drawdown -> Fiat (`v`).
func (c *Chart) CycleMode() {
	c.mode = (c.mode + 1) % 3
	c.resetEvalColor()
	c.applyYFormatter()
	c.refresh()
}

// Mode returns the current chart mode.
func (c *Chart) Mode() ChartMode { return c.mode }

// SetMode forces a specific mode (restoring a workspace).
func (c *Chart) SetMode(m ChartMode) {
	if m > ModeDrawdown {
		return
	}
	c.mode = m
	c.resetEvalColor()
	c.applyYFormatter()
	c.refresh()
}

// Legend renders the one-line legend shown above the chart canvas.
func (c *Chart) Legend() string {
	dot := func(color, label string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render("● ") +
			lipgloss.NewStyle().Foreground(lipgloss.Color(txtColor)).Render(label)
	}
	dim := func(label string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(injColor)).Render("● ") +
			lipgloss.NewStyle().Foreground(lipgloss.Color(textDim)).Render(label)
	}
	switch c.mode {
	case ModeSats:
		return dot(c.stratColor, c.data.StratName) + "  " + dot(baseColor, c.data.BaseName)
	case ModeDrawdown:
		return dot(c.stratColor, c.data.StratName+" dd") + "  " + dot(baseColor, c.data.BaseName+" dd")
	default:
		return dot(c.stratColor, c.data.StratName) + "  " + dot(baseColor, c.data.BaseName) + "  " + dim("cash injected")
	}
}

// series resolves the plotted curves for the current mode.
func (c *Chart) series() (strat, base, inj []float64) {
	n := len(c.data.Times)
	clip := func(v []float64) []float64 {
		if len(v) > n {
			return v[:n]
		}
		return v
	}
	switch c.mode {
	case ModeSats:
		return mulConst(clip(c.data.StratBTC), engine.SatsPerBTC),
			mulConst(clip(c.data.BaseBTC), engine.SatsPerBTC), nil
	case ModeDrawdown:
		return depthOf(clip(c.data.StratEquity)), depthOf(clip(c.data.BaseEquity)), nil
	default:
		return clip(c.data.StratEquity), clip(c.data.BaseEquity), clip(c.data.Injected)
	}
}

// mulConst scales every element.
func mulConst(v []float64, k float64) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x * k
	}
	return out
}

// depthOf returns percent drawdown depth as positive values (0 == at high).
// A single record-high sequence is tracked per curve.
func depthOf(v []float64) []float64 {
	n := len(v)
	out := make([]float64, n)
	var cur float64
	if n > 0 && !isNaN(v[0]) {
		cur = v[0]
	}
	for i, x := range v {
		if isNaN(x) {
			out[i] = 0
			continue
		}
		if x > cur {
			cur = x
		}
		if cur > 0 {
			out[i] = (cur - x) / cur * 100
		}
	}
	return out
}

func isNaN(v float64) bool { return v != v }

// worldYRange computes min/max over the full run for the current mode.
func (c *Chart) worldYRange() (float64, float64) {
	strat, base, inj := c.series()
	lo, hi := minMaxChain(strat, base, inj)
	pad := (hi - lo) * 0.06
	if pad <= 0 {
		pad = 1
	}
	return lo - pad, hi + pad
}

func minMaxChain(curves ...[]float64) (float64, float64) {
	lo, hi := 0.0, 0.0
	first := true
	for _, v := range curves {
		for _, x := range v {
			if isNaN(x) {
				continue
			}
			if first {
				lo, hi = x, x
				first = false
				continue
			}
			if x < lo {
				lo = x
			}
			if x > hi {
				hi = x
			}
		}
	}
	if first {
		return 0, 1
	}
	return lo, hi
}

// visYRange computes min/max over the visible window (padded).
func (c *Chart) visYRange(strat, base, inj []float64) (float64, float64) {
	loMs, hiMs := c.winMin.UnixMilli(), c.winMax.UnixMilli()
	part := func(v []float64) []float64 {
		out := make([]float64, 0, len(v))
		for i, t := range c.data.Times {
			if i < len(v) && t >= loMs && t <= hiMs {
				out = append(out, v[i])
			}
		}
		return out
	}
	lo, hi := minMaxChain(part(strat), part(base), part(inj))
	pad := (hi - lo) * 0.08
	if pad <= 0 {
		pad = 1
	}
	return lo - pad, hi + pad
}

// refresh re-normalizes and redraws everything from raw data.
func (c *Chart) refresh() {
	if len(c.data.Times) < 2 {
		return
	}
	strat, base, inj := c.series()
	c.ts.ClearAllData()
	c.ts.SetTimeRange(c.fullMin, c.fullMax)
	yLo, yHi := c.worldYRange()
	c.ts.SetYRange(yLo, yHi)

	push := func(name string, v []float64) {
		if len(v) == 0 {
			return
		}
		for i, t := range c.data.Times {
			if i >= len(v) {
				break
			}
			x := v[i]
			if isNaN(x) {
				continue
			}
			c.ts.PushDataSet(name, timeserieslinechart.TimePoint{
				Time:  time.Unix(t/1000, 0).UTC(),
				Value: x,
			})
		}
	}
	push(dsStrat, strat)
	push(dsBase, base)
	push(dsInj, inj)

	c.ts.SetViewTimeRange(c.winMin, c.winMax)
	vLo, vHi := c.visYRange(strat, base, inj)
	c.ts.SetViewYRange(vLo, vHi)
	c.ts.DrawAll()
}

// --- navigation ------------------------------------------------------------

// panFrac is the fraction of the window shifted per pan press.
const panFrac = 0.15

// zoomFrac is the fraction of the window removed/added per zoom press.
const zoomFrac = 0.2

// ResetZoom restores the 100% full backtest range (`0`).
func (c *Chart) ResetZoom() {
	c.winMin, c.winMax = c.fullMin, c.fullMax
	c.refresh()
}

// PanRight shifts the visible window into the future (`l` / →).
func (c *Chart) PanRight() {
	span := c.winMax.Sub(c.winMin)
	shift := time.Duration(float64(span) * panFrac)
	if c.fullMax.Sub(c.winMax) <= shift {
		return
	}
	c.winMin = c.winMin.Add(shift)
	c.winMax = c.winMax.Add(shift)
	c.clampWindow()
	c.refresh()
}

// PanLeft shifts the visible window into the past (`h` / ←).
func (c *Chart) PanLeft() {
	span := c.winMax.Sub(c.winMin)
	shift := time.Duration(float64(span) * panFrac)
	if c.winMin.Sub(c.fullMin) <= shift {
		return
	}
	c.winMin = c.winMin.Add(-shift)
	c.winMax = c.winMax.Add(-shift)
	c.clampWindow()
	c.refresh()
}

// ZoomIn narrows the visible window (`+` / k).
func (c *Chart) ZoomIn() {
	span := c.winMax.Sub(c.winMin)
	if span <= c.minZoomSpan() {
		return
	}
	mid := c.winMin.Add(span / 2)
	half := time.Duration(float64(span) * (1 - zoomFrac) / 2)
	c.winMin = mid.Add(-half)
	c.winMax = mid.Add(half)
	c.clampWindow()
	c.refresh()
}

// ZoomOut widens the visible window (`-` / j).
func (c *Chart) ZoomOut() {
	span := c.winMax.Sub(c.winMin)
	mid := c.winMin.Add(span / 2)
	half := time.Duration(float64(span) * (1 + zoomFrac) / 2)
	if mid.Add(-half).Before(c.fullMin) && mid.Add(half).After(c.fullMax) {
		return
	}
	c.winMin = mid.Add(-half)
	c.winMax = mid.Add(half)
	c.clampWindow()
	c.refresh()
}

// minZoomSpan returns the smallest sensible window (about 8 candles).
func (c *Chart) minZoomSpan() time.Duration {
	t := c.data.Times
	if len(t) < 2 {
		return 8 * time.Hour
	}
	spacing := t[1] - t[0]
	if spacing <= 0 {
		return 8 * time.Hour
	}
	return time.Duration(spacing*8) * time.Millisecond
}

// clampWindow pins the window inside the full range.
func (c *Chart) clampWindow() {
	if c.winMin.Before(c.fullMin) {
		delta := c.fullMin.Sub(c.winMin)
		c.winMin = c.winMin.Add(delta)
		c.winMax = c.winMax.Add(delta)
	}
	if c.winMax.After(c.fullMax) {
		delta := c.winMax.Sub(c.fullMax)
		c.winMin = c.winMin.Add(-delta)
		c.winMax = c.winMax.Add(-delta)
	}
	if c.winMin.Before(c.fullMin) {
		c.winMin = c.fullMin
	}
}

// Resize adapts the canvas size when the terminal changes.
func (c *Chart) Resize(w, h int) {
	c.ts.Resize(w, h)
	c.refresh()
}

// View renders the chart canvas (axis labels included).
func (c *Chart) View() string {
	if len(c.data.Times) < 2 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(textDim)).
			Render("press r to run a benchmark")
	}
	return c.ts.View()
}

// WindowRange describes the visible time slice (status bar).
func (c *Chart) WindowRange() string {
	if len(c.data.Times) < 2 {
		return ""
	}
	return compactDateMs(c.winMin.UnixMilli()) + " → " + compactDateMs(c.winMax.UnixMilli())
}

// WindowTimes exposes the visible window edges (for oscillator sync).
func (c *Chart) WindowTimes() (time.Time, time.Time) {
	return c.winMin, c.winMax
}

// HasData reports whether a benchmark result is loaded.
func (c *Chart) HasData() bool { return len(c.data.Times) >= 2 }

// SpanPercent renders the share of the full window currently displayed.
func (c *Chart) SpanPercent() string {
	if len(c.data.Times) < 2 {
		return ""
	}
	full := float64(c.fullMax.Sub(c.fullMin).Milliseconds())
	w := float64(c.winMax.Sub(c.winMin).Milliseconds())
	if full <= 0 {
		return ""
	}
	return fmt.Sprintf("%.0f%%", w/full*100)
}
