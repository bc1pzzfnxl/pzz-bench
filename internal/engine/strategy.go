package engine

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParamKind describes how a strategy parameter string is parsed.
type ParamKind uint8

// Parameter kinds handled by the built-in strategies.
const (
	KindInt ParamKind = iota
	KindFloat
	KindBool
	KindSelect
)

// ParamSpec documents one tunable parameter of a strategy: how to parse it,
// its bounds / options, its default value (as string) and whether it can be
// used as an optimizer axis.
type ParamSpec struct {
	Name    string // stable key, used in Params maps and TOML presets
	Display string // label shown in the sidebar form
	Kind    ParamKind
	Options []string // KindSelect only
	Max     float64  // numeric bounds (inclusive)
	Min     float64
	Step    float64 // optimizer grid granularity
	Default string  // default value, as typed in the form
	Axis    bool    // eligible as a 2D optimizer axis
}

// Params holds raw (string) parameter values keyed by ParamSpec.Name.
// Values may be missing entirely -- Build methods fall back to spec defaults.
type Params map[string]string

// ParseInt returns the int value for the given key, falling back to the spec
// default when missing or invalid.
func (p Params) Int(spec ParamSpec) (int, error) {
	raw, ok := p[spec.Name]
	if ok {
		raw = strings.TrimSpace(raw)
	}
	if !ok || raw == "" {
		raw = spec.Default
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", spec.Display, raw)
	}
	return v, nil
}

// ParseFloat returns the float value for the given key, falling back to the
// spec default when missing or invalid.
func (p Params) Float(spec ParamSpec) (float64, error) {
	raw, ok := p[spec.Name]
	if ok {
		raw = strings.TrimSpace(raw)
	}
	if !ok || raw == "" {
		raw = spec.Default
	}
	raw = strings.ReplaceAll(raw, "%", "")
	raw = strings.ReplaceAll(raw, ",", ".")
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid number %q", spec.Display, raw)
	}
	return v, nil
}

// Select validates that the raw value is one of the spec options.
func (p Params) Select(spec ParamSpec) (string, error) {
	raw, ok := p[spec.Name]
	if ok {
		raw = strings.TrimSpace(raw)
	}
	if !ok || raw == "" {
		raw = spec.Default
	}
	for _, o := range spec.Options {
		if strings.EqualFold(o, raw) {
			return o, nil
		}
	}
	return "", fmt.Errorf("%s: %q is not one of %v", spec.Display, raw, spec.Options)
}

// Bool parses "true"/"1"/"yes" style values.
func (p Params) Bool(spec ParamSpec) (bool, error) {
	raw, ok := p[spec.Name]
	if ok {
		raw = strings.TrimSpace(raw)
	}
	if !ok || raw == "" {
		raw = spec.Default
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes", "y", "on":
		return true, nil
	case "false", "0", "no", "n", "off":
		return false, nil
	}
	return false, fmt.Errorf("%s: %q is not a boolean", spec.Display, raw)
}

// StrategyDesc registers a built-in strategy: identity, parameter contract
// and constructor. OptimizerAxes are the two default param names charted on
// the Workspace 2 heatmap (X first).
type StrategyDesc struct {
	ID            string
	Name          string
	Specs         []ParamSpec
	OptimizerAxes [2]string
	Build         func(p Params) (Strategy, error)
}

var registry = func() map[string]StrategyDesc {
	return map[string]StrategyDesc{
		StrategyMACross{}.ID():   descMACross(),
		StrategyRSIDCA{}.ID():    descRSIDCA(),
		StrategyMACDTrend{}.ID(): descMACDTrend(),
		StrategyMacro200W{}.ID(): descMacro200W(),
	}
}()

// Strategy IDs of the v1.0 built-in roster.
const (
	IDMACross   = "MA_CROSS"
	IDRSIDCA    = "RSI_DCA"
	IDMACDTrend = "MACD_TREND"
	IDMacro200W = "MACRO_200W"
)

// StrategyMACross implements the dual moving average crossover.
type StrategyMACross struct {
	MAType     string // "SMA" | "EMA"
	Fast       int
	Slow       int
	ExitToFiat bool
	fastMA     []float64
	slowMA     []float64
}

func (StrategyMACross) ID() string { return IDMACross }
func (s StrategyMACross) Name() string {
	return fmt.Sprintf("%s Crossover (%s %d/%d)", s.MAType, strings.ToUpper(s.MAType[:1]), s.Fast, s.Slow)
}
func (s StrategyMACross) WarmupCandles() int { return s.Slow + 1 }
func (s StrategyMACross) ExportSeries() map[string][]float64 {
	return map[string][]float64{"fast": s.fastMA, "slow": s.slowMA}
}

