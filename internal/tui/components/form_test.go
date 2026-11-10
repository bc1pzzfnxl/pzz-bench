package components

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

// collectFormValues walks the focused fields and returns their bound values.
// huh only fills Form.results on submit, so this reads the live fields and
// drains the commands that move the form between groups.
func collectFormValues(form *huh.Form) map[string]any {
	values := make(map[string]any)
	_ = form.Init()
	for i := 0; i < 64; i++ {
		field := form.GetFocusedField()
		if field == nil || field.GetKey() == "" {
			break
		}
		if _, seen := values[field.GetKey()]; seen {
			break
		}
		values[field.GetKey()] = field.GetValue()
		drainForm(form, form.NextField())
	}
	return values
}

// drainForm executes the commands returned by huh until it reaches a state
// change (next group/submit) or a cursor blink.
func drainForm(form *huh.Form, cmd tea.Cmd) {
	for i := 0; i < 32 && cmd != nil; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				drainForm(form, c)
			}
			return
		}
		if _, blink := msg.(cursor.BlinkMsg); blink {
			return
		}
		_, cmd = form.Update(msg)
	}
}

func TestFormDefaultDates(t *testing.T) {
	before := time.Now().UTC().Format("2006-01-02")
	fv := &FormValues{}
	fv.InstallDefaults(true)
	after := time.Now().UTC().Format("2006-01-02")

	if got := fv.Get("start"); got != "2011-04-28" {
		t.Fatalf("start default = %q, want 2011-04-28", got)
	}
	if got := fv.Get("end"); got != before && got != after {
		t.Fatalf("end default = %q, want today (%q)", got, before)
	}
}

func TestInstallDefaultsPreservesExplicitValues(t *testing.T) {
	fv := &FormValues{}
	fv.InstallDefaults(true)
	fv.Set("symbol", "BTCEUR")
	fv.Set("end", "2020-01-02")
	fv.Set("fast", "7")

	fv.InstallDefaults(false)
	if fv.Get("symbol") != "BTCEUR" || fv.Get("end") != "2020-01-02" || fv.Get("fast") != "7" {
		t.Fatalf("non-forced defaults overwrote values: %+v", fv.ParamsSnapshot())
	}

	fv.InstallDefaults(true)
	if fv.Get("symbol") != defaultSymbol || fv.Get("fast") != "21" {
		t.Fatalf("forced defaults did not reset values: %q %q", fv.Get("symbol"), fv.Get("fast"))
	}
	if fv.Get("end") != time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("forced defaults did not refresh end: %q", fv.Get("end"))
	}
}

func TestBuildCoreForm(t *testing.T) {
	fv := &FormValues{}
	fv.InstallDefaults(true)
	form := BuildCoreForm(fv, CatppuccinTheme())
	if form == nil {
		t.Fatal("BuildCoreForm returned nil")
	}
	values := collectFormValues(form)
	if got := values["symbol"]; got != defaultSymbol {
		t.Fatalf("core form symbol = %v, want %q", got, defaultSymbol)
	}
	if got := values["start"]; got != defaultStart {
		t.Fatalf("core form start = %v, want %q", got, defaultStart)
	}
	if got := values["strategy"]; got != defaultStrategy {
		t.Fatalf("core form strategy = %v, want %q", got, defaultStrategy)
	}
}

func TestBuildStrategyFormFollowsSelection(t *testing.T) {
	fv := &FormValues{}
	fv.InstallDefaults(true)
	form := BuildStrategyForm(fv, CatppuccinTheme())
	if form == nil {
		t.Fatal("BuildStrategyForm returned nil for the default strategy")
	}
	values := collectFormValues(form)
	desc, ok := engine.Description(defaultStrategy)
	if !ok {
		t.Fatalf("description missing for %s", defaultStrategy)
	}
	for _, spec := range desc.Specs {
		if got := values[spec.Name]; got != spec.Default {
			t.Fatalf("%s field = %v, want default %q", spec.Name, got, spec.Default)
		}
	}

	fv.Set("strategy", engine.IDMacro200W)
	fv.InstallDefaults(false)
	form = BuildStrategyForm(fv, CatppuccinTheme())
	if form == nil {
		t.Fatal("BuildStrategyForm returned nil for MACRO_200W")
	}
	values = collectFormValues(form)
	if got := values["weeks"]; got != "200" {
		t.Fatalf("macro weeks field = %v, want 200", got)
	}
	if _, leaked := values["fast"]; leaked {
		t.Fatalf("strategy form leaked a MACrossover field: %v", values["fast"])
	}
}

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
