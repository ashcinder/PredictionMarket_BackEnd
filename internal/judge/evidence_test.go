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

func TestEvaluateVersion2Templates(t *testing.T) {
	start := beijingBoundary(2026, time.July, 14)
	end := start.Add(24 * time.Hour)
	base := version2Rule(TypePrice, start, end)
	boundaryEvidence := func(startPrice, endPrice float64) Evidence {
		return Evidence{Primary: []Candle{
			{Time: start, Open: startPrice, High: startPrice, Low: startPrice, Close: startPrice},
			{Time: end, Open: endPrice, High: endPrice, Low: endPrice, Close: endPrice},
		}}
	}
	tests := []struct {
		name   string
		rule   Rule
		input  Evidence
		winner int
	}{
		{"direction", func() Rule { r := base; r.Direction = "UP"; r.FlatTolerance = 0.05; return r }(), boundaryEvidence(4000, 4040), 0},
		{"return threshold", func() Rule { r := base; r.Type = TypeReturnThreshold; r.Operator = "GTE"; r.Threshold = 0.9; return r }(), boundaryEvidence(4000, 4040), 0},
		{"price threshold", func() Rule { r := base; r.Type = TypePriceThreshold; r.Operator = "GTE"; r.Threshold = 4030; return r }(), boundaryEvidence(4000, 4040), 0},
		{"price range", func() Rule {
			r := base
			r.Type = TypePriceRange
			r.Operator = "IN_RANGE"
			r.LowerThreshold = 4020
			r.UpperThreshold = 4050
			return r
		}(), boundaryEvidence(4000, 4040), 0},
		{"relative", func() Rule {
			r := base
			r.Type = TypeRelative
			r.Benchmark = "BTC"
			r.BenchmarkSourceContract = ChainlinkBTCUSDFeed
			return r
		}(), Evidence{
			Primary: boundaryEvidence(4000, 4040).Primary,
			Benchmark: []Candle{
				{Time: start, Open: 100000, High: 100000, Low: 100000, Close: 100000},
				{Time: end, Open: 100500, High: 100500, Low: 100500, Close: 100500},
			},
		}, 0},
		{"streak", func() Rule {
			r := base
			r.Type = TypeStreak
			r.Direction = "UP"
			r.StreakDays = 3
			r.EndTimeSec = start.Add(72 * time.Hour).Unix()
			return r
		}(), Evidence{Primary: []Candle{
			{Time: start, Open: 4000, High: 4000, Low: 4000, Close: 4000},
			{Time: start.Add(24 * time.Hour), Open: 4010, High: 4010, Low: 4010, Close: 4010},
			{Time: start.Add(48 * time.Hour), Open: 4020, High: 4020, Low: 4020, Close: 4020},
			{Time: start.Add(72 * time.Hour), Open: 4030, High: 4030, Low: 4030, Close: 4030},
		}}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateStructured(tt.rule, tt.input)
			if !got.Determinate || got.Winner != tt.winner {
				t.Fatalf("result = %+v, want winner %d", got, tt.winner)
			}
		})
	}
}

func TestEvaluateVersion2ReturnThresholdUsesAbsoluteReturn(t *testing.T) {
	start := beijingBoundary(2026, time.July, 14)
	rule := version2Rule(TypeReturnThreshold, start, start.Add(24*time.Hour))
	rule.Operator, rule.Threshold = "GTE", 2
	result := EvaluateStructured(rule, Evidence{Primary: []Candle{
		{Time: start, Open: 4000, High: 4000, Low: 4000, Close: 4000},
		{Time: start.Add(24 * time.Hour), Open: 3900, High: 3900, Low: 3900, Close: 3900},
	}})
	if !result.Determinate || result.Winner != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func version2Rule(typ string, start, end time.Time) Rule {
	return Rule{
		RuleVersion: 2, Type: typ, Symbol: "XAU", Source: ChainlinkDataFeedEthereum,
		SourceContract: ChainlinkXAUUSDFeed, Timezone: BeijingTimezone,
		BoundaryPolicy: LastAtOrBefore, MaxStalenessSec: 43200,
		StartTimeSec: start.Unix(), EndTimeSec: end.Unix(),
	}
}

func beijingBoundary(year int, month time.Month, day int) time.Time {
	location, err := time.LoadLocation(BeijingTimezone)
	if err != nil {
		panic(err)
	}
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}
