// Sidebar form pane (Workspace left column, fixed 28 columns).
package components

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

// Sidebar colors (Catppuccin Mocha, PRD section 6.1).
const (
	colorPrimaryText = "#CDD6F4"
	colorSubtext     = "#A6ADC8"
	colorYellow      = "#F9E2AF"
	colorMauve       = "#CBA6F7"
	colorMint        = "#A6E3A1"
	colorBTCOrange   = "#F7931A"
)

// SidebarWidth is the fixed column budget of the sidebar pane.
const SidebarWidth = 28

// FormValues owns every sidebar value as plain strings (the raw form state).
// Well-known keys: "symbol", "timeframe", "start", "end", "capital", "dca",
// "interval", "fee", "slippage", "baseline", "strategy"; strategy-specific
// parameters are stored under their ParamSpec.Name.
type FormValues struct {
	strs map[string]*string
}

// Str returns a stable pointer to the string behind a key (huh binds by pointer).
func (fv *FormValues) Str(key string) *string {
	if fv.strs == nil {
		fv.strs = map[string]*string{}
	}
	if s, ok := fv.strs[key]; ok {
		return s
	}
	fv.strs[key] = new(string)
	return fv.strs[key]
}

// Get reads a value (empty when unset).
func (fv *FormValues) Get(key string) string {
	if s, ok := fv.strs[key]; ok && s != nil {
		return *s
	}
	return ""
}

// Set writes a value.
func (fv *FormValues) Set(key, val string) { *fv.Str(key) = val }

// Sidebar default values.
const (
	defaultSymbol    = "BTCUSDT"
	defaultTimeframe = "1d"
	defaultStart     = "2011-04-28"
	defaultCapital   = "1000"
	defaultDCA       = "50"
	defaultInterval  = "weekly"
	defaultFee       = "0.10"
	defaultSlippage  = "0.05"
	defaultBaseline  = engine.BaselinePureDCA
	defaultStrategy  = engine.IDMACross
)

// BaselineLabels maps internal baseline ids to display labels.
var BaselineLabels = map[string]string{
	engine.BaselinePureDCA: "Pure DCA",
	engine.BaselineLumpSum: "Lump Sum",
}

// InjectIntervalLabels maps interval ids to display labels.
var InjectIntervalLabels = map[string]string{
	engine.InjDaily:    "daily",
	engine.InjWeekly:   "weekly",
	engine.InjBiweekly: "biweekly",
	engine.InjMonthly:  "monthly",
}

// InstallDefaults fills unset keys (or everything when force=true).
func (fv *FormValues) InstallDefaults(force bool) {
	set := func(key, val string) {
		if force || fv.Get(key) == "" {
			fv.Set(key, val)
		}
	}
	set("symbol", defaultSymbol)
	set("timeframe", defaultTimeframe)
	set("start", defaultStart)
	if force || fv.Get("end") == "" {
		fv.Set("end", time.Now().UTC().Format("2006-01-02"))
	}
	set("capital", defaultCapital)
	set("dca", defaultDCA)
	set("interval", defaultInterval)
	set("fee", defaultFee)
	set("slippage", defaultSlippage)
	set("baseline", defaultBaseline)
	set("strategy", defaultStrategy)
	if desc, ok := engine.Description(strings.TrimSpace(fv.Get("strategy"))); ok {
		for _, spec := range desc.Specs {
			if force || fv.Get(spec.Name) == "" {
				fv.Set(spec.Name, spec.Default)
			}
		}
	}
}

// rawParams collects the active strategy's parameter strings.
func (fv *FormValues) rawParams() engine.Params {
	desc, ok := engine.Description(strings.TrimSpace(fv.Get("strategy")))
	if !ok {
		return engine.Params{}
	}
	p := engine.Params{}
	for _, spec := range desc.Specs {
		p[spec.Name] = fv.Get(spec.Name)
	}
	return p
}

