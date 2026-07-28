package marketdata

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"PredictionMarket/internal/judge"

	"github.com/ethereum/go-ethereum/common"
)

// SignalRound is a compact, auditable Chainlink observation included in the
// managed-trading context. The model receives source metadata and calculated
// values, not permission to invent a missing quote.
type SignalRound struct {
	Symbol         string  `json:"symbol"`
	Boundary       string  `json:"boundary"`
	SourceTime     string  `json:"source_time"`
	RoundID        string  `json:"round_id"`
	SourceContract string  `json:"source_contract"`
	PriceUSD       float64 `json:"price_usd"`
}

// StructuredSignal is an interim evaluation of the exact version-2 rule
// committed to IPFS. It uses "current" only as a progress snapshot; Sentinel
// still performs the authoritative evaluation at the committed end boundary.
type StructuredSignal struct {
	RuleType              string        `json:"rule_type"`
	Status                string        `json:"status"`
	CurrentCondition      string        `json:"current_condition"`
	ObservationTime       string        `json:"observation_time"`
	PrimarySymbol         string        `json:"primary_symbol"`
	PrimaryStartPrice     float64       `json:"primary_start_price,omitempty"`
	PrimaryCurrentPrice   float64       `json:"primary_current_price,omitempty"`
	PrimaryReturnPct      float64       `json:"primary_return_pct,omitempty"`
	BenchmarkSymbol       string        `json:"benchmark_symbol,omitempty"`
	BenchmarkStartPrice   float64       `json:"benchmark_start_price,omitempty"`
	BenchmarkCurrentPrice float64       `json:"benchmark_current_price,omitempty"`
	BenchmarkReturnPct    float64       `json:"benchmark_return_pct,omitempty"`
	RelativeSpreadPct     float64       `json:"relative_spread_pct,omitempty"`
	CompletedPeriods      int           `json:"completed_periods,omitempty"`
	RequiredPeriods       int           `json:"required_periods,omitempty"`
	Summary               string        `json:"summary"`
	Evidence              []SignalRound `json:"evidence"`
}

// AnalyzeAt evaluates current progress for every version-2 market template.
// It intentionally does not mutate the committed end time or claim that an
// in-progress condition is the final settlement result.
func (r *ChainlinkResolver) AnalyzeAt(
	ctx context.Context,
	rule judge.Rule,
	at time.Time,
) (StructuredSignal, error) {
	signal := StructuredSignal{
		RuleType: rule.Type, PrimarySymbol: strings.ToUpper(strings.TrimSpace(rule.Symbol)),
		BenchmarkSymbol:  strings.ToUpper(strings.TrimSpace(rule.Benchmark)),
		CurrentCondition: "PENDING",
	}
	if r == nil || r.repo == nil || r.client == nil {
		return signal, fmt.Errorf("Chainlink signal provider is not configured")
	}
	if err := judge.ValidateVersion2Rule(rule); err != nil {
		return signal, fmt.Errorf("invalid version 2 rule: %w", err)
	}
	if r.xauFeed == (common.Address{}) ||
		!strings.EqualFold(r.xauFeed.Hex(), rule.SourceContract) {
		return signal, fmt.Errorf("configured XAU feed does not match source_contract")
	}

	start := time.Unix(rule.StartTimeSec, 0).UTC()
	end := time.Unix(rule.EndTimeSec, 0).UTC()
	at = at.UTC()
	signal.ObservationTime = at.Format(time.RFC3339)
	if at.Before(start) {
		signal.Status = "NOT_STARTED"
		signal.Summary = fmt.Sprintf("观察期尚未开始；开始时间 %s", start.Format(time.RFC3339))
		return signal, nil
	}
	boundary := at
	if boundary.After(end) {
		boundary = end
		signal.Status = "COMPLETE"
	} else {
		signal.Status = "IN_PROGRESS"
	}
	maxStaleness := time.Duration(rule.MaxStalenessSec) * time.Second

	switch rule.Type {
	case judge.TypePriceThreshold, judge.TypePriceRange:
		current, err := r.signalCandles(ctx, signal.PrimarySymbol, r.xauFeed,
			[]time.Time{boundary}, maxStaleness)
		if err != nil {
			return signal, err
		}
		signal.Evidence = append(signal.Evidence, current.rounds...)
		signal.PrimaryCurrentPrice = current.candles[0].Close
		signal.evaluateCloseRule(rule)
		return signal, nil

	case judge.TypeStreak:
		return r.analyzeStreak(ctx, rule, signal, start, end, boundary, maxStaleness)

	default:
		primary, err := r.signalCandles(ctx, signal.PrimarySymbol, r.xauFeed,
			uniqueBoundaries(start, boundary), maxStaleness)
		if err != nil {
			return signal, err
		}
		signal.Evidence = append(signal.Evidence, primary.rounds...)
		signal.PrimaryStartPrice = primary.candles[0].Close
		signal.PrimaryCurrentPrice = primary.candles[len(primary.candles)-1].Close
		signal.PrimaryReturnPct = percentMove(
			signal.PrimaryStartPrice, signal.PrimaryCurrentPrice)

		switch rule.Type {
		case judge.TypePrice:
			signal.evaluateDirection(rule)
		case judge.TypeReturnThreshold:
			actual := math.Abs(signal.PrimaryReturnPct)
			signal.CurrentCondition = yesNo(compareOrdered(actual, rule.Operator, rule.Threshold))
			signal.Summary = fmt.Sprintf(
				"XAU 当前绝对收益率 %.6f%%，规则 %s %.6f%%；当前条件 %s",
				actual, rule.Operator, rule.Threshold, signal.CurrentCondition)
		case judge.TypeRelative:
			if err := r.addRelativeBenchmark(ctx, rule, &signal, start, boundary, maxStaleness); err != nil {
				return signal, err
			}
		default:
			return signal, fmt.Errorf("unsupported managed signal type %s", rule.Type)
		}
		return signal, nil
	}
}

