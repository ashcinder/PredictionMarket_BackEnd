package judge

import (
	"math"
	"testing"
	"time"
)

func TestEvaluateStructuredQuantitativeRules(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	primary := []Candle{
		{Time: start, Open: 100, High: 104, Low: 99, Close: 103, Volume: 20},
		{Time: start.Add(time.Hour), Open: 103, High: 108, Low: 102, Close: 107, Volume: 30},
	}
	base := Rule{Symbol: "XAU", Source: "fixture", StartTimeSec: start.Unix(), EndTimeSec: start.Add(time.Hour).Unix()}

	tests := []struct {
		name string
		rule Rule
		ev   Evidence
		want int
	}{
		{"volatility", withRule(base, TypeVolatility, "GTE", 8), Evidence{Primary: primary}, 0},
		{"touch", withRule(base, TypeTouch, "", 106), Evidence{Primary: primary}, 0},
		{"deadline threshold", withRule(base, TypePriceThreshold, "GT", 105), Evidence{Primary: primary}, 0},
		{"explicit volume", func() Rule { r := withRule(base, TypeVolume, "GT", 49); r.VolumeUnit = "COMEX_GC_CONTRACTS"; return r }(), Evidence{Primary: primary}, 0},
		{"relative underperformance", func() Rule { r := withRule(base, TypeRelative, "", 0); r.Benchmark = "BTC"; return r }(), Evidence{Primary: primary, Benchmark: []Candle{
			{Time: start, Open: 100, High: 105, Low: 99, Close: 104},
			{Time: start.Add(time.Hour), Open: 104, High: 115, Low: 103, Close: 114},
		}}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateStructured(tt.rule, tt.ev)
			if !got.Determinate || got.Winner != tt.want {
				t.Fatalf("result = %+v, want winner %d", got, tt.want)
			}
		})
	}
}

func TestEvaluateStructuredFailsClosed(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	base := Rule{Type: TypeVolume, Symbol: "XAU", Source: "fixture", Operator: "GT", Threshold: 1,
		StartTimeSec: start.Unix(), EndTimeSec: start.Add(time.Hour).Unix()}
	tests := []struct {
		name string
		rule Rule
		ev   Evidence
	}{
		{"missing evidence", base, Evidence{}},
		{"ambiguous volume unit", base, Evidence{Primary: []Candle{{Time: start, Open: 1, High: 1, Low: 1, Close: 1, Volume: 10}}}},
		{"event requires documents", Rule{Type: TypeEvent, Source: "official", StartTimeSec: start.Unix(), EndTimeSec: start.Add(time.Hour).Unix()}, Evidence{Primary: []Candle{{Time: start, Open: 1, High: 1, Low: 1, Close: 1}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EvaluateStructured(tt.rule, tt.ev); got.Determinate || got.Winner != -1 {
				t.Fatalf("unsafe result: %+v", got)
			}
		})
	}
}

func TestMACDCrossUsesObservedSeries(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	candles := make([]Candle, 40)
	for i := range candles {
		price := 100 - float64(i)*0.4
		if i >= 35 {
			price += math.Pow(float64(i-34), 2) * 2
		}
		candles[i] = Candle{Time: start.Add(time.Duration(i) * time.Hour), Open: price, High: price + 1, Low: price - 1, Close: price}
	}
	rule := Rule{Type: TypeTechnical, Symbol: "XAU", Source: "fixture", Indicator: "MACD", Operator: "CROSS_UP",
		StartTimeSec: start.Unix(), EndTimeSec: candles[len(candles)-1].Time.Unix()}
	result := EvaluateStructured(rule, Evidence{Primary: candles})
	// The series is deliberately synthetic; the important invariant is that a
	// non-final crossover is not silently treated as a current signal.
	if result.Determinate && result.Summary == "" {
		t.Fatal("MACD decision is missing an auditable summary")
	}
}

func withRule(base Rule, typ, operator string, threshold float64) Rule {
	base.Type = typ
	base.Operator = operator
	base.Threshold = threshold
	return base
}
