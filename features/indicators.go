package features

import "math"

func EMA(bars []Bar, period int) float64 {
	if len(bars) < period || period <= 0 {
		return 0
	}
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += bars[i].Close
	}
	ema := sum / float64(period)
	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(bars); i++ {
		ema = (bars[i].Close-ema)*multiplier + ema
	}
	return sanitizeFloat(ema)
}

func RSI(bars []Bar, period int) float64 {
	if len(bars) <= period || period <= 0 {
		return 0
	}

	gains := 0.0
	losses := 0.0
	for i := 1; i <= period; i++ {
		change := bars[i].Close - bars[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	for i := period + 1; i < len(bars); i++ {
		change := bars[i].Close - bars[i-1].Close
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) + (-change)) / float64(period)
		}
	}
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))
	return sanitizeFloat(rsi)
}

func MACD(bars []Bar) float64 {
	if len(bars) < 26 {
		return 0
	}
	return sanitizeFloat(EMA(bars, 12) - EMA(bars, 26))
}

func ATR(bars []Bar, period int) float64 {
	if len(bars) <= period || period <= 0 {
		return 0
	}
	trs := trueRangeSeries(bars)
	if len(trs) <= period {
		return 0
	}
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)
	for i := period + 1; i < len(trs); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}
	return sanitizeFloat(atr)
}

func TrueRangeAverage(bars []Bar, period int) float64 {
	if len(bars) <= period || period <= 0 {
		return 0
	}
	trs := trueRangeSeries(bars)
	if len(trs) <= period {
		return 0
	}
	sum := 0.0
	count := 0
	for i := len(trs) - period; i < len(trs); i++ {
		if i <= 0 {
			continue
		}
		sum += trs[i]
		count++
	}
	if count == 0 {
		return 0
	}
	return sanitizeFloat(sum / float64(count))
}

func Returns(bars []Bar) []float64 {
	if len(bars) < 2 {
		return nil
	}
	out := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		prev := bars[i-1].Close
		curr := bars[i].Close
		if prev <= 0 || curr <= 0 {
			continue
		}
		out = append(out, sanitizeFloat((curr-prev)/prev))
	}
	return out
}

func MeanStd(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	if len(values) == 1 {
		return sanitizeFloat(mean), 0
	}
	acc := 0.0
	for _, v := range values {
		d := v - mean
		acc += d * d
	}
	std := math.Sqrt(acc / float64(len(values)))
	return sanitizeFloat(mean), sanitizeFloat(std)
}

func trueRangeSeries(bars []Bar) []float64 {
	trs := make([]float64, len(bars))
	for i := 1; i < len(bars); i++ {
		high := bars[i].High
		low := bars[i].Low
		prevClose := bars[i-1].Close
		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)
		trs[i] = sanitizeFloat(math.Max(tr1, math.Max(tr2, tr3)))
	}
	return trs
}

func sanitizeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}