type loadedSignalCandles struct {
	candles []judge.Candle
	rounds  []SignalRound
}

func (r *ChainlinkResolver) signalCandles(
	ctx context.Context,
	symbol string,
	feed common.Address,
	boundaries []time.Time,
	maxStaleness time.Duration,
) (loadedSignalCandles, error) {
	var zero loadedSignalCandles
	if feed == (common.Address{}) {
		return zero, fmt.Errorf("Chainlink feed for %s is not configured", symbol)
	}
	candles, _, err := r.loadEvidence(ctx, feed, boundaries, maxStaleness)
	if err != nil {
		return zero, err
	}
	rounds := make([]SignalRound, 0, len(candles))
	for _, candle := range candles {
		rounds = append(rounds, SignalRound{
			Symbol: symbol, Boundary: candle.Time.UTC().Format(time.RFC3339),
			SourceTime: candle.SourceTime.UTC().Format(time.RFC3339),
			RoundID:    candle.RoundID, SourceContract: candle.SourceContract,
			PriceUSD: candle.Close,
		})
	}
	return loadedSignalCandles{candles: candles, rounds: rounds}, nil
}

func (r *ChainlinkResolver) addRelativeBenchmark(
	ctx context.Context,
	rule judge.Rule,
	signal *StructuredSignal,
	start, boundary time.Time,
	maxStaleness time.Duration,
) error {
	benchmark := strings.ToUpper(strings.TrimSpace(rule.Benchmark))
	feed := r.benchmarkFeeds[benchmark]
	if feed == (common.Address{}) ||
		!strings.EqualFold(feed.Hex(), rule.BenchmarkSourceContract) {
		return fmt.Errorf("configured %s feed does not match benchmark_source_contract", benchmark)
	}
	loaded, err := r.signalCandles(ctx, benchmark, feed,
		uniqueBoundaries(start, boundary), maxStaleness)
	if err != nil {
		return fmt.Errorf("%s benchmark: %w", benchmark, err)
	}
	signal.Evidence = append(signal.Evidence, loaded.rounds...)
	signal.BenchmarkStartPrice = loaded.candles[0].Close
	signal.BenchmarkCurrentPrice = loaded.candles[len(loaded.candles)-1].Close
	signal.BenchmarkReturnPct = percentMove(
		signal.BenchmarkStartPrice, signal.BenchmarkCurrentPrice)
	signal.RelativeSpreadPct = signal.PrimaryReturnPct - signal.BenchmarkReturnPct
	signal.CurrentCondition = yesNo(signal.RelativeSpreadPct > 0)
	signal.Summary = fmt.Sprintf(
		"同期 XAU 收益率 %.6f%%，%s 收益率 %.6f%%，相对收益 %+.6f%%；当前条件 %s",
		signal.PrimaryReturnPct, benchmark, signal.BenchmarkReturnPct,
		signal.RelativeSpreadPct, signal.CurrentCondition)
	return nil
}

