package components

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestChartDrawsLineRunesWithoutBraille(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	c := NewChart(100, 30)
	const n = 180
	data := ChartData{
		Times:       make([]int64, n),
		StratEquity: make([]float64, n),
		BaseEquity:  make([]float64, n),
		StratBTC:    make([]float64, n),
		BaseBTC:     make([]float64, n),
		Injected:    make([]float64, n),
	}
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		data.Times[i] = start.AddDate(0, 0, i).UnixMilli()
		data.StratEquity[i] = 1000 + float64(i)*3
		data.BaseEquity[i] = 1000 + float64(i)*2
		data.StratBTC[i] = float64(i) / 100
		data.BaseBTC[i] = float64(i) / 150
		data.Injected[i] = 1000
	}
	c.SetData(data)

	out := c.View()
	if strings.TrimSpace(out) == "" {
		t.Fatal("chart rendered an empty canvas")
	}
	for _, r := range out {
		if r >= 0x2800 && r <= 0x28FF {
			t.Fatalf("chart still renders braille rune %U", r)
		}
	}
	if !strings.ContainsAny(out, "╭╮╰╯─│") {
		t.Fatalf("chart has no line-run plot: %q", out)
	}
}

func TestChartEmptyState(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	c := NewChart(80, 20)
	if got := c.View(); !strings.Contains(got, "press r") {
		t.Fatalf("empty chart hint = %q", got)
	}
}