func (s *StrategyMACross) ComputeIndicators(candles []Candle) {
	closes := Closes(candles)
	var fast, slow []float64
	if strings.EqualFold(s.MAType, "EMA") {
		fast = EMA(closes, s.Fast)
		slow = EMA(closes, s.Slow)
	} else {
		fast = SMA(closes, s.Fast)
		slow = SMA(closes, s.Slow)
	}
	s.fastMA, s.slowMA = fast, slow
}

func (s *StrategyMACross) OnCandle(index int, _ PortfolioState) Signal {
	if index < s.WarmupCandles() {
		return Signal{Action: ActionHold}
	}
	fNow, fPrev := s.fastMA[index], s.fastMA[index-1]
	sNow, sPrev := s.slowMA[index], s.slowMA[index-1]
	if math.IsNaN(fNow) || math.IsNaN(fPrev) || math.IsNaN(sNow) || math.IsNaN(sPrev) {
		return Signal{Action: ActionHold}
	}
	if fPrev <= sPrev && fNow > sNow {
		return Signal{Action: ActionBuy, Weight: 1.0, Tag: fmt.Sprintf("MA cross %s>%s", s.MAType, s.MAType)}
	}
	if fPrev >= sPrev && fNow < sNow {
		if s.ExitToFiat {
			return Signal{Action: ActionSell, Weight: 1.0, Tag: fmt.Sprintf("MA cross %s<%s", s.MAType, s.MAType)}
		}
		// Stay long: pause DCA purchases until the next bullish cross.
		return Signal{Action: ActionHold, Tag: "bearish pause"}
	}
	if fNow < sNow {
		return Signal{Action: ActionHold, Tag: "bearish pause"}
	}
	return Signal{Action: ActionHold}
}

// StrategyRSIDCA accumulates on schedule, splurges on oversold dips.
type StrategyRSIDCA struct {
	Period           int
	Oversold         float64
	Overbought       float64
	BaseBuyWeight    float64
	DipBuyWeight     float64
	TakeProfitWeight float64
	rsi              []float64
	prevInjected     float64
}

func (StrategyRSIDCA) ID() string { return IDRSIDCA }
func (s StrategyRSIDCA) Name() string {
	return fmt.Sprintf("RSI Dip Accumulator (RSI %d)", s.Period)
}
func (s StrategyRSIDCA) WarmupCandles() int { return s.Period + 1 }
func (s StrategyRSIDCA) ExportSeries() map[string][]float64 {
	return map[string][]float64{"rsi": s.rsi}
}

func (s *StrategyRSIDCA) ComputeIndicators(candles []Candle) {
	s.rsi = RSI(Closes(candles), s.Period)
	s.prevInjected = math.NaN() // forces first candle to be treated as injected? no: NaN means "no comparison yet"
}

func (s *StrategyRSIDCA) OnCandle(index int, state PortfolioState) Signal {
	if index < s.WarmupCandles() || index >= len(s.rsi) || math.IsNaN(s.rsi[index]) {
		return Signal{Action: ActionHold}
	}
	// Detect a periodic injection: TotalInjected grew since last candle.
	injectedHere := !math.IsNaN(s.prevInjected) && state.TotalInjected > s.prevInjected
	s.prevInjected = state.TotalInjected
	cash := state.Cash // includes this candle's injection (if any)
	rsi := s.rsi[index]

	if rsi < s.Oversold && cash > 0 {
		return Signal{Action: ActionBuy, Weight: s.DipBuyWeight,
			Tag: fmt.Sprintf("RSI(%d)=%.1f < %.0f dip", s.Period, rsi, s.Oversold)}
	}
	if rsi < s.Oversold {
		return Signal{Action: ActionHold, Tag: "dry powder empty"}
	}
	if injectedHere && rsi <= 70 && cash > 0 {
		return Signal{Action: ActionBuy, Weight: s.BaseBuyWeight,
			Tag: fmt.Sprintf("inject@RSI %.1f", rsi)}
	}
	if rsi > s.Overbought && s.TakeProfitWeight > 0 && state.BTC > 0 {
		return Signal{Action: ActionSell, Weight: s.TakeProfitWeight,
			Tag: fmt.Sprintf("RSI(%d)=%.1f > %.0f TP", s.Period, rsi, s.Overbought)}
	}
	return Signal{Action: ActionHold}
}