// EngineConfig converts the sidebar values into an EngineConfig.
// Fee and slippage fields show percents (0.10 == 0.10%) and are divided by 100.
func (fv *FormValues) EngineConfig() (engine.EngineConfig, error) {
	cfg := engine.EngineConfig{
		Symbol:            strings.TrimSpace(fv.Get("symbol")),
		Timeframe:         strings.TrimSpace(fv.Get("timeframe")),
		InjectionInterval: strings.TrimSpace(fv.Get("interval")),
		BaselineType:      strings.TrimSpace(fv.Get("baseline")),
	}
	start, err := ParseDateLoose(fv.Get("start"))
	if err != nil {
		return cfg, fmt.Errorf("start date: %w", err)
	}
	end, err := ParseDateLoose(fv.Get("end"))
	if err != nil {
		return cfg, fmt.Errorf("end date: %w", err)
	}
	cfg.StartDate, cfg.EndDate = start, end
	capital, err := parseLooseFloat(fv.Get("capital"))
	if err != nil {
		return cfg, fmt.Errorf("capital: %w", err)
	}
	cfg.InitialCapital = capital
	dca, err := parseLooseFloat(fv.Get("dca"))
	if err != nil {
		return cfg, fmt.Errorf("DCA amount: %w", err)
	}
	cfg.PeriodicInjection = dca
	fee, err := parseLooseFloat(fv.Get("fee"))
	if err != nil {
		return cfg, fmt.Errorf("fee: %w", err)
	}
	cfg.FeeRate = fee / 100
	slip, err := parseLooseFloat(fv.Get("slippage"))
	if err != nil {
		return cfg, fmt.Errorf("slippage: %w", err)
	}
	cfg.SlippageRate = slip / 100
	return cfg, nil
}

// BuildStrategy constructs the active strategy instance from form values.
func (fv *FormValues) BuildStrategy() (engine.Strategy, error) {
	return engine.BuildStrategy(strings.TrimSpace(fv.Get("strategy")), fv.rawParams())
}

// ParamsSnapshot returns the raw strategy parameter strings (for exports).
func (fv *FormValues) ParamsSnapshot() engine.Params { return fv.rawParams() }

// Summary renders a one-line digest of the current parameters (status bar).
func (fv *FormValues) Summary() string {
	b := BaselineLabels[strings.TrimSpace(fv.Get("baseline"))]
	if b == "" {
		b = fv.Get("baseline")
	}
	iv := InjectIntervalLabels[strings.TrimSpace(fv.Get("interval"))]
	return fmt.Sprintf("%s %s · %s→%s · $%s + %s%s/%s · %s vs %s",
		fv.Get("symbol"), fv.Get("timeframe"),
		fv.Get("start"), fv.Get("end"),
		fv.Get("capital"), fv.Get("dca"), "%", iv, fv.Get("strategy"), strings.ToLower(b))
}

// parseLooseFloat parses a money/percent field, tolerant to commas and %.
func parseLooseFloat(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "%", "")
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", raw)
	}
	return v, nil
}

// ParseDateLoose accepts YYYY-MM-DD or RFC3339 timestamps (UTC).
func ParseDateLoose(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02 15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a valid date (use YYYY-MM-DD)", raw)
}

// CatppuccinTheme colors the huh widgets with the pzz-bench palette.
func CatppuccinTheme() *huh.Theme {
	t := huh.ThemeCharm()
	t.Focused.Base = t.Focused.Base.Foreground(lipgloss.Color(colorPrimaryText))
	t.Focused.Title = t.Focused.Title.Foreground(lipgloss.Color(colorYellow)).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(lipgloss.Color(colorSubtext))
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(lipgloss.Color(colorMauve))
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(lipgloss.Color(colorMint))
	t.Focused.Next = t.Focused.Next.Foreground(lipgloss.Color(colorMauve))
	t.Blurred.Base = t.Blurred.Base.Foreground(lipgloss.Color(colorSubtext))
	return t
}

// SidebarEvent tells the app what the form did during the last Update.
type SidebarEvent uint8

// Sidebar events.
const (
	SidebarNone     SidebarEvent = iota
	SidebarRun                   // Enter on the last field: (re)run the benchmark
	SidebarStrategy              // active strategy changed: rebuild chart pages too
	SidebarAborted               // huh consumed a quit key: form state recovered
)

// Sidebar is the huh-powered parameter panel (fixed 28-column column budget).
type Sidebar struct {
	fv      *FormValues
	form    *huh.Form
	prevStr string // last seen strategy id for change detection
}

// NewSidebar builds the sidebar form over the given shared values.
func NewSidebar(fv *FormValues) *Sidebar {
	fv.InstallDefaults(false)
	s := &Sidebar{fv: fv}
	s.rebuild()
	return s
}

// rebuild swaps in a fresh form instance. Values live in FormValues, so the
// user never loses input across rebuilds.
func (s *Sidebar) rebuild() {
	s.form = buildSidebarForm(s.fv, CatppuccinTheme())
	_ = s.form.Init()
	s.prevStr = strings.TrimSpace(s.fv.Get("strategy"))
}

