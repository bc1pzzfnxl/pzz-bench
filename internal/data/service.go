package data

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

// binanceTradingStart is 2017-08-17, the first Binance klines timestamp.
var binanceTradingStart = int64(1502928000000) // 2017-08-17 00:00 UTC

// Progress reports downloader activity to the TUI (optional callback).
type Progress func(label string, fetched int)

// DataLoader stitches the SQLite cache with the providers:
//
//   - BTCUSDT / BTCEUR: Binance klines (from 2017-08-17), all timeframes;
//     daily/weekly history *before* that falls back to the mempool.space
//     mirror (synthetic candles built from the weekly price).
//   - BTCUSD: mempool mirror only (daily/weekly).
//
// Network calls only fetch the missing delta between the cached range and
// the requested one -- offline runs hit SQLite only.
type DataLoader struct {
	Store    *Store
	Binance  *BinanceProvider
	Mempool  *MempoolProvider
	Progress Progress
}

// NewDataLoader wires production providers over the given store.
func NewDataLoader(store *Store) *DataLoader {
	return &DataLoader{Store: store, Binance: NewBinanceProvider(), Mempool: NewMempoolProvider()}
}

// Status describes one cached (symbol, timeframe) extent for the TUI.
type Status struct {
	Symbol    string
	Timeframe string
	Count     int64
	First     int64
	Last      int64
}

// Statuses returns the cache inventory for every timeframe.
func (d *DataLoader) Statuses(symbol string) ([]Status, error) {
	out := make([]Status, 0, len(engine.Timeframes))
	for _, tf := range engine.Timeframes {
		min, max, count, err := d.Store.Extents(symbol, tf)
		if err != nil {
			return nil, err
		}
		out = append(out, Status{Symbol: symbol, Timeframe: tf, Count: count, First: min, Last: max})
	}
	return out, nil
}

// Ensure loads candles for [startMs, endMs], reconciling missing head/tail
// gaps online, then serves the range from SQLite.
func (d *DataLoader) Ensure(ctx context.Context, symbol, timeframe string, startMs, endMs int64) ([]engine.Candle, error) {
	if err := engine.ValidateTimeframe(timeframe); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := d.mergeMempoolHistory(ctx, symbol, timeframe, startMs, endMs); err != nil {
		return nil, err
	}

	min, max, count, err := d.Store.Extents(symbol, timeframe)
	if err != nil {
		return nil, err
	}
	inWindow := func(a, b int64) bool { return b >= a && a <= endMs && b >= startMs }

	var fetched int
	report := func(label string, n int) {
		fetched += n
		if d.Progress != nil {
			d.Progress(label, fetched)
		}
	}

	// Mempool-only symbols: the mirror sends everything at once; just make
	// sure the daily frame is already cached (mergeMempoolHistory handles it).
	if symbol == SymBTCUSD {
		if count == 0 {
			return nil, fmt.Errorf("mempool history not cached yet; press u in the Data manager")
		}
		candles, err := d.Store.LoadRange(symbol, timeframe, startMs, endMs)
		if err != nil {
			return nil, err
		}
		if len(candles) == 0 {
			return nil, fmt.Errorf("no %s data cached between %s and %s",
				timeframe, millitoDate(startMs), millitoDate(endMs))
		}
		return candles, nil
	}

	// Binance-native symbols: reconcile gaps.
	switch {
	case inWindow(min, max):
		// head gap
		if count > 0 && startMs < min {
			candles, err := d.fetchBinance(ctx, symbol, timeframe, startMs, min-engine.TimeframeMS(timeframe), report)
			if err != nil {
				return nil, err
			}
			if err := d.Store.UpsertCandles(symbol, timeframe, candles); err != nil {
				return nil, err
			}
		}
		// tail gap
		if count > 0 && endMs > max {
			candles, err := d.fetchBinance(ctx, symbol, timeframe, max+engine.TimeframeMS(timeframe), endMs, report)
			if err != nil {
				return nil, err
			}
			if err := d.Store.UpsertCandles(symbol, timeframe, candles); err != nil {
				return nil, err
			}
		}
	default:
		// nothing cached for this window at all
		candles, err := d.fetchBinance(ctx, symbol, timeframe, startMs, endMs, report)
		if err != nil && len(candles) == 0 {
			return nil, err
		}
		if err := d.Store.UpsertCandles(symbol, timeframe, candles); err != nil {
			return nil, err
		}
	}

	candles, err := d.Store.LoadRange(symbol, timeframe, startMs, endMs)
	if err != nil {
		return nil, err
	}
	if len(candles) == 0 {
		return nil, fmt.Errorf("no candles available for %s/%s in %s..%s (earliest Binance data: %s)",
			symbol, timeframe, millitoDate(startMs), millitoDate(endMs), millitoDate(binanceTradingStart))
	}
	return candles, nil
}

