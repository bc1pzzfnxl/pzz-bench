// Package tui hosts the Charmbracelet terminal interface of pzz-bench.
package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Catppuccin Mocha + Bitcoin Orange palette (PRD section 6.1).
const (
	BaseBg       = "#1E1E2E" // Mocha Base
	Surface0     = "#313244" // inactive borders
	Mauve        = "#CBA6F7" // focus borders
	BTCOrange    = "#F7931A" // baseline curve & accent
	MintGreen    = "#A6E3A1" // strategy when beating baseline
	SapphireBlue = "#89B4FA" // strategy alternative
	SoftRed      = "#F38BA8" // drawdown / sell / negative
	PrimaryText  = "#CDD6F4"
	Subtext      = "#A6ADC8"
	Yellow       = "#F9E2AF"
	Maroon       = "#EBA0AC"
	Teal         = "#94E2D5"
)

// Reusable lipgloss styles.
var (
	styleBaseText = lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryText))
	styleSubtext  = lipgloss.NewStyle().Foreground(lipgloss.Color(Subtext))
	styleAccent   = lipgloss.NewStyle().Foreground(lipgloss.Color(BTCOrange)).Bold(true)
	styleGood     = lipgloss.NewStyle().Foreground(lipgloss.Color(MintGreen)).Bold(true)
	styleBad      = lipgloss.NewStyle().Foreground(lipgloss.Color(SoftRed)).Bold(true)
	styleTitle    = lipgloss.NewStyle().Foreground(lipgloss.Color(Yellow)).Bold(true)
)

// Border styles for panes: active panes get the Mauve focus border.
func paneBorder(active bool) lipgloss.Style {
	color := lipgloss.Color(Surface0)
	if active {
		color = lipgloss.Color(Mauve)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color)
}

// Title bar style.
var titleBarStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(PrimaryText)).
	Background(lipgloss.Color(Surface0))

// themeForm brings the Catppuccin palette into huh input styling.
func themeForm() *lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryText))
	return &s
}

// money renders 12345.67 as "$12,345.67" (negative-aware).
func money(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "n/a"
	}
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	whole := math.Floor(v + 0.5)
	frac := int(math.Round((v - math.Floor(v)) * 100))
	wholeStr := thousands(int(whole))
	if frac >= 100 {
		frac = 0
		wholeStr = thousands(int(whole) + 1)
	}
	return fmt.Sprintf("$%s%s.%02d", sign, wholeStr, frac)
}

// thousands formats an int with dot-free comma grouping.
func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}

// pct renders a float as "+12.4%" with an explicit sign.
func pct(v float64, digits int) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	s := fmt.Sprintf("%+.*f%%", digits, v)
	return strings.ReplaceAll(s, "nan%", "n/a")
}

// num renders a float with the requested precision, NaN-safe.
func num(v float64, digits int) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	return fmt.Sprintf("%.*f", digits, v)
}

// sats renders 61200000 as "61 200 000".
func sats(n int64) string {
	if n <= 0 {
		return "0"
	}
	s := fmt.Sprintf("%d", n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, " ")
}

// signedPctColor renders a delta value with a colored pill:
// green when positive, red when negative, dim when NaN.
func signedPctColor(v float64, digits int) string {
	if math.IsNaN(v) {
		return styleSubtext.Render("n/a")
	}
	s := fmt.Sprintf("%+.*f%%", digits, v)
	if v >= 0 {
		return styleGood.Render("🟢 " + s)
	}
	return styleBad.Render("🔴 " + s)
}

// compactMoney renders big values like "$45.3k" / "$1.2M".
func compactMoney(v float64) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	a := math.Abs(v)
	switch {
	case a >= 1e9:
		return fmt.Sprintf("$%.2fB", v/1e9)
	case a >= 1e6:
		return fmt.Sprintf("$%.2fM", v/1e6)
	case a >= 1e3:
		return fmt.Sprintf("$%.1fk", v/1e3)
	default:
		return money(v)
	}
}

// fmtDownPanel builds date strings for labels.
func fmtDate(unixMs int64) string {
	return compactDate(unixMs / 1000)
}