// coreGroups builds the shared parameters: market, window, costs and the
// strategy selector. Strategy-specific fields are appended by strategyGroup.
func coreGroups(fv *FormValues) []*huh.Group {
	strategies := engine.StrategyIDs()
	stratOptions := make([]huh.Option[string], 0, len(strategies))
	for _, id := range strategies {
		name := id
		if d, ok := engine.Description(id); ok && d.Name != "" {
			name = d.Name
		}
		stratOptions = append(stratOptions, huh.NewOption(name, id))
	}
	symbolOptions := []huh.Option[string]{
		huh.NewOption("BTCUSDT", "BTCUSDT"),
		huh.NewOption("BTCEUR", "BTCEUR"),
		huh.NewOption("BTCUSD (mempool)", "BTCUSD"),
	}
	tfOptions := []huh.Option[string]{
		huh.NewOption("15m", engine.TF15m),
		huh.NewOption("1h", engine.TF1h),
		huh.NewOption("4h", engine.TF4h),
		huh.NewOption("1d", engine.TF1d),
		huh.NewOption("1w", engine.TF1w),
	}
	baselineOptions := []huh.Option[string]{
		huh.NewOption(BaselineLabels[engine.BaselinePureDCA], engine.BaselinePureDCA),
		huh.NewOption(BaselineLabels[engine.BaselineLumpSum], engine.BaselineLumpSum),
	}
	intervalOptions := []huh.Option[string]{
		huh.NewOption(InjectIntervalLabels[engine.InjDaily], engine.InjDaily),
		huh.NewOption(InjectIntervalLabels[engine.InjWeekly], engine.InjWeekly),
		huh.NewOption(InjectIntervalLabels[engine.InjBiweekly], engine.InjBiweekly),
		huh.NewOption(InjectIntervalLabels[engine.InjMonthly], engine.InjMonthly),
	}

	return []*huh.Group{
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("strategy").
				Title("strategy").
				Options(stratOptions...).
				Value(fv.Str("strategy")),
			huh.NewSelect[string]().
				Key("baseline").
				Title("baseline").
				Options(baselineOptions...).
				Value(fv.Str("baseline")),
			huh.NewInput().
				Key("capital").
				Title("capital").
				Placeholder("1000").
				Value(fv.Str("capital")),
		),
		// DCA only makes sense for the pure-DCA baseline; a lump sum
		// deploys the initial capital once and receives no periodic cash.
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("interval").
				Title("dca interval").
				Options(intervalOptions...).
				Value(fv.Str("interval")),
			huh.NewInput().
				Key("dca").
				Title("dca amount").
				Placeholder("50").
				Value(fv.Str("dca")),
		).WithHideFunc(func() bool {
			return strings.TrimSpace(fv.Get("baseline")) != engine.BaselinePureDCA
		}),
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("symbol").
				Title("pair").
				Options(symbolOptions...).
				Value(fv.Str("symbol")),
			huh.NewSelect[string]().
				Key("timeframe").
				Title("timeframe").
				Options(tfOptions...).
				Value(fv.Str("timeframe")),
			huh.NewInput().
				Key("start").
				Title("start").
				Placeholder("2011-04-28").
				Value(fv.Str("start")),
			huh.NewInput().
				Key("end").
				Title("end").
				Placeholder("today").
				Value(fv.Str("end")),
		),
		huh.NewGroup(
			huh.NewInput().
				Key("fee").
				Title("fees %").
				Placeholder("0.10").
				Value(fv.Str("fee")),
			huh.NewInput().
				Key("slippage").
				Title("slippage %").
				Placeholder("0.05").
				Value(fv.Str("slippage")),
		),
	}
}

// candlePeriodParams are strategy parameters expressed in number of candles of
// the selected timeframe, not in days/weeks.
var candlePeriodParams = map[string]bool{
	"fast":   true,
	"slow":   true,
	"period": true,
	"signal": true,
}

// fieldTitle renders a strategy parameter label, appending "(candles)" when the
// value is a candle count so the timeframe dependency is obvious.
func fieldTitle(spec engine.ParamSpec) string {
	title := strings.ToLower(spec.Display)
	if candlePeriodParams[spec.Name] {
		title += " (candles)"
	}
	return title
}

