// Shared text formatters for the components package (pzz-bench palette).
package components

import (
	"fmt"
	"math"
	"strings"
)

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

// thousands formats an int with comma grouping.
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

// pct renders a float as "+12.40%" with an explicit sign, NaN-safe.
func pct(v float64, digits int) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	return fmt.Sprintf("%+.*f%%", digits, v)
}

// num renders a float with the requested precision, NaN-safe.
func num(v float64, digits int) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	return fmt.Sprintf("%.*f", digits, v)
}
