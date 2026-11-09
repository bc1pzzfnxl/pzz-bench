// Root tea.Model: workspaces, focus zones, async jobs and the help overlay.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bc1pzzfnxl/pzz-bench/internal/config"
	"github.com/bc1pzzfnxl/pzz-bench/internal/data"
	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
	"github.com/bc1pzzfnxl/pzz-bench/internal/tui/components"
)

// workspaces (PRD 6.3).
type workspace uint8

const (
	wsStudio workspace = iota // 1: chart + sidebar + kpi
	wsGrid                    // 2: 2D optimizer heatmap
	wsData                    // 3: data manager
)

// workspaceName renders the workspace label.
func workspaceName(w workspace) string {
	switch w {
	case wsGrid:
		return "[2] grid optimizer"
	case wsData:
		return "[3] data manager"
	default:
		return "[1] studio"
	}
}

// focus zones.
type zone uint8

const (
	zSidebar zone = iota
	zChart
	zBottom
	zMain // ws2/ws3 main pane
)

// async job messages sent through m.jobs.
type (
	runDoneMsg struct {
		res *engine.Result
		err error
	}
	gridDoneMsg struct {
		res engine.GridResult
		err error
	}
	progressMsg string
	statusMsg   struct {
		text     string
		isErr    bool
		statuses []data.Status
		dbSize   int64
		dbPath   string
	}
)

// runSpec is the immutable input of one queued benchmark run.
type runSpec struct {
	cfg   engine.EngineConfig
	strat engine.Strategy
}

// Model is the pzz-bench root model.
type Model struct {
	paths  config.Paths
	loader *data.DataLoader

	W, H int
	ws   workspace
	zone zone

 fv *components.FormValues
	sb  *components.Sidebar

	chart *components.Chart
	osc   *components.Oscillator
	kpi   *components.KPIPane
	heat  *components.Heatmap

	// cached pane sizes (avoid needless chart refreshes)
	sizes map[string][2]int

	help   bool
	busy   bool
	status string
	isErr  bool

	result *engine.Result
	metric engine.MetricKey
	lastTask engine.GridTask // axis metadata of the latest sweep

	statuses []data.Status
	dbSize   int64
	dbPath   string

	jobs chan tea.Msg
}

// New wires the root model from resolved paths and an open data loader.
func New(paths config.Paths, loader *data.DataLoader) *Model {
	fv := &components.FormValues{}
	if last, err := paths.LoadPresets(); err == nil {
		if p, ok := last["last-run"]; ok && p.Strategy != "" {
			fv.Set("symbol", p.Symbol)
			fv.Set("timeframe", p.Timeframe)
			fv.Set("start", p.Start)
			fv.Set("end", p.End)
			fv.Set("capital", fmt.Sprintf("%g", p.InitialCapital))
			fv.Set("dca", fmt.Sprintf("%g", p.PeriodicInjection))
			fv.Set("interval", p.InjectionInterval)
			fv.Set("fee", fmt.Sprintf("%g", p.FeeRate*100))
			fv.Set("slippage", fmt.Sprintf("%g", p.SlippageRate*100))
			fv.Set("baseline", p.Baseline)
			fv.Set("strategy", p.Strategy)
			for k, v := range p.StrategyParams {
				fv.Set(k, v)
			}
		}
	}
	m := &Model{
		paths:  paths,
		loader: loader,
		zone:   zChart,
		fv:     fv,
		sb:     components.NewSidebar(fv),
		chart:  components.NewChart(80, 20),
		osc:    components.NewOscillator(80, 8),
		kpi:    components.NewKPIPane(),
		heat:   components.NewHeatmap(),
		sizes:  map[string][2]int{},
		status: "ready — press r to run, ? for help",
		jobs:   make(chan tea.Msg),
	}
	loader.Progress = func(label string, n int) {
		m.jobs <- progressMsg(fmt.Sprintf("%s (+%d)", label, n))
	}
	return m
}

// Init auto-runs the benchmark once at startup.
func (m *Model) Init() tea.Cmd {
	return m.queueRun()
}

// waitJob re-arms the single job consumer of m.jobs.
func (m *Model) waitJob() tea.Cmd {
	return func() tea.Msg { return <-m.jobs }
}

// SidebarValues exposes the shared form values (CLI flag seeding).
func (m *Model) SidebarValues() *components.FormValues { return m.fv }

// SyncDefaults re-installs defaults and rebuilds the sidebar wiring.
func (m *Model) SyncDefaults() {
	m.fv.InstallDefaults(false)
	m.sb = components.NewSidebar(m.fv)
}