// strategyGroup builds the active strategy's parameter fields. Values persist
// per key in FormValues, so a rebuild never loses user input.
func strategyGroup(fv *FormValues) *huh.Group {
	d, ok := engine.Description(strings.TrimSpace(fv.Get("strategy")))
	if !ok {
		return nil
	}
	fields := make([]huh.Field, 0, len(d.Specs))
	for _, spec := range d.Specs {
		switch spec.Kind {
		case engine.KindSelect:
			opts := make([]huh.Option[string], 0, len(spec.Options))
			for _, o := range spec.Options {
				opts = append(opts, huh.NewOption(strings.ToLower(o), o))
			}
			fields = append(fields, huh.NewSelect[string]().
				Key(spec.Name).
				Title(fieldTitle(spec)).
				Options(opts...).
				Value(fv.Str(spec.Name)))
		case engine.KindBool:
			fields = append(fields, huh.NewSelect[string]().
				Key(spec.Name).
				Title(fieldTitle(spec)).
				Options(huh.NewOption("true", "true"), huh.NewOption("false", "false")).
				Value(fv.Str(spec.Name)))
		default:
			fields = append(fields, huh.NewInput().
				Key(spec.Name).
				Title(fieldTitle(spec)).
				Placeholder(spec.Default).
				Value(fv.Str(spec.Name)))
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return huh.NewGroup(fields...)
}

func newForm(groups []*huh.Group, theme *huh.Theme) *huh.Form {
	return huh.NewForm(groups...).
		WithTheme(theme).
		WithWidth(SidebarWidth - 4).
		WithShowHelp(false)
}

// buildSidebarForm composes the full sidebar: shared fields + strategy params,
// with the strategy's own parameters inserted immediately after the first core
// group (strategy/baseline/capital) so they stay visually tied to the choice.
func buildSidebarForm(fv *FormValues, theme *huh.Theme) *huh.Form {
	groups := coreGroups(fv)
	if g := strategyGroup(fv); g != nil {
		out := make([]*huh.Group, 0, len(groups)+1)
		if len(groups) > 0 {
			out = append(out, groups[0])
			out = append(out, g)
			out = append(out, groups[1:]...)
		} else {
			out = append(out, g)
		}
		groups = out
	}
	return newForm(groups, theme)
}

// BuildCoreForm builds the shared parameters for standalone flows (wizard).
// Callers can widen it with huh.Form.WithWidth.
func BuildCoreForm(fv *FormValues, theme *huh.Theme) *huh.Form {
	return newForm(coreGroups(fv), theme)
}

// BuildStrategyForm builds the active strategy's parameter form. It returns
// nil when the strategy has no parameters.
func BuildStrategyForm(fv *FormValues, theme *huh.Theme) *huh.Form {
	g := strategyGroup(fv)
	if g == nil {
		return nil
	}
	return newForm([]*huh.Group{g}, theme)
}

// Update forwards a bubbletea message to the inner huh form. Quit keys must
// never reach it (the app owns q / esc / ctrl+c); a completed or aborted form
// is transparently rebuilt so the sidebar stays alive.
func (s *Sidebar) Update(msg tea.Msg) (tea.Cmd, SidebarEvent) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "q", "esc", "ctrl+c":
			return nil, SidebarNone
		}
	}
	// huh ignores updates outside StateNormal; recover stale terminals first.
	if s.form.State != huh.StateNormal {
		s.rebuild()
	}
	_, cmd := s.form.Update(msg)

	// Only commit a strategy change (and rebuild the form) when the user is
	// not merely browsing options with the arrow keys: navigating a <select>
	// calls updateValue() on every arrow, and rebuilding mid-navigation resets
	// the highlighted option to the first entry.
	isArrow := false
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "up", "down", "left", "right", "k", "j", "h", "l":
			isArrow = true
		}
	}

	switch s.form.State {
	case huh.StateCompleted:
		s.rebuild() // Enter on the last field -> run, then reset the form
		return cmd, SidebarRun
	case huh.StateAborted:
		s.rebuild() // never let the user brick the sidebar
		return cmd, SidebarAborted
	default:
		if isArrow {
			// keep defaults in sync while browsing, but don't rebuild
			if cur := strings.TrimSpace(s.fv.Get("strategy")); cur != s.prevStr {
				s.fv.InstallDefaults(false)
			}
		} else {
			if cur := strings.TrimSpace(s.fv.Get("strategy")); cur != s.prevStr {
				s.fv.InstallDefaults(false) // sync defaults for the new strategy
				s.rebuild()
				return cmd, SidebarStrategy
			}
		}
	}
	return cmd, SidebarNone
}

// View renders the sidebar form inside its fixed-width column, followed by a
// compact footer advertising the run action and the active timeframe (so candle
// counts can be read in the right unit).
func (s *Sidebar) View() string {
	body := s.form.View()
	tf := strings.TrimSpace(s.fv.Get("timeframe"))
	if tf == "" {
		tf = defaultTimeframe
	}
	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorSubtext)).
		Width(SidebarWidth - 2).
		Render("tf: " + tf + " · periods in candles\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color(colorMint)).Bold(true).Render("▶ run [r]"))
	pad := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorPrimaryText)).
		Width(SidebarWidth - 2)
	return pad.Render(body) + "\n" + footer
}
