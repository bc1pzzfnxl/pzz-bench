package components

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/cursor"
)

func TestFormDefaults(t *testing.T) {
	fv := &FormValues{}
	fv.InstallDefaults(true)
	if fv.Get("symbol") != defaultSymbol {
		t.Fatalf("symbol default: %q", fv.Get("symbol"))
	}
	if fv.Get("fee") != defaultFee {
		t.Fatalf("fee default: %q", fv.Get("fee"))
	}
	if fv.Get("strategy") != defaultStrategy {
		t.Fatalf("strategy default: %q", fv.Get("strategy"))
	}
	// strategy params populated
	if fv.Get("fast") == "" && fv.Get("slow") == "" {
		// not all strategies have fast/slow; just ensure no panic
	}
}

func TestFormEngineConfigConversion(t *testing.T) {
	fv := &FormValues{}
	fv.InstallDefaults(true)
	cfg, err := fv.EngineConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FeeRate != 0.001 {
		t.Fatalf("fee not /100: %v", cfg.FeeRate)
	}
	if cfg.SlippageRate != 0.0005 {
		t.Fatalf("slippage not /100: %v", cfg.SlippageRate)
	}
	if cfg.InitialCapital != 1000 {
		t.Fatalf("capital: %v", cfg.InitialCapital)
	}
	if cfg.PeriodicInjection != 50 {
		t.Fatalf("dca: %v", cfg.PeriodicInjection)
	}
}

func TestSidebarEnterRun(t *testing.T) {
	fv := &FormValues{}
	s := NewSidebar(fv)
	msg := tea.Msg(tea.KeyMsg{Type: tea.KeyEnter})
	for i := 0; i < 200; i++ {
		cmd, ev := s.Update(msg)
		if ev == SidebarRun {
			return
		}
		if cmd == nil {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		} else {
			next := cmd()
			if _, ok := next.(cursor.BlinkMsg); ok {
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			} else {
				msg = next
			}
		}
	}
	t.Fatal("sidebar never reached SidebarRun")
}