// fetchBinance guards the provider call for Binance symbols.
func (d *DataLoader) fetchBinance(ctx context.Context, symbol, timeframe string, startMs, endMs int64, report Progress) ([]engine.Candle, error) {
	if endMs < startMs {
		return nil, nil
	}
	if report != nil {
		report(fmt.Sprintf("Binance %s/%s %s..%s", symbol, timeframe, millitoDate(startMs), millitoDate(endMs)), 0)
	}
	candles, err := d.Binance.Range(ctx, symbol, timeframe, startMs, endMs)
	if err != nil && len(candles) > 0 {
		// partial batches are still cached; surface as non-fatal
		if report != nil {
			report(fmt.Sprintf("Binance partial fetch (%d candles): %v", len(candles), err), len(candles))
		}
		return candles, nil
	}
	return candles, err
}

// mergeMempoolHistory backfills daily/weekly candles before Binance existed.
// For BTCEUR the mirror's EUR series only starts in ~2015; missing weeks are
// skipped transparently.
func (d *DataLoader) mergeMempoolHistory(ctx context.Context, symbol, timeframe string, startMs, endMs int64) error {
	if timeframe != engine.TF1d && timeframe != engine.TF1w {
		return nil
	}
	if !d.Mempool.CanServe(symbol, timeframe) {
		return nil
	}
	// Only bother when the requested window starts before Binance history.
	if startMs >= binanceTradingStart {
		return nil
	}
	min, _, count, err := d.Store.Extents(symbol, timeframe)
	if err != nil {
		return err
	}
	if count > 0 && min <= startMs {
		return nil // already covered
	}
	if d.Progress != nil {
		d.Progress(fmt.Sprintf("mempool.space %s %s (pre-2017 macro archive)", symbol, timeframe), 0)
	}
	points, err := d.Mempool.FetchPrices(ctx, symbol)
	if err != nil {
		return fmt.Errorf("mempool fetch: %w", err)
	}
	candles, err := mempoolToCandles(points, timeframe)
	if err != nil {
		return err
	}
	filtered := make([]engine.Candle, 0, len(candles))
	for _, c := range candles {
		if c.OpenTime < binanceTradingStart {
			filtered = append(filtered, c)
		}
	}
	return d.Store.UpsertCandles(symbol, timeframe, filtered)
}

// mempoolToCandles builds candles from weekly price points: for `1d`, each
// day of the week carries the weekly price (step function); for `1w`, one
// candle per point.
func mempoolToCandles(points []pricePoint, timeframe string) ([]engine.Candle, error) {
	if timeframe == engine.TF1w {
		return (&MempoolProvider{}). weeklyFromPoints(points), nil
	}
	day := int64(86400000)
	out := make([]engine.Candle, 0, len(points)*7)
	for i, p := range points {
		price := p.USD
		if timeframe == engine.TF1d && price <= 0 {
			continue
		}
		end := p.Time + day
		if i+1 < len(points) {
			end = points[i+1].Time
		}
		for t := p.Time; t < end && len(out) < 6000; t += day {
			out = append(out, engine.Candle{
				OpenTime: t, CloseTime: t + day - 1,
				Open: price, High: price, Low: price, Close: price, Volume: 0,
			})
		}
	}
	return out, nil
}

func (m *MempoolProvider) weeklyFromPoints(points []pricePoint) []engine.Candle {
	week := int64(7 * 86400000)
	out := make([]engine.Candle, 0, len(points))
	for _, p := range points {
		out = append(out, engine.Candle{
			OpenTime: p.Time, CloseTime: p.Time + week - 1,
			Open: p.USD, High: p.USD, Low: p.USD, Close: p.USD, Volume: 0,
		})
	}
	return out
}

// DBSizeBytes reports the sqlite file size for the data manager.
func DBSizeBytes(path string) (int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return st.Size(), nil
}

// millitoDate renders a ms timestamp as YYYY-MM-DD (UTC).
func millitoDate(ms int64) string {
	if ms <= 0 {
		return "?"
	}
	return time.Unix(ms/1000, 0).UTC().Format("2006-01-02")
}
