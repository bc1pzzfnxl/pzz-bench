package data

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

const binanceKlinesPath = "/api/v3/klines"

// BinanceProvider pulls klines from the public Binance REST API: 1,000
// candles per request, 150 ms between batches, exponential backoff on 429.
type BinanceProvider struct {
	BaseURLs []string
	HTTP     *http.Client
	Delay    time.Duration
	MaxRetry int

	si int // selected base URL index
}

// NewBinanceProvider builds the production provider.
func NewBinanceProvider() *BinanceProvider {
	return &BinanceProvider{
		BaseURLs: binanceEndpoints,
		HTTP:     sharedHTTPClient(),
		Delay:    CandleIntervalHTTPDelay,
		MaxRetry: 5,
	}
}

// ID identifies the provider in status lines.
func (b *BinanceProvider) ID() string { return "binance" }

// CanServe: every engine.Timeframes interval is natively available for
// Binance-native symbols (BTCUSDT, BTCEUR), history from 2017-08-17.
func (b *BinanceProvider) CanServe(symbol, timeframe string) bool {
	switch symbol {
	case SymBTCUSDT, SymBTCEUR:
		return engine.ValidateTimeframe(timeframe) == nil
	default:
		return false
	}
}

// klineRow mirrors one row of the Binance klines payload.
type klineRow [12]any

// fetchPage requests up to limit candles for [startMs, endMs].
func (b *BinanceProvider) fetchPage(ctx context.Context, httpClient *http.Client, symbol, timeframe string,
	startMs, endMs int64) ([]engine.Candle, int, error) {

	url := fmt.Sprintf("%s%s?symbol=%s&interval=%s&startTime=%d&endTime=%d&limit=1000",
		b.pickBase(), binanceKlinesPath, symbol, timeframe, startMs, endMs)

	var lastErr error
	for attempt := 0; attempt <= b.MaxRetry; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", "pzz-bench/1.0")
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("binance request: %w", err)
		} else {
			body, readErr := io.ReadAll(resp.Body)
			_ = readErr
			resp.Body.Close()
			switch {
			case resp.StatusCode == http.StatusOK:
				var rows []([]any)
				if err := json.Unmarshal(body, &rows); err != nil {
					return nil, 0, fmt.Errorf("binance klines decode: %w", err)
				}
				candles, err := parseBinanceKlines(rows, timeframe)
				if err != nil {
					return nil, 0, err
				}
				return candles, resp.StatusCode, nil
			case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == 418:
				// exponential backoff: 300ms, 600ms, 1.2s, 2.4s, ...
				backoff := 300 * time.Millisecond << uint(attempt)
				if b.Delay > 0 {
					select {
					case <-ctx.Done():
						return nil, resp.StatusCode, ctx.Err()
					case <-time.After(backoff):
					}
				}
				lastErr = fmt.Errorf("binance %d (rate limited), retrying in %s", resp.StatusCode, backoff)
				// rotate endpoint on repeated throttling
				b.shiftBase()
			default:
				return nil, resp.StatusCode, fmt.Errorf("binance HTTP %d: %s", resp.StatusCode, snippet(body))
			}
		}
	}
	return nil, http.StatusTooManyRequests, fmt.Errorf("binance retries exhausted: %w", lastErr)
}

func (b *BinanceProvider) pickBase() string {
	if len(b.BaseURLs) == 0 {
		return binanceEndpoints[0]
	}
	return b.BaseURLs[b.si%len(b.BaseURLs)]
}

func (b *BinanceProvider) shiftBase() {
	if len(b.BaseURLs) > 0 {
		b.si = (b.si + 1) % len(b.BaseURLs)
	}
}

// Range walks the requested window forward in 1,000-candle batches.
func (b *BinanceProvider) Range(ctx context.Context, symbol, timeframe string, startMs, endMs int64) ([]engine.Candle, error) {
	httpClient := b.HTTP
	if httpClient == nil {
		httpClient = sharedHTTPClient()
	}
	delay := b.Delay
	if delay <= 0 {
		delay = CandleIntervalHTTPDelay
	}
	var out []engine.Candle
	cursor := startMs
	for cursor <= endMs {
		page, _, err := b.fetchPage(ctx, httpClient, symbol, timeframe, cursor, endMs)
		if err != nil {
			return out, err
		}
		for _, c := range page {
			out = append(out, c)
		}
		if len(page) < 1000 {
			break
		}
		// advance past the last received open time
		last := page[len(page)-1].OpenTime
		step := engine.TimeframeMS(timeframe)
		if last+step > cursor {
			cursor = last + step
		} else {
			cursor += step * 999 // fallback, never loops
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(delay):
		}
	}
	return out, nil
}

func snippet(body []byte) string {
	const max = 240
	if len(body) > max {
		return string(body[:max])
	}
	return string(body)
}

// parseBinanceKlines converts API rows into engine candles.
func parseBinanceKlines(rows []([]any), timeframe string) ([]engine.Candle, error) {
	step := engine.TimeframeMS(timeframe)
	out := make([]engine.Candle, 0, len(rows))
	for _, row := range rows {
		if len(row) < 6 {
			return nil, fmt.Errorf("binance kline row truncated (%d fields)", len(row))
		}
		openTime, err := toF64(row[0])
		if err != nil {
			return nil, fmt.Errorf("binance openTime: %w", err)
		}
		opens, err := toF64(row[1])
		if err != nil {
			return nil, fmt.Errorf("binance open: %w", err)
		}
		high, err2 := toF64(row[2])
		low, err3 := toF64(row[3])
		closeP, err4 := toF64(row[4])
		vol, err5 := toF64(row[5])
		if err := firstErr(err, err2, err3, err4, err5); err != nil {
			return nil, fmt.Errorf("binance kline: %w", err)
		}
		out = append(out, engine.Candle{
			OpenTime:  int64(openTime),
			CloseTime: int64(openTime) + step - 1,
			Open:      opens,
			High:      high,
			Low:       low,
			Close:     closeP,
			Volume:    vol,
		})
	}
	return out, nil
}

func toF64(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case string:
		return strconv.ParseFloat(x, 64)
	default:
		return 0, fmt.Errorf("unexpected type %T", v)
	}
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