// StrategyMACDTrend trades momentum crossovers of the MACD oscillator.
type StrategyMACDTrend struct {
	Fast            int
	Slow            int
	Signal          int
	ZeroLineFilter  bool
	macd, sig, hist []float64
}

func (StrategyMACDTrend) ID() string { return IDMACDTrend }
func (s StrategyMACDTrend) Name() string {
	return fmt.Sprintf("MACD Trend (%d/%d/%d)", s.Fast, s.Slow, s.Signal)
}
func (s StrategyMACDTrend) WarmupCandles() int { return s.Slow + s.Signal + 1 }
func (s StrategyMACDTrend) ExportSeries() map[string][]float64 {
	return map[string][]float64{"macd": s.macd, "signal": s.sig, "hist": s.hist}
}

func (s *StrategyMACDTrend) ComputeIndicators(candles []Candle) {
	s.macd, s.sig, s.hist = MACD(Closes(candles), s.Fast, s.Slow, s.Signal)
}

func (s *StrategyMACDTrend) OnCandle(index int, _ PortfolioState) Signal {
	if index < s.WarmupCandles() || index > len(s.macd) {
		return Signal{Action: ActionHold}
	}
	cur, prev := s.macd[index], s.macd[index-1]
	curSig, prevSig := s.sig[index], s.sig[index-1]
	if math.IsNaN(cur) || math.IsNaN(prev) || math.IsNaN(curSig) || math.IsNaN(prevSig) {
		return Signal{Action: ActionHold}
	}
	if prev <= prevSig && cur > curSig {
		if s.ZeroLineFilter && cur >= 0 {
			return Signal{Action: ActionHold, Tag: "MACD bull filtered above zero"}
		}
		return Signal{Action: ActionBuy, Weight: 1.0,
			Tag: fmt.Sprintf("MACD %.2f crosses %.2f", cur, curSig)}
	}
	if prev >= prevSig && cur < curSig {
		return Signal{Action: ActionSell, Weight: 1.0,
			Tag: fmt.Sprintf("MACD %.2f crosses %.2f", cur, curSig)}
	}
	return Signal{Action: ActionHold}
}

// StrategyMacro200W senses price against the 200-week moving average and
// deploys dry powder aggressively when price touches the long-term baseline.
type StrategyMacro200W struct {
	BaseTrendWeeks  int
	MaxMultiplier   float64
	OvervaluedRatio float64
	sma             []float64
	ratio           []float64
	window          int
}

func (StrategyMacro200W) ID() string { return IDMacro200W }
func (s StrategyMacro200W) Name() string {
	return fmt.Sprintf("200W SMA Multiplier (x%.1f)", s.MaxMultiplier)
}
func (s StrategyMacro200W) WarmupCandles() int { return s.window + 1 }

func (s StrategyMacro200W) ExportSeries() map[string][]float64 {
	return map[string][]float64{"ratio": s.ratio, "sma200w": s.sma}
}

func (s *StrategyMacro200W) ComputeIndicators(candles []Candle) {
	// No timeframe knowledge here: the window is derived from the candle grid
	// spacing (first two candles) and the configured week count.
	tf := "1d"
	if len(candles) > 1 {
		d := candles[1].OpenTime - candles[0].OpenTime
		if d <= 0 {
			d = 86400000
		}
		switch {
		case d <= 15*60*1000:
			tf = TF15m
		case d <= 3600000:
			tf = TF1h
		case d <= 4*3600000:
			tf = TF4h
		case d <= 7*24*3600*1000:
			tf = TF1d
		default:
			tf = TF1w
		}
	}
	s.window = s.BaseTrendWeeks * CandlesPerWeek(tf)
	closes := Closes(candles)
	s.sma = SMA(closes, s.window)
	s.ratio = make([]float64, len(closes))
	for i, c := range closes {
		s.ratio[i] = math.NaN()
		if !math.IsNaN(s.sma[i]) && s.sma[i] > 0 {
			s.ratio[i] = c / s.sma[i]
		}
	}
}

