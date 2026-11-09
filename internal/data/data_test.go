package data

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"
)

func TestStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "market.db")
	st, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	in := []engine.Candle{
		{OpenTime: 1000, CloseTime: 1999, Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 10},
		{OpenTime: 2000, CloseTime: 2999, Open: 2, High: 3, Low: 1.5, Close: 2.5, Volume: 20},
	}
	if err := st.UpsertCandles("BTCUSDT", "1d", in); err != nil {
		t.Fatal(err)
	}
	min, max, count, err := st.Extents("BTCUSDT", "1d")
	if err != nil || count != 2 || min != 1000 || max != 2000 {
		t.Fatalf("extents: %d %d %d %v", min, max, count, err)
	}
	out, err := st.LoadRange("BTCUSDT", "1d", 1000, 2000)
	if err != nil || len(out) != 2 {
		t.Fatalf("load: %v %d", err, len(out))
	}
	if out[0].CloseTime != 86400999 {
		t.Fatalf("closetime recomputed: %d", out[0].CloseTime)
	}
	// upsert replace
	in[0].Close = 99
	if err := st.UpsertCandles("BTCUSDT", "1d", in[:1]); err != nil {
		t.Fatal(err)
	}
	out, _ = st.LoadRange("BTCUSDT", "1d", 1000, 1000)
	if out[0].Close != 99 {
		t.Fatalf("replace failed: %v", out[0].Close)
	}
	_ = os.Remove
}

func TestBinanceParsehttptest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := [][]any{
			{float64(1700000000000), "10", "11", "9", "10.5", "100"},
			{float64(1700086400000), "10.5", "12", "10", "11", "200"},
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer srv.Close()

	bp := &BinanceProvider{BaseURLs: []string{srv.URL}, HTTP: srv.Client(), Delay: 0, MaxRetry: 0}
	candles, err := bp.Range(context.Background(), "BTCUSDT", "1h", 1700000000000, 1700086400000)
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 2 {
		t.Fatalf("want 2 candles, got %d", len(candles))
	}
	if candles[0].Open != 10 || candles[1].Close != 11 {
		t.Fatalf("parse mismatch: %+v", candles)
	}
}

func TestMempoolSynthStep(t *testing.T) {
	day := int64(86400000)
	pts := []pricePoint{
		{Time: 0 * day, USD: 100},
		{Time: 2 * day, USD: 200},
	}
	out, err := mempoolToCandles(pts, "1d")
	if err != nil {
		t.Fatal(err)
	}
	// first point spans 2 days at price 100
	if len(out) != 3 {
		t.Fatalf("want 3 daily candles, got %d", len(out))
	}
	if out[0].Open != 100 || out[1].Open != 100 || out[2].Open != 200 {
		t.Fatalf("step mismatch: %+v", out)
	}
	// weekly
	w, err := mempoolToCandles(pts, "1w")
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 2 || w[1].Open != 200 {
		t.Fatalf("weekly mismatch: %+v", w)
	}
}