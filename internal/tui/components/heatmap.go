// Workspace 2 heatmap: 2D parameter grid rendered with ANSI true colors.
package components

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

// Heatmap renders the optimizer grid with a red -> orange -> green ramp, a
// cursor (arrow keys) and a star on the best cell.
type Heatmap struct {
	res engine.GridResult
	axX engine.GridAxis
	axY engine.GridAxis
	cx  int
	cy  int
	w   int // usable width, minus the y-axis label column
}

// NewHeatmap builds an empty heatmap.
func NewHeatmap() *Heatmap { return &Heatmap{} }

// SetResult installs a completed sweep and resets the cursor to the best cell.
func (h *Heatmap) SetResult(res engine.GridResult) {
	h.res = res
	h.cx, h.cy = res.Best.X, res.Best.Y
	h.clampCursor()
}

// SetAxes installs the display metadata used to format axis labels.
func (h *Heatmap) SetAxes(x, y engine.GridAxis) {
	if x.Name != "" {
		h.axX = x
	}
	if y.Name != "" {
		h.axY = y
	}
}

// HasData reports whether a sweep result is loaded.
func (h *Heatmap) HasData() bool { return len(h.res.Cells) > 0 }

// Metric returns the metric key the heatmap is colored by.
func (h *Heatmap) Metric() engine.MetricKey { return h.res.Metric }

// Cursor returns the current cell coordinates.
func (h *Heatmap) Cursor() engine.GridPoint { return engine.GridPoint{X: h.cx, Y: h.cy} }

// CursorValue returns the metric value under the cursor (NaN when empty).
func (h *Heatmap) CursorValue() float64 {
	h.clampCursor()
	if len(h.res.Cells) == 0 || len(h.res.Cells[h.cy]) == 0 {
		return math.NaN()
	}
	return h.res.Cells[h.cy][h.cx]
}

// MoveLeft / MoveRight / MoveUp / MoveDown navigate the grid.
func (h *Heatmap) MoveLeft()  { h.cx--; h.clampCursor() }
func (h *Heatmap) MoveRight() { h.cx++; h.clampCursor() }
func (h *Heatmap) MoveUp()    { h.cy--; h.clampCursor() }
func (h *Heatmap) MoveDown()  { h.cy++; h.clampCursor() }

// clampCursor pins the cursor inside the grid.
func (h *Heatmap) clampCursor() {
	if len(h.res.Cells) == 0 {
		h.cx, h.cy = 0, 0
		return
	}
	h.cx = clampInt(h.cx, 0, len(h.res.Cells[0])-1)
	h.cy = clampInt(h.cy, 0, len(h.res.Cells)-1)
}

// nCols / nRows proxy the grid shape (0 when empty).
func (h *Heatmap) nCols() int {
	if len(h.res.Cells) == 0 {
		return 0
	}
	return len(h.res.Cells[0])
}

func (h *Heatmap) nRows() int { return len(h.res.Cells) }

// clampInt bounds i into [lo, hi].
func clampInt(i, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if i < lo {
		return lo
	}
	if i > hi {
		return hi
	}
	return i
}

// maxInt returns the larger of two ints.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func (h *Heatmap) fmtAxisVal(ax engine.GridAxis, v float64) string {
	if ax.Step > 0 {
		return engine.FormatAxisValue(ax, v)
	}
	if v == math.Round(v) {
		return fmt.Sprintf("%d", int(v))
	}
	return fmt.Sprintf("%.1f", v)
}

// span returns the metric value range (ignoring NaN).
func (h *Heatmap) span() (float64, float64) {
	lo, hi := math.NaN(), math.NaN()
	for _, row := range h.res.Cells {
		for _, v := range row {
			if math.IsNaN(v) {
				continue
			}
			if math.IsNaN(lo) {
				lo, hi = v, v
				continue
			}
			lo = minF(lo, v)
			hi = maxF(hi, v)
		}
	}
	if math.IsNaN(lo) || hi <= lo {
		return 0, 1
	}
	return lo, hi
}

// ramp colors: red -> orange (bottom half), orange -> green (top half).
var (
	rampLow  = [6]float64{0xF3, 0x8B, 0xA8, 0xF9, 0xE2, 0xAF} // red   -> orange
	rampHigh = [6]float64{0xF9, 0xE2, 0xAF, 0xA6, 0xE3, 0xA1} // orange-> green
)

