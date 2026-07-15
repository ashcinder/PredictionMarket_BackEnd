package judge

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	TypePrice           = "TYPE_PRICE"
	TypeVolatility      = "TYPE_VOLATILITY"
	TypeVolume          = "TYPE_VOLUME"
	TypeTechnical       = "TYPE_TECHNICAL"
	TypeTouch           = "TYPE_TOUCH"
	TypeRelative        = "TYPE_RELATIVE"
	TypePriceThreshold  = "TYPE_PRICE_THRESHOLD"
	TypeEvent           = "TYPE_EVENT"
	TypeReturnThreshold = "TYPE_RETURN_THRESHOLD"
	TypePriceRange      = "TYPE_PRICE_RANGE"
	TypeStreak          = "TYPE_STREAK"

	ChainlinkDataFeedEthereum = "CHAINLINK_DATA_FEED_ETHEREUM"
	ChainlinkXAUUSDFeed       = "0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6"
	ChainlinkBTCUSDFeed       = "0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c"
	ChainlinkETHUSDFeed       = "0x5f4eC3Df9cbd43714FE2740f5E3616155c5b8419"
	ChainlinkSOLUSDFeed       = "0x4ffC43a60e009B551865A93d232E33Fce9f01507"
	ChainlinkBNBUSDFeed       = "0x14e613AC84a31f709eadbdF89C6CC390fDc9540A"
	BeijingTimezone           = "Asia/Shanghai"
	LastAtOrBefore            = "LAST_AT_OR_BEFORE"
)

var relativeBenchmarkFeeds = map[string]string{
	"BTC": ChainlinkBTCUSDFeed,
	"ETH": ChainlinkETHUSDFeed,
	"SOL": ChainlinkSOLUSDFeed,
	"BNB": ChainlinkBNBUSDFeed,
}

// RelativeBenchmarkFeed returns the only accepted Ethereum Chainlink feed for
// a supported relative-return benchmark.
func RelativeBenchmarkFeed(symbol string) (string, bool) {
	feed, ok := relativeBenchmarkFeeds[strings.ToUpper(strings.TrimSpace(symbol))]
	return feed, ok
}

// Rule is the machine-readable settlement contract committed in market metadata.
type Rule struct {
	RuleVersion             int     `json:"rule_version,omitempty"`
	Type                    string  `json:"type"`
	Symbol                  string  `json:"symbol"`
	Source                  string  `json:"source"`
	SourceContract          string  `json:"source_contract,omitempty"`
	Benchmark               string  `json:"benchmark,omitempty"`
	BenchmarkSource         string  `json:"benchmark_source,omitempty"`
	BenchmarkSourceContract string  `json:"benchmark_source_contract,omitempty"`
	Timezone                string  `json:"timezone,omitempty"`
	BoundaryPolicy          string  `json:"boundary_policy,omitempty"`
	MaxStalenessSec         int64   `json:"max_staleness_sec,omitempty"`
	Operator                string  `json:"operator,omitempty"`
	Direction               string  `json:"direction,omitempty"`
	Threshold               float64 `json:"threshold,omitempty"`
	LowerThreshold          float64 `json:"lower_threshold,omitempty"`
	UpperThreshold          float64 `json:"upper_threshold,omitempty"`
	StreakDays              int     `json:"streak_days,omitempty"`
	FlatTolerance           float64 `json:"flat_tolerance_percent,omitempty"`
	Indicator               string  `json:"indicator,omitempty"`
	Interval                string  `json:"interval,omitempty"`
	VolumeUnit              string  `json:"volume_unit,omitempty"`
	StartTimeSec            int64   `json:"start_time_sec"`
	EndTimeSec              int64   `json:"end_time_sec"`
}

// Candle is one immutable observation returned by the configured data source.
type Candle struct {
	Time           time.Time
	SourceTime     time.Time
	RoundID        string
	SourceContract string
	Open           float64
	High           float64
	Low            float64
	Close          float64
	Volume         float64
}

type Evidence struct {
	Primary   []Candle
	Benchmark []Candle
}

type Result struct {
	Determinate bool
	Winner      int
	Summary     string
}

