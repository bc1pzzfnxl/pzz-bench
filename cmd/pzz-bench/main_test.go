package main

import (
	"testing"

	"github.com/bc1pzzfnxl/pzz-bench/internal/tui/components"
)

func newFlagValues() *components.FormValues {
	fv := &components.FormValues{}
	fv.InstallDefaults(true)
	return fv
}

func TestApplyFormFlagsSeedsExplicitOverrides(t *testing.T) {
	fv := newFlagValues()
	applyFormFlags(fv, flagSet{
		symbol:    "BTCEUR",
		timeframe: "4h",
		start:     "2020-01-01",
		end:       "2024-01-01",
		capital:   "2500",
		dca:       "100",
		interval:  "monthly",
		fee:       "0.2",
		slippage:  "0.1",
		baseline:  "LUMP_SUM",
		strategy:  "RSI_DCA",
		params:    "period=21, oversold=25",
	})
	for key, want := range map[string]string{
		"symbol":    "BTCEUR",
		"timeframe": "4h",
		"start":     "2020-01-01",
		"end":       "2024-01-01",
		"capital":   "2500",
		"dca":       "100",
		"interval":  "monthly",
		"fee":       "0.2",
		"slippage":  "0.1",
		"baseline":  "LUMP_SUM",
		"strategy":  "RSI_DCA",
		"period":    "21",
		"oversold":  "25",
	} {
		if got := fv.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestApplyFormFlagsKeepsOmittedValues(t *testing.T) {
	fv := newFlagValues()
	fv.Set("fee", "0.42")
	applyFormFlags(fv, flagSet{capital: "2500"})
	if got := fv.Get("fee"); got != "0.42" {
		t.Fatalf("omitted fee overwritten: %q", got)
	}
	if got := fv.Get("symbol"); got != "BTCUSDT" {
		t.Fatalf("omitted symbol overwritten: %q", got)
	}
	if got := fv.Get("capital"); got != "2500" {
		t.Fatalf("explicit capital not applied: %q", got)
	}
}

func TestApplyFormFlagsIgnoresMalformedParams(t *testing.T) {
	fv := newFlagValues()
	applyFormFlags(fv, flagSet{params: "nope,,=x, fast=9"})
	if got := fv.Get("nope"); got != "" {
		t.Fatalf("malformed param stored: %q", got)
	}
	if got := fv.Get("fast"); got != "9" {
		t.Fatalf("valid param after malformed ones not applied: %q", got)
	}
}