func (r *ChainlinkResolver) analyzeStreak(
	ctx context.Context,
	rule judge.Rule,
	signal StructuredSignal,
	start, end, boundary time.Time,
	maxStaleness time.Duration,
) (StructuredSignal, error) {
	location, err := time.LoadLocation(judge.BeijingTimezone)
	if err != nil {
		return signal, err
	}
	latestComplete := boundary.In(location)
	latestComplete = time.Date(
		latestComplete.Year(), latestComplete.Month(), latestComplete.Day(),
		0, 0, 0, 0, location,
	).UTC()
	if latestComplete.After(end) {
		latestComplete = end
	}
	boundaries := make([]time.Time, 0, rule.StreakDays+1)
	for current := start; !current.After(latestComplete); current = current.Add(24 * time.Hour) {
		boundaries = append(boundaries, current)
	}
	if len(boundaries) == 0 {
		signal.Summary = "尚无已完成的整日边界"
		return signal, nil
	}
	loaded, err := r.signalCandles(ctx, signal.PrimarySymbol, r.xauFeed,
		boundaries, maxStaleness)
	if err != nil {
		return signal, err
	}
	signal.Evidence = append(signal.Evidence, loaded.rounds...)
	signal.PrimaryStartPrice = loaded.candles[0].Close
	signal.PrimaryCurrentPrice = loaded.candles[len(loaded.candles)-1].Close
	signal.PrimaryReturnPct = percentMove(signal.PrimaryStartPrice, signal.PrimaryCurrentPrice)
	signal.CompletedPeriods = len(loaded.candles) - 1
	signal.RequiredPeriods = rule.StreakDays
	matched := true
	direction := strings.ToUpper(strings.TrimSpace(rule.Direction))
	for index := 1; index < len(loaded.candles); index++ {
		previous := loaded.candles[index-1].Close
		current := loaded.candles[index].Close
		if direction == "UP" && current <= previous || direction == "DOWN" && current >= previous {
			matched = false
			break
		}
	}
	switch {
	case !matched:
		signal.CurrentCondition = "NO"
	case signal.CompletedPeriods >= signal.RequiredPeriods:
		signal.CurrentCondition = "YES"
	default:
		signal.CurrentCondition = "PENDING"
	}
	signal.Summary = fmt.Sprintf(
		"%s 连续方向已完成 %d/%d 个整日区间；当前条件 %s",
		direction, signal.CompletedPeriods, signal.RequiredPeriods, signal.CurrentCondition)
	return signal, nil
}

func (s *StructuredSignal) evaluateDirection(rule judge.Rule) {
	change := s.PrimaryReturnPct
	tolerance := rule.FlatTolerance
	var matched bool
	switch strings.ToUpper(strings.TrimSpace(rule.Direction)) {
	case "UP":
		matched = change > tolerance
	case "DOWN":
		matched = change < -tolerance
	case "FLAT":
		matched = math.Abs(change) <= tolerance
	}
	s.CurrentCondition = yesNo(matched)
	s.Summary = fmt.Sprintf(
		"XAU 当前收益率 %+.6f%%，方向 %s，横盘容差 %.6f%%；当前条件 %s",
		change, rule.Direction, tolerance, s.CurrentCondition)
}

func (s *StructuredSignal) evaluateCloseRule(rule judge.Rule) {
	switch rule.Type {
	case judge.TypePriceThreshold:
		s.CurrentCondition = yesNo(compareOrdered(
			s.PrimaryCurrentPrice, rule.Operator, rule.Threshold))
		s.Summary = fmt.Sprintf(
			"XAU 当前价格 %.6f，规则 %s %.6f；当前条件 %s",
			s.PrimaryCurrentPrice, rule.Operator, rule.Threshold, s.CurrentCondition)
	case judge.TypePriceRange:
		inside := s.PrimaryCurrentPrice >= rule.LowerThreshold &&
			s.PrimaryCurrentPrice <= rule.UpperThreshold
		if strings.EqualFold(rule.Operator, "OUTSIDE_RANGE") {
			inside = !inside
		}
		s.CurrentCondition = yesNo(inside)
		s.Summary = fmt.Sprintf(
			"XAU 当前价格 %.6f，规则 %s [%.6f, %.6f]；当前条件 %s",
			s.PrimaryCurrentPrice, rule.Operator, rule.LowerThreshold,
			rule.UpperThreshold, s.CurrentCondition)
	}
}

func uniqueBoundaries(start, current time.Time) []time.Time {
	if current.Equal(start) {
		return []time.Time{start}
	}
	return []time.Time{start, current}
}

func percentMove(start, current float64) float64 {
	if start <= 0 {
		return 0
	}
	return (current - start) / start * 100
}

func compareOrdered(actual float64, operator string, threshold float64) bool {
	switch strings.ToUpper(strings.TrimSpace(operator)) {
	case "GT", "GREATER_THAN":
		return actual > threshold
	case "GTE", "GREATER_THAN_OR_EQUAL":
		return actual >= threshold
	case "LT", "LESS_THAN":
		return actual < threshold
	case "LTE", "LESS_THAN_OR_EQUAL":
		return actual <= threshold
	default:
		return false
	}
}

func yesNo(value bool) string {
	if value {
		return "YES"
	}
	return "NO"
}
