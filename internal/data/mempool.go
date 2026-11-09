package data

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

// MempoolProvider serves pre-Binance history (down to 2011) from the
// mempool.space mirror. One caveat: mirror points are weekly -- the daily
// candles we derive from them are a step function of the weekly close and
// are flagged as synthetic.
type MempoolProvider struct {
	BaseURL string
	HTTP    *http.Client
}

// NewMempoolProvider builds the production provider.
func NewMempoolProvider() *MempoolProvider {
	return &MempoolProvider{BaseURL: "https://mempool.space", HTTP: sharedHTTPClient()}
}

// pricePoint mirrors one entry of mempool's historical-price payload.
// time is Unix **seconds**; missing fiat series use -1 sentinels.
type pricePoint struct {
	Time int64 // milliseconds
	USD  float64
	EUR  float64
}

// ID identifies the provider in status lines.
func (m *MempoolProvider) ID() string { return "mempool.space" }

// CanServe: the mirror answers daily/weekly macro history (USD & EUR).
func (m *MempoolProvider) CanServe(symbol, timeframe string) bool {
	switch symbol {
	case SymBTCUSD, SymBTCUSDT, SymBTCEUR:
		return timeframe == engine.TF1d || timeframe == engine.TF1w
	default:
		return false
	}
}

// fetchHistorical downloads and parses the weekly price frame in the given
// quotation currency ("USD" or "EUR"). Use FetchPrices instead.
func (m *MempoolProvider) fetchHistorical(ctx context.Context, currency string) ([]pricePoint, error) {
	return m.FetchPrices(ctx, currency)
}

// FetchPrices returns ascending weekly price points for currency USD|EUR.
func (m *MempoolProvider) FetchPrices(ctx context.Context, currency string) ([]pricePoint, error) {
	url := m.BaseURL + "/api/v1/historical-price"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pzz-bench/1.0")
	httpClient := m.HTTP
	if httpClient == nil {
		httpClient = sharedHTTPClient()
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mempool request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 240))
		return nil, fmt.Errorf("mempool HTTP %d: %s", resp.StatusCode, string(body))
	}
	var raw []struct {
		Time int64 `json:"time"`
		USD  any   `json:"USD"`
		EUR  any   `json:"EUR"`
		GBP  any   `json:"GBP"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("mempool decode: %w", err)
	}
	out := make([]pricePoint, 0, len(raw))
	for _, e := range raw {
		var v float64
		switch currency {
		case SymBTCEUR, "EUR":
			v, _ = toF64(e.EUR)
		default:
			v, _ = toF64(e.USD)
		}
		if v <= 0 { // -1 sentinel or missing week
			continue
		}
		out = append(out, pricePoint{Time: e.Time * 1000, USD: v, EUR: v})
	}
	// wire order is newest-first; restore ascending
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// DailyCandles derives synthetic daily candles (step function of the weekly
// price) covering every day between successive weekly points.
func (m *MempoolProvider) DailyCandles(ctx context.Context, currency string) ([]engine.Candle, error) {
	points, err := m.FetchPrices(ctx, currency)
	if err != nil {
		return nil, err
	}
	day := int64(86400000)
	var out []engine.Candle
	for i, p := range points {
		end := p.Time + day
		if i+1 < len(points) {
			end = points[i+1].Time
		}
		for t := p.Time; t < end && len(out) < 6000; t += day {
			out = append(out, engine.Candle{
				OpenTime:  t,
				CloseTime: t + day - 1,
				Open:      p.USD, High: p.USD, Low: p.USD, Close: p.USD,
				Volume: 0,
			})
		}
	}
	return out, nil
}

// WeeklyCandles converts each weekly point into one weekly candle.
func (m *MempoolProvider) WeeklyCandles(ctx context.Context, currency string) ([]engine.Candle, error) {
	points, err := m.FetchPrices(ctx, currency)
	if err != nil {
		return nil, err
	}
	week := int64(7 * 86400000)
	out := make([]engine.Candle, 0, len(points))
	for _, p := range points {
		out = append(out, engine.Candle{
			OpenTime:  p.Time,
			CloseTime: p.Time + week - 1,
			Open:      p.USD, High: p.USD, Low: p.USD, Close: p.USD,
			Volume: 0,
		})
	}
	return out, nil
}

// Range adapts the provider to the Provider interface (daily/weekly only).
func (m *MempoolProvider) Range(ctx context.Context, symbol, timeframe string, _start, endMs int64) ([]engine.Candle, error) {
	if timeframe != engine.TF1d && timeframe != engine.TF1w {
		return nil, fmt.Errorf("mempool mirror only supports 1d/1w (got %s)", timeframe)
	}
	var (
		candles []engine.Candle
		err     error
	)
	if timeframe == engine.TF1d {
		candles, err = m.DailyCandles(ctx, symbol)
	} else {
		candles, err = m.WeeklyCandles(ctx, symbol)
	}
	if err != nil {
		return nil, err
	}
	out := make([]engine.Candle, 0, len(candles))
	for _, c := range candles {
		if c.OpenTime >= endMs {
			break
		}
		out = append(out, c)
	}
	return out, nil
}

// mempoolFirstDate is 2011-04-28, the mirror's earliest weekly point.
var MempoolFirstDate = time.Date(2011, 4, 28, 0, 0, 0, 0, time.UTC)