func (s StrategyMacro200W) OnCandle(index int, state PortfolioState) Signal {
	if index < s.WarmupCandles() || math.IsNaN(s.ratio[index]) {
		return Signal{Action: ActionHold, Tag: "warming up 200W SMA"}
	}
	r := s.ratio[index]
	if r > s.OvervaluedRatio {
		return Signal{Action: ActionHold, Tag: fmt.Sprintf("ratio %.2f hoard", r)}
	}
	if r <= 1.0 && state.Cash > 0 {
		return Signal{Action: ActionBuy, Weight: 1.0,
			Tag: fmt.Sprintf("ratio %.2f <= 1.0 deploy x%.1f", r, s.MaxMultiplier)}
	}
	// Between 1.0 and OvervaluedRatio: tap the reserve only when injections land.
	if state.Cash > 0 && index > 0 && state.TotalInjected > 0 {
		// deploy a fraction (MaxMultiplier scaled) every candle is too pouncy;
		// invert: deploy remaining reserve proportionally once ratio < 1.5
		if r < 1.5 {
			return Signal{Action: ActionBuy, Weight: 1.0,
				Tag: fmt.Sprintf("ratio %.2f < 1.5 deploy", r)}
		}
	}
	return Signal{Action: ActionHold, Tag: fmt.Sprintf("ratio %.2f wait", r)}
}

// --- registry -------------------------------------------------------------

func descMACross() StrategyDesc {
	return StrategyDesc{
		ID:   IDMACross,
		Name: "Dual MA Crossover",
		Specs: []ParamSpec{
			{Name: "matype", Display: "MA Type", Kind: KindSelect, Options: []string{"SMA", "EMA"}, Default: "EMA"},
			{Name: "fast", Display: "Fast MA", Kind: KindInt, Min: 2, Max: 300, Step: 5, Default: "21", Axis: true},
			{Name: "slow", Display: "Slow MA", Kind: KindInt, Min: 3, Max: 400, Step: 20, Default: "55", Axis: true},
			{Name: "exitToFiat", Display: "Exit to fiat", Kind: KindBool, Default: "true"},
		},
		OptimizerAxes: [2]string{"fast", "slow"},
		Build: func(p Params) (Strategy, error) {
			specs := descMACross().Specs
			mt, err := p.Select(specs[0])
			if err != nil {
				return nil, err
			}
			fast, err := p.Int(specs[1])
			if err != nil {
				return nil, err
			}
			slow, err := p.Int(specs[2])
			if err != nil {
				return nil, err
			}
			exit, err := p.Bool(specs[3])
			if err != nil {
				return nil, err
			}
			if fast < 2 || slow < 3 || fast >= slow {
				return nil, fmt.Errorf("invalid periods: need fast(%d) < slow(%d)", fast, slow)
			}
			return &StrategyMACross{MAType: mt, Fast: fast, Slow: slow, ExitToFiat: exit}, nil
		},
	}
}

func descRSIDCA() StrategyDesc {
	return StrategyDesc{
		ID:   IDRSIDCA,
		Name: "RSI Dip Accumulator",
		Specs: []ParamSpec{
			{Name: "period", Display: "RSI Period", Kind: KindInt, Min: 2, Max: 100, Step: 1, Default: "14"},
			{Name: "oversold", Display: "Oversold", Kind: KindFloat, Min: 5, Max: 50, Step: 2, Default: "30", Axis: true},
			{Name: "overbought", Display: "Take Profit RSI", Kind: KindFloat, Min: 55, Max: 95, Step: 2, Default: "75", Axis: true},
			{Name: "baseWeight", Display: "Base Buy %", Kind: KindFloat, Min: 0.01, Max: 1, Step: 0.01, Default: "0.25"},
			{Name: "dipWeight", Display: "Dip Buy %", Kind: KindFloat, Min: 0.01, Max: 1, Step: 0.01, Default: "1.0"},
			{Name: "tpWeight", Display: "TP Sell %", Kind: KindFloat, Min: 0, Max: 1, Step: 0.05, Default: "0"},
		},
		OptimizerAxes: [2]string{"oversold", "overbought"},
		Build: func(p Params) (Strategy, error) {
			specs := descRSIDCA().Specs
			period, err := p.Int(specs[0])
			if err != nil {
				return nil, err
			}
			os, err := p.Float(specs[1])
			if err != nil {
				return nil, err
			}
			ob, err := p.Float(specs[2])
			if err != nil {
				return nil, err
			}
			bw, err := p.Float(specs[3])
			if err != nil {
				return nil, err
			}
			dw, err := p.Float(specs[4])
			if err != nil {
				return nil, err
			}
			tp, err := p.Float(specs[5])
			if err != nil {
				return nil, err
			}
			if period < 2 || os >= ob || bw <= 0 || dw <= 0 {
				return nil, fmt.Errorf("invalid RSI params")
			}
			return &StrategyRSIDCA{Period: period, Oversold: os, Overbought: ob,
				BaseBuyWeight: clamp(bw), DipBuyWeight: clamp(dw), TakeProfitWeight: clamp(tp)}, nil
		},
	}
}