// EvaluateStructured deterministically evaluates quantitative market types.
// It returns an indeterminate result whenever required evidence is missing.
func EvaluateStructured(rule Rule, evidence Evidence) Result {
	if err := validateRule(rule); err != nil {
		return indeterminate(err.Error())
	}
	primary, err := validatedCandles(evidence.Primary, rule.StartTimeSec, rule.EndTimeSec)
	if err != nil {
		return indeterminate(err.Error())
	}
	open, close := primary[0].Open, primary[len(primary)-1].Close

	switch rule.Type {
	case TypePrice:
		change := percentChange(open, close)
		tolerance := rule.FlatTolerance
		if tolerance <= 0 {
			return indeterminate("flat tolerance must be explicitly configured")
		}
		var yes bool
		switch strings.ToUpper(rule.Direction) {
		case "UP":
			yes = change > tolerance
		case "DOWN":
			yes = change < -tolerance
		case "FLAT":
			yes = math.Abs(change) <= tolerance
		default:
			return indeterminate("unsupported price direction")
		}
		return decided(yes, fmt.Sprintf("XAU return %.6f%%, tolerance %.6f%%", change, tolerance))

	case TypeVolatility:
		high, low := rangeExtremes(primary)
		amplitude := (high - low) / open * 100
		return compareResult(amplitude, rule.Operator, rule.Threshold, "range amplitude %")

	case TypeReturnThreshold:
		change := math.Abs(percentChange(open, close))
		return compareResult(change, rule.Operator, rule.Threshold, "absolute return %")

	case TypeTouch:
		high, low := rangeExtremes(primary)
		yes := high >= rule.Threshold && low <= rule.Threshold
		// A sampled OHLC range contains every traded price between its low and high.
		return decided(yes, fmt.Sprintf("range low %.6f high %.6f target %.6f", low, high, rule.Threshold))

	case TypePriceThreshold:
		return compareResult(close, rule.Operator, rule.Threshold, "deadline close")

	case TypePriceRange:
		inside := close >= rule.LowerThreshold && close <= rule.UpperThreshold
		switch strings.ToUpper(strings.TrimSpace(rule.Operator)) {
		case "IN_RANGE", "INSIDE":
			return decided(inside, fmt.Sprintf("deadline close %.6f, inclusive range [%.6f, %.6f]", close, rule.LowerThreshold, rule.UpperThreshold))
		case "OUTSIDE_RANGE", "OUTSIDE":
			return decided(!inside, fmt.Sprintf("deadline close %.6f, outside inclusive range [%.6f, %.6f]", close, rule.LowerThreshold, rule.UpperThreshold))
		default:
			return indeterminate("price range operator must be IN_RANGE or OUTSIDE_RANGE")
		}

	case TypeVolume:
		if strings.TrimSpace(rule.VolumeUnit) == "" {
			return indeterminate("volume unit and instrument must be explicit")
		}
		var total float64
		for _, candle := range primary {
			total += candle.Volume
		}
		return compareResult(total, rule.Operator, rule.Threshold, "source volume "+rule.VolumeUnit)

	case TypeTechnical:
		if !strings.EqualFold(rule.Indicator, "MACD") {
			return indeterminate("only MACD is currently supported by deterministic evidence")
		}
		if len(primary) < 35 {
			return indeterminate("MACD requires at least 35 ordered closing prices")
		}
		cross, ok := macdCross(primary)
		if !ok {
			return indeterminate("MACD did not cross on the final observation")
		}
		wantUp := strings.EqualFold(rule.Operator, "CROSS_UP")
		wantDown := strings.EqualFold(rule.Operator, "CROSS_DOWN")
		if !wantUp && !wantDown {
			return indeterminate("MACD operator must be CROSS_UP or CROSS_DOWN")
		}
		return decided((cross > 0 && wantUp) || (cross < 0 && wantDown), fmt.Sprintf("MACD final cross direction %d", cross))

	case TypeRelative:
		benchmark, err := validatedCandles(evidence.Benchmark, rule.StartTimeSec, rule.EndTimeSec)
		if err != nil {
			return indeterminate("benchmark: " + err.Error())
		}
		primaryReturn := percentChange(open, close)
		benchmarkReturn := percentChange(benchmark[0].Open, benchmark[len(benchmark)-1].Close)
		primaryName := firstLabel(rule.Symbol, "XAU")
		benchmarkName := firstLabel(rule.Benchmark, "benchmark")
		return decided(primaryReturn > benchmarkReturn, fmt.Sprintf(
			"%s open %.6f close %.6f return %.6f%%; %s open %.6f close %.6f return %.6f%%",
			primaryName, open, close, primaryReturn,
			benchmarkName, benchmark[0].Open, benchmark[len(benchmark)-1].Close, benchmarkReturn,
		))

	case TypeStreak:
		if len(primary) != rule.StreakDays+1 {
			return indeterminate(fmt.Sprintf("streak requires %d daily boundaries, got %d", rule.StreakDays+1, len(primary)))
		}
		direction := strings.ToUpper(strings.TrimSpace(rule.Direction))
		for i := 1; i < len(primary); i++ {
			if !primary[i].Time.Equal(primary[i-1].Time.Add(24 * time.Hour)) {
				return indeterminate("streak observations must use consecutive Beijing-day boundaries")
			}
			previous, current := primary[i-1].Close, primary[i].Close
			matched := direction == "UP" && current > previous || direction == "DOWN" && current < previous
			if !matched {
				return decided(false, fmt.Sprintf("streak failed on day %d: %.6f to %.6f", i, previous, current))
			}
		}
		return decided(true, fmt.Sprintf("%s streak satisfied across %d consecutive days", direction, rule.StreakDays))

	case TypeEvent:
		return indeterminate("event markets require authoritative documentary evidence and AI consensus")
	default:
		return indeterminate("unsupported rule type")
	}
}

