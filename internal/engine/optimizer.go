package engine

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"time"
)

// GridAxis is one parameter sweep dimension of the 2D optimizer.
type GridAxis struct {
	Name  string  // Params key
	Label string  // display label
	Start float64 // inclusive
	Stop  float64 // inclusive
	Step  float64
}

// Points returns the swept values (at most a few thousand).
func (a GridAxis) Points() []float64 {
	if a.Step <= 0 {
		return nil
	}
	out := make([]float64, 0, int(a.Stop-a.Start)/int(a.Step)+2)
	for v := a.Start; v <= a.Stop+1e-9; v += a.Step {
		out = append(out, math.Round(v*1e6)/1e6)
	}
	return out
}

// MetricKey selects which KPI colors the heatmap cells.
type MetricKey string

// Optimizer metrics.
const (
	MetricSatsAlpha   MetricKey = "sats_alpha"   // default: BTC beats DCA?
	MetricSortino     MetricKey = "sortino"      // risk-adjusted
	MetricEquityAlpha MetricKey = "equity_alpha"
)

// MetricLabel gives the display label of a metric key.
func MetricLabel(m MetricKey) string {
	switch m {
	case MetricSortino:
		return "Sortino Ratio"
	case MetricEquityAlpha:
		return "Equity Alpha %"
	default:
		return "Sats Alpha %"
	}
}

// GridTask is one optimizer submission: a fixed benchmark configuration, a
// strategy ID, and the two axis sweeps. Axis values override the strategy
// defaults; every other parameter stays at its spec default.
type GridTask struct {
	Cfg        EngineConfig
	StrategyID string
	X, Y       GridAxis
	Metric     MetricKey // cells colored by this KPI (default Sats Alpha)
}

// resultMetric resolves the task metric with its default.
func (t GridTask) resultMetric() MetricKey {
	if t.Metric == "" {
		return MetricSatsAlpha
	}
	return t.Metric
}

// GridPoint identifies a heatmap cell.
type GridPoint struct {
	X, Y int
}

// GridResult is the completed 2D sweep.
type GridResult struct {
	XVals, YVals []float64
	Cells        [][]float64 // [y][x], NaN for skipped/invalid combos
	Best         GridPoint
	BestVal      float64
	RanCount     int
	SkippedCount int
	Elapsed      time.Duration
	Metric       MetricKey
}

// FormatAxisValue renders an axis value for labels, matching the step's precision.
func FormatAxisValue(axis GridAxis, v float64) string {
	if v == math.Round(v) {
		return fmt.Sprintf("%d", int(v))
	}
	digits := 0
	s := axis.Step
	for s < 0.9999 && digits < 2 {
		s *= 10
		digits++
	}
	return fmt.Sprintf("%.*f", digits, v)
}

// RunGrid executes every (x, y) combination across runtime.NumCPU() worker
// goroutines. The immutable candle slice is shared read-only; each cell builds
// its own Strategy instance so indicator buffers never race.
func RunGrid(candles []Candle, task GridTask) GridResult {
	start := time.Now()
	xs := task.X.Points()
	ys := task.Y.Points()
	nX, nY := len(xs), len(ys)
	res := GridResult{
		XVals: xs, YVals: ys,
		Cells:  make([][]float64, nY),
		Metric: task.resultMetric(),
	}
	for yi := range ys {
		res.Cells[yi] = make([]float64, nX)
		for xi := range xs {
			res.Cells[yi][xi] = math.NaN()
		}
	}

	type job struct{ x, y int }
	jobs := make(chan job, nX*nY)
	for y := 0; y < nY; y++ {
		for x := 0; x < nX; x++ {
			jobs <- job{x, y}
		}
	}
	close(jobs)

	workers := runtime.NumCPU()
	if workers > nX*nY {
		workers = nX*nY
	}
	if workers < 1 {
		workers = 1
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				val := cellValue(candles, task, xs[j.x], ys[j.y])
				mu.Lock()
				res.Cells[j.y][j.x] = val
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// find best (NaN excluded) & counts
	res.BestVal = math.NaN()
	for y := 0; y < nY; y++ {
		for x := 0; x < nX; x++ {
			v := res.Cells[y][x]
			if math.IsNaN(v) {
				res.SkippedCount++
				continue
			}
			res.RanCount++
			if math.IsNaN(res.BestVal) || v > res.BestVal {
				res.BestVal = v
				res.Best = GridPoint{X: x, Y: y}
			}
		}
	}
	res.Elapsed = time.Since(start)
	return res
}

func cellValue(candles []Candle, task GridTask, xv, yv float64) float64 {
	p, ok := DefaultParams(task.StrategyID)
	if !ok {
		return math.NaN()
	}
	p[task.X.Name] = FormatAxisValue(task.X, xv)
	p[task.Y.Name] = FormatAxisValue(task.Y, yv)
	strat, err := BuildStrategy(task.StrategyID, p)
	if err != nil {
		return math.NaN()
	}
	r, err := Run(candles, task.Cfg, strat)
	if err != nil || r.Report == nil {
		return math.NaN()
	}
	switch task.resultMetric() {
	case MetricSortino:
		return r.Report.Strat.Sortino
	case MetricEquityAlpha:
		return r.Report.EquityAlpha
	default:
		return r.Report.SatsAlpha
	}
}