func descMACDTrend() StrategyDesc {
	return StrategyDesc{
		ID:   IDMACDTrend,
		Name: "MACD Momentum Trend",
		Specs: []ParamSpec{
			{Name: "fast", Display: "MACD Fast", Kind: KindInt, Min: 2, Max: 120, Step: 1, Default: "12", Axis: true},
			{Name: "slow", Display: "MACD Slow", Kind: KindInt, Min: 5, Max: 400, Step: 2, Default: "26", Axis: true},
			{Name: "signal", Display: "MACD Signal", Kind: KindInt, Min: 2, Max: 100, Step: 1, Default: "9"},
			{Name: "zeroLine", Display: "Zero-line filter", Kind: KindBool, Default: "true"},
		},
		OptimizerAxes: [2]string{"fast", "slow"},
		Build: func(p Params) (Strategy, error) {
			specs := descMACDTrend().Specs
			fast, err := p.Int(specs[0])
			if err != nil {
				return nil, err
			}
			slow, err := p.Int(specs[1])
			if err != nil {
				return nil, err
			}
			sig, err := p.Int(specs[2])
			if err != nil {
				return nil, err
			}
			zero, err := p.Bool(specs[3])
			if err != nil {
				return nil, err
			}
			if fast < 2 || slow < 5 || sig < 2 || fast >= slow {
				return nil, fmt.Errorf("invalid MACD periods")
			}
			return &StrategyMACDTrend{Fast: fast, Slow: slow, Signal: sig, ZeroLineFilter: zero}, nil
		},
	}
}

func descMacro200W() StrategyDesc {
	return StrategyDesc{
		ID:   IDMacro200W,
		Name: "200W SMA Valuation",
		Specs: []ParamSpec{
			{Name: "weeks", Display: "Trend SMA (weeks)", Kind: KindInt, Min: 20, Max: 300, Step: 10, Default: "200"},
			{Name: "maxMult", Display: "Max Multiplier", Kind: KindFloat, Min: 1, Max: 5, Step: 0.5, Default: "3.0", Axis: true},
			{Name: "ratio", Display: "Overvalued x", Kind: KindFloat, Min: 1.2, Max: 4, Step: 0.5, Default: "2.5", Axis: true},
		},
		OptimizerAxes: [2]string{"ratio", "maxMult"},
		Build: func(p Params) (Strategy, error) {
			specs := descMacro200W().Specs
			weeks, err := p.Int(specs[0])
			if err != nil {
				return nil, err
			}
			mult, err := p.Float(specs[1])
			if err != nil {
				return nil, err
			}
			ratio, err := p.Float(specs[2])
			if err != nil {
				return nil, err
			}
			if weeks < 20 || mult <= 0 || ratio <= 1 {
				return nil, fmt.Errorf("invalid 200W SMA params")
			}
			return &StrategyMacro200W{BaseTrendWeeks: weeks, MaxMultiplier: mult, OvervaluedRatio: ratio}, nil
		},
	}
}

func clamp(w float64) float64 {
	if w < 0 {
		return 0
	}
	if w > 1 {
		return 1
	}
	return w
}

// StrategyIDs lists built-in strategy IDs in roster order.
func StrategyIDs() []string { return []string{IDMACross, IDRSIDCA, IDMACDTrend, IDMacro200W} }

// Description returns the registry descriptor for a strategy ID.
func Description(id string) (StrategyDesc, bool) {
	d, ok := registry[id]
	return d, ok
}

// DefaultParams returns every spec default for a strategy as a Params map.
func DefaultParams(id string) (Params, bool) {
	d, ok := registry[id]
	if !ok {
		return nil, false
	}
	p := Params{}
	for _, s := range d.Specs {
		p[s.Name] = s.Default
	}
	return p, true
}

// BuildStrategy constructs a strategy instance from the given raw params,
// applying spec defaults for missing or blank fields.
func BuildStrategy(id string, p Params) (Strategy, error) {
	d, ok := registry[id]
	if !ok {
		return nil, fmt.Errorf("unknown strategy %q", id)
	}
	return d.Build(p)
}