func firstLabel(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return strings.ToUpper(value)
	}
	return fallback
}

func validateRule(rule Rule) error {
	if rule.RuleVersion >= 2 {
		return ValidateVersion2Rule(rule)
	}
	if strings.TrimSpace(rule.Type) == "" || strings.TrimSpace(rule.Source) == "" {
		return errors.New("rule type and source are required")
	}
	if rule.StartTimeSec <= 0 || rule.EndTimeSec <= rule.StartTimeSec {
		return errors.New("rule observation window is invalid")
	}
	return nil
}

func validatedCandles(input []Candle, start, end int64) ([]Candle, error) {
	if len(input) < 1 {
		return nil, errors.New("no market observations")
	}
	for i, candle := range input {
		if candle.Time.Unix() < start || candle.Time.Unix() > end {
			return nil, errors.New("observation lies outside committed window")
		}
		if candle.Open <= 0 || candle.High <= 0 || candle.Low <= 0 || candle.Close <= 0 || candle.High < candle.Low {
			return nil, errors.New("invalid OHLC observation")
		}
		if i > 0 && !candle.Time.After(input[i-1].Time) {
			return nil, errors.New("observations must be strictly ordered")
		}
	}
	return input, nil
}

func compareResult(actual float64, operator string, threshold float64, label string) Result {
	var yes bool
	switch strings.ToUpper(strings.TrimSpace(operator)) {
	case "GT", "GREATER_THAN":
		yes = actual > threshold
	case "GTE", "GREATER_THAN_OR_EQUAL":
		yes = actual >= threshold
	case "LT", "LESS_THAN":
		yes = actual < threshold
	case "LTE", "LESS_THAN_OR_EQUAL":
		yes = actual <= threshold
	case "EQ", "EQUAL":
		yes = math.Abs(actual-threshold) <= 1e-9
	default:
		return indeterminate("unsupported comparison operator")
	}
	return decided(yes, fmt.Sprintf("%s %.6f, threshold %.6f", label, actual, threshold))
}

func rangeExtremes(candles []Candle) (float64, float64) {
	high, low := candles[0].High, candles[0].Low
	for _, candle := range candles[1:] {
		high = math.Max(high, candle.High)
		low = math.Min(low, candle.Low)
	}
	return high, low
}

func percentChange(open, close float64) float64 { return (close - open) / open * 100 }

func decided(yes bool, summary string) Result {
	winner := 1
	if yes {
		winner = 0
	}
	return Result{Determinate: true, Winner: winner, Summary: summary}
}

func indeterminate(summary string) Result {
	return Result{Determinate: false, Winner: -1, Summary: summary}
}

func macdCross(candles []Candle) (int, bool) {
	closes := make([]float64, len(candles))
	for i, candle := range candles {
		closes[i] = candle.Close
	}
	fast, slow := ema(closes, 12), ema(closes, 26)
	line := make([]float64, len(closes))
	for i := range line {
		line[i] = fast[i] - slow[i]
	}
	signal := ema(line, 9)
	prev := line[len(line)-2] - signal[len(signal)-2]
	curr := line[len(line)-1] - signal[len(signal)-1]
	if prev <= 0 && curr > 0 {
		return 1, true
	}
	if prev >= 0 && curr < 0 {
		return -1, true
	}
	return 0, false
}

func ema(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	if len(values) == 0 {
		return out
	}
	k := 2.0 / float64(period+1)
	out[0] = values[0]
	for i := 1; i < len(values); i++ {
		out[i] = values[i]*k + out[i-1]*(1-k)
	}
	return out
}
