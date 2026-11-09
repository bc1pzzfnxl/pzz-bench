package engine

import "math"

// Vectorized indicator helpers. Every function returns a slice aligned 1:1
// with the input series; positions before the indicator warmup hold NaN.

// Closes extracts the Close series of a candle slice.
func Closes(candles []Candle) []float64 {
	out := make([]float64, len(candles))
	for i, c := range candles {
		out[i] = c.Close
	}
	return out
}

// SMA computes a rolling simple moving average (NaN before window-1).
func SMA(v []float64, window int) []float64 {
	out := make([]float64, len(v))
	if window <= 0 || len(v) == 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	var sum float64
	for i, x := range v {
		sum += x
		if i >= window {
			sum -= v[i-window]
		}
		if i >= window-1 {
			out[i] = sum / float64(window)
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}

// EMA computes an exponential moving average seeded with the simple average
// of the first `period` values (NaN before period-1). alpha = 2/(period+1).
func EMA(v []float64, period int) []float64 {
	out := make([]float64, len(v))
	if period <= 0 || len(v) == 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	alpha := 2.0 / float64(period+1)
	var sum float64
	for i := 0; i < len(v); i++ {
		if i < period-1 {
			sum += v[i]
			out[i] = math.NaN()
			continue
		}
		if i == period-1 {
			sum += v[i]
			out[i] = sum / float64(period)
			continue
		}
		out[i] = out[i-1] + alpha*(v[i]-out[i-1])
	}
	return out
}

// RSI computes Wilder's relative strength index (NaN before `period`).
func RSI(closes []float64, period int) []float64 {
	n := len(closes)
	out := make([]float64, n)
	if period <= 0 || n <= period {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	var avgGain, avgLoss float64
	for i := 1; i <= period; i++ {
		d := closes[i] - closes[i-1]
		if d >= 0 {
			avgGain += d
		} else {
			avgLoss -= d
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	for i := range out {
		out[i] = math.NaN()
	}
	out[period] = rsiFromAvg(avgGain, avgLoss)
	for i := period + 1; i < n; i++ {
		d := closes[i] - closes[i-1]
		gain, loss := 0.0, 0.0
		if d >= 0 {
			gain = d
		} else {
			loss = -d
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		out[i] = rsiFromAvg(avgGain, avgLoss)
	}
	return out
}

func rsiFromAvg(avgGain, avgLoss float64) float64 {
	if avgLoss <= 0 {
		if avgGain <= 0 {
			return 50 // flat series: neutral
		}
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}

// MACD computes the MACD line, signal line and histogram (both EMAs seeded by
// their simple average). All outputs share the input indexing, NaN before the
// slow EMA warmup.零 look-ahead: signal EMA starts on the first valid MACD value.
func MACD(closes []float64, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, hist []float64) {
	n := len(closes)
	macd = make([]float64, n)
	signal = make([]float64, n)
	hist = make([]float64, n)
	fast := EMA(closes, fastPeriod)
	slow := EMA(closes, slowPeriod)
	for i := 0; i < n; i++ {
		macd[i] = math.NaN()
		signal[i] = math.NaN()
		hist[i] = math.NaN()
		if !math.IsNaN(fast[i]) && !math.IsNaN(slow[i]) {
			macd[i] = fast[i] - slow[i]
		}
	}
	// Signal = EMA over the valid MACD segment, seeded by its simple average.
	first := -1
	for i, m := range macd {
		if !math.IsNaN(m) {
			first = i
			break
		}
	}
	if first >= 0 && n-first >= signalPeriod {
		segment := macd[first:]
		seed := 0.0
		for i := 0; i < signalPeriod; i++ {
			seed += segment[i]
		}
		seed /= float64(signalPeriod)
		signal[first+signalPeriod-1] = seed
		alpha := 2.0 / float64(signalPeriod+1)
		for i := first + signalPeriod; i < n; i++ {
			signal[i] = signal[i-1] + alpha*(macd[i]-signal[i-1])
		}
	}
	for i := 0; i < n; i++ {
		if !math.IsNaN(macd[i]) && !math.IsNaN(signal[i]) {
			hist[i] = macd[i] - signal[i]
		}
	}
	return macd, signal, hist
}

// Bollinger computes the middle SMA and the upper/lower bands at
// `mid ± k * population stddev` over a rolling window.
func Bollinger(closes []float64, window int, k float64) (mid, upper, lower []float64) {
	n := len(closes)
	mid = SMA(closes, window)
	upper = make([]float64, n)
	lower = make([]float64, n)
	for i := 0; i < n; i++ {
		upper[i] = math.NaN()
		lower[i] = math.NaN()
	}
	if window <= 0 || n < window {
		return mid, upper, lower
	}
	var sum, sumSq float64
	scale := 1 / float64(window)
	for i := 0; i < n; i++ {
		x := closes[i]
		sum += x
		sumSq += x * x
		if i >= window {
			old := closes[i-window]
			sum -= old
			sumSq -= old * old
		}
		if i < window-1 {
			continue
		}
		m := sum * scale
		variance := sumSq*scale - m*m
		if variance < 0 {
			variance = 0
		}
		sd := math.Sqrt(variance)
		upper[i] = m + k*sd
		lower[i] = m - k*sd
	}
	return mid, upper, lower
}

// CandlesPerWeek maps a timeframe to how many of its candles fit in one week.
func CandlesPerWeek(tf string) int {
	switch tf {
	case TF15m:
		return 672
	case TF1h:
		return 168
	case TF4h:
		return 42
	case TF1w:
		return 1
	default: // TF1d
		return 7
	}
}