// queueRun validates the sidebar and launches an async benchmark.
func (m *Model) queueRun() tea.Cmd {
	if m.busy {
		m.status, m.isErr = "run already in progress…", false
		return nil
	}
	cfg, errCfg := m.fv.EngineConfig()
	if errCfg != nil {
		m.status, m.isErr = errCfg.Error(), true
		return nil
	}
	strat, errStrat := m.fv.BuildStrategy()
	if errStrat != nil {
		m.status, m.isErr = errStrat.Error(), true
		return nil
	}
	if err := engine.Validate(cfg); err != nil {
		m.status, m.isErr = err.Error(), true
		return nil
	}
	m.busy = true
	m.isErr = false
	m.status = fmt.Sprintf("ensuring %s/%s candles…", cfg.Symbol, cfg.Timeframe)

	spec := runSpec{cfg: cfg, strat: strat}
	go func() {
		from := engine.WarmupStart(spec.cfg, spec.strat)
		candles, err := m.loader.Ensure(context.Background(),
			spec.cfg.Symbol, spec.cfg.Timeframe,
			from.UnixMilli(), spec.cfg.EndDate.UnixMilli())
		if err != nil {
			m.jobs <- runDoneMsg{err: err}
			return
		}
		res, err := engine.Run(candles, spec.cfg, spec.strat)
		m.jobs <- runDoneMsg{res: res, err: err}
	}()
	return m.waitJob()
}

// applyResult refreshes every studio pane after a completed run.
func (m *Model) applyResult(res *engine.Result) {
	m.result = res
	rep := res.Report
	m.chart.SetData(components.ChartData{
		Times:       res.Times,
		StratEquity: res.StratEquity,
		BaseEquity:  res.BaseEquity,
		StratBTC:    res.StratBTC,
		BaseBTC:     res.BaseBTC,
		Injected:    res.Injected,
		StratName:   res.StrategyName,
		BaseName:    baselineTitle(res.Cfg.BaselineType),
		SatsAlpha:   rep.SatsAlpha,
		EquityAlpha: rep.EquityAlpha,
		MDDStrat:    rep.Strat.MDD,
		MDDBase:     rep.Base.MDD,
	})
	m.osc.SetOsc(components.ResolveOscMode(res.Series), components.OscData{
		Times: res.Times, Series: res.Series,
	})
	m.kpi.SetReport(rep)
	m.kpi.SetTrades(res.Trades)
}

// baselineTitle maps a baseline id to its curve label.
func baselineTitle(id string) string {
	if v, ok := components.BaselineLabels[id]; ok {
		return v
	}
	return id
}

// queueGrid launches the 2D optimizer sweep for the current parameters.
func (m *Model) queueGrid() tea.Cmd {
	if m.busy {
		return nil
	}
	cfg, errCfg := m.fv.EngineConfig()
	if errCfg != nil {
		m.status, m.isErr = errCfg.Error(), true
		return nil
	}
	id := strings.TrimSpace(m.fv.Get("strategy"))
	desc, ok := engine.Description(id)
	if !ok {
		m.status, m.isErr = "unknown strategy "+id, true
		return nil
	}
	xAxis, okX := specAxis(desc, desc.OptimizerAxes[0])
	yAxis, okY := specAxis(desc, desc.OptimizerAxes[1])
	if !okX || !okY {
		m.status, m.isErr = "strategy has no optimizer axes", true
		return nil
	}
	if err := engine.Validate(cfg); err != nil {
		m.status, m.isErr = err.Error(), true
		return nil
	}
	m.busy = true
	m.isErr = false
	m.status = fmt.Sprintf("sweeping %s × %s…", xAxis.Name, yAxis.Name)

	task := engine.GridTask{Cfg: cfg, StrategyID: id, X: xAxis, Y: yAxis, Metric: m.metric}
	m.lastTask = task
	go func() {
		strat, err := engine.BuildStrategy(id, nil)
		if err != nil {
			m.jobs <- gridDoneMsg{err: err}
			return
		}
		from := engine.WarmupStart(cfg, strat)
		candles, err := m.loader.Ensure(context.Background(), cfg.Symbol, cfg.Timeframe,
			from.UnixMilli(), cfg.EndDate.UnixMilli())
		if err != nil {
			m.jobs <- gridDoneMsg{err: err}
			return
		}
		m.jobs <- gridDoneMsg{res: engine.RunGrid(candles, task)}
	}()
	return m.waitJob()
}

// specAxis resolves a ParamSpec into an optimizer GridAxis.
func specAxis(desc engine.StrategyDesc, name string) (engine.GridAxis, bool) {
	for _, s := range desc.Specs {
		if s.Name != name {
			continue
		}
		if !s.Axis || s.Step <= 0 || s.Max <= s.Min {
			return engine.GridAxis{}, false
		}
		return engine.GridAxis{
			Name: s.Name, Label: strings.ToLower(s.Display),
			Start: s.Min, Stop: s.Max, Step: s.Step,
		}, true
	}
	return engine.GridAxis{}, false
}

// cycleMetric advances the heatmap coloring KPI and re-runs the sweep.
func (m *Model) cycleMetric() tea.Cmd {
	switch m.metric {
	case engine.MetricSortino:
		m.metric = engine.MetricEquityAlpha
	case engine.MetricEquityAlpha:
		m.metric = engine.MetricSatsAlpha
	default:
		m.metric = engine.MetricSortino
	}
	return m.queueGrid()
}