// cellColor maps a metric value onto the ramp.
func cellColor(v, lo, hi float64) string {
	t := (v - lo) / (hi - lo)
	t = clampFloat(t, 0, 1)
	ramp := rampLow
	if t > 0.5 {
		ramp = rampHigh
		t = (t - 0.5) * 2
	} else {
		t *= 2
	}
	r := numLerp(ramp[0], ramp[3], t)
	g := numLerp(ramp[1], ramp[4], t)
	b := numLerp(ramp[2], ramp[5], t)
	return fmt.Sprintf("#%02X%02X%02X", int(r), int(g), int(b))
}

func numLerp(a, b, t float64) float64 { return a + (b-a)*t }

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// SetWidth stores the usable width for the next View call.
func (h *Heatmap) SetWidth(w int) { h.w = w }

// Caption describes metric, best cell and cursor cell.
func (h *Heatmap) Caption() string {
	if !h.HasData() {
		return ""
	}
	lo, hi := h.span()
	best := fmt.Sprintf("★ best %s @ %s×%s = %s",
		engine.MetricLabel(h.res.Metric),
		h.fmtAxisVal(h.axX, h.res.XVals[h.res.Best.X]),
		h.fmtAxisVal(h.axY, h.res.YVals[h.res.Best.Y]),
		num(h.res.BestVal, 2))
	cur := fmt.Sprintf("▶ cursor %s×%s = %s",
		h.fmtAxisVal(h.axX, h.res.XVals[h.cx]),
		h.fmtAxisVal(h.axY, h.res.YVals[h.cy]),
		num(h.CursorValue(), 2))
	meta := fmt.Sprintf(" (range %.2f..%.2f)", lo, hi)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#A6ADC8"))
	return dim.Render(best + "  ·  " + cur + meta)
}

// View renders the grid; expects SetWidth to have been called.
func (h *Heatmap) View() string {
	if !h.HasData() {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#A6ADC8")).
			Render("no sweep yet — the grid runs automatically on entry (g to re-run)")
	}
	nc, nr := h.nCols(), h.nRows()
	cellW := (h.w - 8) / maxInt(nc, 1)
	if cellW < 2 {
		cellW = 2
	}
	if cellW > 8 {
		cellW = 8
	}
	labelW := 7

	lo, hi := h.span()
	var b strings.Builder
	lblStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7086"))

	// column header: X values every other cell
	head := " " + fmt.Sprintf("%*s", labelW, "")
	for x := 0; x < nc; x++ {
		lbl := ""
		if x%2 == 0 {
			lbl = h.fmtAxisVal(h.axX, h.res.XVals[x])
		}
		head += lblStyle.Render(fmt.Sprintf("%-*s", cellW, lbl))
	}
	b.WriteString(head + "\n")

	for y := 0; y < nr; y++ {
		rowLbl := ""
		if y%2 == 0 {
			rowLbl = h.fmtAxisVal(h.axY, h.res.YVals[y])
		}
		line := lblStyle.Render(fmt.Sprintf("%*s", labelW, rowLbl)) + " "
		for x := 0; x < nc; x++ {
			v := h.res.Cells[y][x]
			st := lipgloss.NewStyle()
			txt := "  "
			if !math.IsNaN(v) {
				st = st.Background(lipgloss.Color(cellColor(v, lo, hi))).
					Foreground(lipgloss.Color("#11111B"))
				txt = shortVal(v, lo, hi)
			}
			mark := ""
			if x == h.res.Best.X && y == h.res.Best.Y {
				mark = "★"
			}
			if x == h.cx && y == h.cy {
				mark = "▶"
			}
			if mark != "" {
				txt = mark
			}
			line += st.Render(fmt.Sprintf("%-*s", cellW, txt))
		}
		b.WriteString(line)
		if y < nr-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// shortVal renders a metric compactly to fit a cell.
func shortVal(v, lo, hi float64) string {
	a := math.Abs(hi - lo)
	switch {
	case math.Abs(v) >= 1000:
		return fmt.Sprintf("%.0fk", v/1000)
	case a >= 100:
		return fmt.Sprintf("%.0f", v)
	case a >= 10:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.1f", v)
	}
}
