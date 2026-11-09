// Oscillator sub-chart: the active indicator under the main canvas, plus a
// custom block histogram for MACD (barchart cannot plot negative deltas).
package components

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
)

// OscMode selects what the oscillator pane shows.
type OscMode uint8

// Oscillator modes, resolved from the strategy's exported series.
const (
	OscNone  OscMode = iota // nothing to plot
	OscRSI                  // RSI 0..100 + 30/70 guides
	OscMACD                 // MACD histogram (custom canvas)
	OscRatio                // Close / trend-SMA ratio + 1.0 / 2.0 guides
	OscLines                // fast/slow MA overlays
)

// OscModeName is the display label of the oscillator mode.
func OscModeName(m OscMode) string {
	switch m {
	case OscRSI:
		return "RSI"
	case OscMACD:
		return "MACD histogram"
	case OscRatio:
		return "price / trend SMA ratio"
	case OscLines:
		return "MA fast / slow"
	default:
		return "no indicator"
	}
}

// ResolveOscMode picks the oscillator mode from a strategy's exported series.
func ResolveOscMode(series map[string][]float64) OscMode {
	has := func(k string) bool { s, ok := series[k]; return ok && len(s) > 0 }
	switch {
	case has("rsi"):
		return OscRSI
	case has("macd"):
		return OscMACD
	case has("ratio"):
		return OscRatio
	case has("fast") && has("slow"):
		return OscLines
	default:
		return OscNone
	}
}

// oscGuide is a horizontal reference line plotted as a constant dataset.
type oscGuide struct {
	name string
	v    float64
	clr  string
}

// OscData carries the aligned indicator series of one run.
type OscData struct {
	Times  []int64
	Series map[string][]float64
}

// oscillator catalog colors.
const (
	guideColor = "#6C7086" // dim guides
	bullColor  = "#A6E3A1" // mint bars above zero
	bearColor  = "#F38BA8" // red bars below zero
	pivotColor = "#45475A" // zero line
	oscLine1   = "#89B4FA"
	oscLine2   = "#F7931A"
)

// Oscillator renders one of four sub-charts; line modes share a single
// timeserieslinechart instance, the MACD histogram a custom canvas.
type Oscillator struct {
	line   timeserieslinechart.Model
	bars   canvas.Model
	mode   OscMode
	times  []int64
	series map[string][]float64
	guides []oscGuide
}

// NewOscillator builds both sub-renderers at the requested size.
func NewOscillator(w, h int) *Oscillator {
	o := &Oscillator{bars: canvas.New(w, h)}
	o.line = timeserieslinechart.New(w, h, timeserieslinechart.WithXYSteps(8, 2))
	o.line.XLabelFormatter = func(_ int, v float64) string {
		return compactDateSec(int64(v))
	}
	return o
}

// SetOsc switches the pane content and redraws.
func (o *Oscillator) SetOsc(mode OscMode, data OscData) {
	o.mode = mode
	o.times = data.Times
	o.series = data.Series
	o.buildGuides()
	o.render()
}

// Mode returns the active oscillator mode.
func (o *Oscillator) Mode() OscMode { return o.mode }

// SetWindow synchronizes the X view range with the main chart after pan/zoom.
func (o *Oscillator) SetWindow(winMin, winMax time.Time) {
	if o.mode == OscMACD {
		o.drawHistogram()
		return
	}
	o.line.SetViewTimeRange(winMin, winMax)
	o.line.DrawBrailleAll()
}

// Resize adapts both sub-renderers when the terminal size changes.
func (o *Oscillator) Resize(w, h int) {
	o.bars.Resize(w, h)
	o.line.Resize(w, h)
	o.render()
}

// buildGuides prepares the horizontal reference lines of the active mode.
func (o *Oscillator) buildGuides() {
	switch o.mode {
	case OscRSI:
		o.guides = []oscGuide{
			{name: "t70", v: 70, clr: guideColor},
			{name: "t30", v: 30, clr: guideColor},
		}
	case OscRatio:
		o.guides = []oscGuide{
			{name: "g2", v: 2.0, clr: guideColor},
			{name: "g1", v: 1.0, clr: bearColor},
		}
	default:
		o.guides = nil
	}
}

// applyStyles installs per-dataset colors: mains cycle blue/orange, guides
// keep archive colors.
func (o *Oscillator) applyStyles(main []string) {
	guideClr := func(name string) (string, bool) {
		for _, g := range o.guides {
			if g.name == name {
				return g.clr, true
			}
		}
		return "", false
	}
	names := append(append([]string{}, main...), guideNames(o.guides)...)
	for i, n := range names {
		clr, isGuide := guideClr(n)
		if !isGuide {
			clr = []string{oscLine1, oscLine2, bullColor, bearColor}[i%4]
		}
		o.line.SetDataSetStyle(n, lipgloss.NewStyle().Foreground(lipgloss.Color(clr)))
	}
}

// yFormatter swaps the Y tick rendering for the current mode.
func (o *Oscillator) yFormatter() {
	switch o.mode {
	case OscRSI:
		o.line.YLabelFormatter = func(_ int, v float64) string { return fmtNum(v, 0) }
	case OscRatio:
		o.line.YLabelFormatter = func(_ int, v float64) string { return fmtNum(v, 1) }
	default: // OscLines: price-like values
		o.line.YLabelFormatter = func(_ int, v float64) string {
			if v >= 1e6 {
				return fmtNum(v/1e6, 1) + "M"
			}
			if v >= 1e3 {
				return fmtNum(v/1e3, 0) + "k"
			}
			return fmtNum(v, 0)
		}
	}
}