// queueRefreshStatuses loads the SQLite inventory for workspace 3.
func (m *Model) queueRefreshStatuses() tea.Cmd {
	go func() {
		symbol := m.fv.Get("symbol")
		st, err := m.loader.Statuses(symbol)
		size, _ := data.DBSizeBytes(m.paths.DatabasePath())
		send := statusMsg{dbPath: m.paths.DatabasePath()}
		if err != nil {
			send.isErr = true
			send.text = "cache read failed: " + err.Error()
		} else {
			send.statuses = st
			send.dbSize = size
			send.text = fmt.Sprintf("cache inventory: %d candles on disk (%s)",
				statusTotal(st), humanSize(size))
		}
		m.jobs <- send
	}()
	return m.waitJob()
}

func statusTotal(st []data.Status) int {
	total := 0
	for _, s := range st {
		total += int(s.Count)
	}
	return total
}

// queueSync pulls every missing candle for the sidebar symbol.
func (m *Model) queueSync() tea.Cmd {
	go func() {
		symbol := m.fv.Get("symbol")
		now := time.Now().UTC().UnixMilli()
		day := int64(24 * 60 * 60 * 1000)
		spans := map[string]int64{
			engine.TF15m: 14 * day,
			engine.TF1h:  30 * day,
			engine.TF4h:  60 * day,
			engine.TF1d:  int64(1502928000000),
			engine.TF1w:  int64(1502928000000),
		}
		var firstErr error
		for _, tf := range engine.Timeframes {
			m.jobs <- progressMsg("sync " + symbol + "/" + tf + "…")
			_, err := m.loader.Ensure(context.Background(), symbol, tf, spans[tf], now)
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
		send := statusMsg{}
		if firstErr != nil {
			send.isErr = true
			send.text = "cache sync incomplete: " + firstErr.Error()
		} else {
			send.text = "cache sync complete"
		}
		m.jobs <- send
	}()
	return m.waitJob()
}

// savePreset persists the current sidebar to presets.toml.
func (m *Model) savePreset() {
	strategyParams := map[string]string{}
	if desc, ok := engine.Description(strings.TrimSpace(m.fv.Get("strategy"))); ok {
		for _, spec := range desc.Specs {
			strategyParams[spec.Name] = m.fv.Get(spec.Name)
		}
	}
	capital, dca := m.fv.Get("capital"), m.fv.Get("dca")
	fee, slip := m.fv.Get("fee"), m.fv.Get("slippage")
	p := config.Preset{
		Symbol:            m.fv.Get("symbol"),
		Timeframe:         m.fv.Get("timeframe"),
		Start:             m.fv.Get("start"),
		End:               m.fv.Get("end"),
		InitialCapital:    parseNum(capital),
		PeriodicInjection: parseNum(dca),
		InjectionInterval: m.fv.Get("interval"),
		FeeRate:           parseNum(fee) / 100,
		SlippageRate:      parseNum(slip) / 100,
		Baseline:          m.fv.Get("baseline"),
		Strategy:          m.fv.Get("strategy"),
		StrategyParams:    strategyParams,
	}
	if err := m.paths.SavePreset(p); err != nil {
		m.status, m.isErr = "preset save failed: "+err.Error(), true
		return
	}
	m.status, m.isErr = "parameters saved to presets.toml", false
}

// parseNum parses loose numeric strings, defaulting to 0.
func parseNum(raw string) float64 {
	s := strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
	s = strings.ReplaceAll(s, "%", "")
	var v float64
	fmt.Sscanf(s, "%g", &v)
	return v
}

// exportReport writes report.md + trades.csv into ./exports.
func (m *Model) exportReport() {
	if m.result == nil {
		m.status, m.isErr = "nothing to export yet — press r", true
		return
	}
	if err := os.MkdirAll(m.paths.ExportsDir, 0o755); err != nil {
		m.status, m.isErr = "export failed: "+err.Error(), true
		return
	}
	res := m.result
	params := m.fv.ParamsSnapshot()
	stamp := time.Now().UTC().Format("20060102-150405")
	md := BuildMarkdown(res, params)
	mdPath := filepath.Join(m.paths.ExportsDir, fmt.Sprintf("report-%s-%s.md", res.Cfg.Symbol, stamp))
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		m.status, m.isErr = "export failed: "+err.Error(), true
		return
	}
	csvPath := filepath.Join(m.paths.ExportsDir, fmt.Sprintf("trades-%s-%s.csv", res.Cfg.Symbol, stamp))
	if err := os.WriteFile(csvPath, []byte(BuildTradesCSV(res.Trades)), 0o644); err != nil {
		m.status, m.isErr = "export failed: "+err.Error(), true
		return
	}
	m.status, m.isErr = fmt.Sprintf("exported %s + %s", mdPath, csvPath), false
}

// humanSize renders byte sizes compactly.
func humanSize(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
