package judge

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var version2Types = map[string]bool{
	TypePrice: true, TypeReturnThreshold: true, TypePriceThreshold: true,
	TypePriceRange: true, TypeRelative: true, TypeStreak: true,
}

func IsVersion2Type(ruleType string) bool {
	return version2Types[strings.ToUpper(strings.TrimSpace(ruleType))]
}

func ValidateVersion2Rule(rule Rule) error {
	if rule.RuleVersion != 2 {
		return errors.New("rule_version must be 2")
	}
	if !version2Types[rule.Type] {
		return fmt.Errorf("unsupported version 2 rule type %q", rule.Type)
	}
	if !strings.EqualFold(strings.TrimSpace(rule.Symbol), "XAU") {
		return errors.New("symbol must be XAU")
	}
	if rule.Source != ChainlinkDataFeedEthereum {
		return fmt.Errorf("source must be %s", ChainlinkDataFeedEthereum)
	}
	if !strings.EqualFold(strings.TrimSpace(rule.SourceContract), ChainlinkXAUUSDFeed) {
		return errors.New("source_contract must be the Ethereum XAU/USD Chainlink feed")
	}
	if rule.Timezone != BeijingTimezone {
		return fmt.Errorf("timezone must be %s", BeijingTimezone)
	}
	if rule.BoundaryPolicy != LastAtOrBefore {
		return fmt.Errorf("boundary_policy must be %s", LastAtOrBefore)
	}
	if rule.MaxStalenessSec < 3600 || rule.MaxStalenessSec > 43200 {
		return errors.New("max_staleness_sec must be between 3600 and 43200")
	}
	location, err := time.LoadLocation(BeijingTimezone)
	if err != nil {
		return fmt.Errorf("load Beijing timezone: %w", err)
	}
	start, end := time.Unix(rule.StartTimeSec, 0).In(location), time.Unix(rule.EndTimeSec, 0).In(location)
	if !isMidnight(start) || !isMidnight(end) {
		return errors.New("start_time_sec and end_time_sec must be Beijing midnight")
	}
	if !end.After(start) || end.Sub(start) < 24*time.Hour {
		return errors.New("observation window must be at least one day")
	}
	if end.Sub(start) > 4*24*time.Hour {
		return errors.New("observation window must be no more than four days")
	}
	if !isSettlementWeekday(start.Weekday()) || !isSettlementWeekday(end.Weekday()) {
		return errors.New("Beijing boundary weekday must be Tuesday through Saturday")
	}

	switch rule.Type {
	case TypePrice:
		if !oneOf(rule.Direction, "UP", "DOWN", "FLAT") {
			return errors.New("direction must be UP, DOWN, or FLAT")
		}
		if rule.FlatTolerance <= 0 || rule.FlatTolerance > 5 {
			return errors.New("flat_tolerance_percent must be greater than 0 and no more than 5")
		}
	case TypeReturnThreshold:
		if err := validateOrderedThreshold(rule.Operator, rule.Threshold, "return threshold"); err != nil {
			return err
		}
	case TypePriceThreshold:
		if err := validateOrderedThreshold(rule.Operator, rule.Threshold, "price threshold"); err != nil {
			return err
		}
	case TypePriceRange:
		if !oneOf(rule.Operator, "IN_RANGE", "OUTSIDE_RANGE") {
			return errors.New("price range operator must be IN_RANGE or OUTSIDE_RANGE")
		}
		if invalidPositive(rule.LowerThreshold) || invalidPositive(rule.UpperThreshold) || rule.UpperThreshold <= rule.LowerThreshold {
			return errors.New("price range thresholds must be positive and upper_threshold must exceed lower_threshold")
		}
	case TypeRelative:
		expectedFeed, ok := RelativeBenchmarkFeed(rule.Benchmark)
		if !ok {
			return errors.New("benchmark must be BTC, ETH, SOL, or BNB")
		}
		if !strings.EqualFold(strings.TrimSpace(rule.BenchmarkSourceContract), expectedFeed) {
			return fmt.Errorf("benchmark_source_contract must match the Ethereum %s/USD Chainlink feed",
				strings.ToUpper(strings.TrimSpace(rule.Benchmark)))
		}
	case TypeStreak:
		if !oneOf(rule.Direction, "UP", "DOWN") {
			return errors.New("streak direction must be UP or DOWN")
		}
		if rule.StreakDays < 1 || rule.StreakDays > 30 {
			return errors.New("streak_days must be between 1 and 30")
		}
		if int(end.Sub(start)/(24*time.Hour)) != rule.StreakDays {
			return errors.New("streak_days must equal the committed observation window")
		}
	}
	return nil
}

func isMidnight(value time.Time) bool {
	return value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 && value.Nanosecond() == 0
}

func isSettlementWeekday(day time.Weekday) bool {
	return day >= time.Tuesday && day <= time.Saturday
}

func oneOf(value string, allowed ...string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateOrderedThreshold(operator string, threshold float64, label string) error {
	if !oneOf(operator, "GT", "GTE", "LT", "LTE", "GREATER_THAN", "GREATER_THAN_OR_EQUAL", "LESS_THAN", "LESS_THAN_OR_EQUAL") {
		return fmt.Errorf("%s operator must be an ordered comparison", label)
	}
	if invalidPositive(threshold) {
		return fmt.Errorf("%s must be positive", label)
	}
	return nil
}

func invalidPositive(value float64) bool {
	return value <= 0 || math.IsNaN(value) || math.IsInf(value, 0)
}