// render rebuilds the active sub-renderer from the stored data.
func (o *Oscillator) render() {
	if len(o.times) < 2 {
		return
	}
	if o.mode == OscMACD {
		o.drawHistogram()
		return
	}
	if o.mode == OscNone {
		return
	}

	o.line.ClearAllData()
	o.line.SetTimeRange(time.Unix(o.times[0]/1000, 0).UTC(),
		time.Unix(o.times[len(o.times)-1]/1000, 0).UTC())
	o.yFormatter()

	// resolve Y bounds + main dataset names per mode
	var lo, hi float64
	var main []string
	switch o.mode {
	case OscRSI:
		lo, hi = 0, 100
		main = []string{"rsi"}
	case OscRatio:
		_, hi = seriesMinMax(o.series["ratio"])
		ceil := 2.5
		for ceil < hi {
			ceil += 0.5
		}
		lo, hi = 0, ceil*1.1
		main = []string{"ratio"}
	case OscLines:
		fLo, fHi := seriesMinMax(o.series["fast"])
		sLo, sHi := seriesMinMax(o.series["slow"])
		lo, hi = minF(fLo, sLo), maxF(fHi, sHi)
		pad := (hi - lo) * 0.08
		lo, hi = lo-pad, hi+pad
		main = []string{"fast", "slow"}
	}
	o.line.SetYRange(lo, hi)

	push := func(name string, v []float64) {
		for i, t := range o.times {
			if i < len(v) && !isNaN(v[i]) {
				o.line.PushDataSet(name, timeserieslinechart.TimePoint{
					Time:  time.Unix(t/1000, 0).UTC(),
					Value: v[i],
				})
			}
		}
	}
	for _, n := range main {
		push(n, o.series[n])
	}
	for _, g := range o.guides {
		for _, t := range o.times {
			o.line.PushDataSet(g.name, timeserieslinechart.TimePoint{
				Time:  time.Unix(t/1000, 0).UTC(),
				Value: g.v,
			})
		}
	}
	o.applyStyles(main)
	o.line.DrawBrailleAll()
}

// drawHistogram paints the MACD histogram with Unicode block glyphs: mint
// bars above zero, red bars below, a dotted zero line in the middle. Buckets
// are averaged when candles outnumber canvas columns.
func (o *Oscillator) drawHistogram() {
	w, h := o.bars.Width(), o.bars.Height()
	o.bars.Clear()
	if w <= 0 || h <= 0 {
		return
	}
	style := lipgloss.NewStyle()
	mint := style.Foreground(lipgloss.Color(bullColor)).Bold(true)
	red := style.Foreground(lipgloss.Color(bearColor)).Bold(true)
	pivot := style.Foreground(lipgloss.Color(pivotColor))

	hist := o.series["hist"]
	if len(hist) == 0 || len(o.times) < 2 {
		return
	}
	pivotY := (h - 1) / 2
	maxMag := seriesMaxAbs(hist)
	if maxMag <= 0 {
		maxMag = 1
	}
	half := (h - 1) / 2
	if half < 1 {
		return
	}

	colCount := w
	if len(hist) < w {
		colCount = len(hist)
	}
	for x := 0; x < colCount; x++ {
		a := x * len(hist) / colCount
		b := (x + 1) * len(hist) / colCount
		if b <= a {
			b = a + 1
		}
		if b > len(hist) {
			b = len(hist)
		}
		var sum float64
		var n int
		for i := a; i < b; i++ {
			if !isNaN(hist[i]) {
				sum += hist[i]
				n++
			}
		}
		if n == 0 {
			continue
		}
		v := sum / float64(n)
		rows := int(vAbs(v) / maxMag * float64(half))
		if rows < 1 {
			rows = 1
		}
		if rows > half {
			rows = half
		}
		st := mint
		dir := -1
		if v < 0 {
			dir = 1
			st = red
		}
		for k := 1; k <= rows; k++ {
			y := pivotY + dir*k
			if y >= 0 && y < h {
				o.bars.SetRuneWithStyle(canvas.Point{X: x, Y: y}, '█', st)
			}
		}
	}
	// zero baseline over the bars region
	for x := 0; x < w; x++ {
		o.bars.SetRuneWithStyle(canvas.Point{X: x, Y: pivotY}, '·', pivot)
	}
}

// View renders the active sub-render (histogram canvas or line chart).
func (o *Oscillator) View() string {
	if o.mode == OscMACD {
		return o.bars.View()
	}
	if o.mode == OscNone {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(textDim)).
			Render("no indicator exported by this strategy")
	}
	return o.line.View()
}

// guideNames lists the guide dataset names (style assignment).
func guideNames(g []oscGuide) []string {
	out := make([]string, 0, len(g))
	for _, x := range g {
		out = append(out, x.name)
	}
	return out
}

// fmtNum renders a float with fixed precision, NaN-safe.
func fmtNum(v float64, digits int) string {
	if isNaN(v) {
		return "n/a"
	}
	return fmt.Sprintf("%.*f", digits, v)
}

func vAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// --- generic little helpers ------------------------------------------------

func seriesMinMax(v []float64) (float64, float64) {
	lo, hi := 0.0, 1.0
	first := true
	for _, x := range v {
		if isNaN(x) {
			continue
		}
		if first {
			lo, hi = x, x
			first = false
			continue
		}
		lo = minF(lo, x)
		hi = maxF(hi, x)
	}
	return lo, hi
}

func seriesMaxAbs(v []float64) float64 {
	max := 0.0
	for _, x := range v {
		if isNaN(x) {
			continue
		}
		if a := vAbs(x); a > max {
			max = a
		}
	}
	return max
}
