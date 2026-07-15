package marketdata

import (
	"context"
	"fmt"
	"strings"
	"time"

	"PredictionMarket/internal/judge"
)

type StructuredResolver struct {
	gold    goldHistoricalSource
	bitcoin historicalOHLCSource
	sampled *SampledGoldSource
}

type VersionedResolver struct {
	legacy    *StructuredResolver
	chainlink *ChainlinkResolver
}

func NewVersionedResolver(legacy *StructuredResolver, chainlink *ChainlinkResolver) *VersionedResolver {
	return &VersionedResolver{legacy: legacy, chainlink: chainlink}
}

func (r *VersionedResolver) Resolve(ctx context.Context, rule judge.Rule) judge.Result {
	if r == nil {
		return judge.Result{Winner: -1, Summary: "market data resolver is not configured"}
	}
	if rule.RuleVersion >= 2 {
		if r.chainlink == nil {
			return judge.Result{Winner: -1, Summary: "Chainlink market data resolver is not configured"}
		}
		return r.chainlink.Resolve(ctx, rule)
	}
	if r.legacy == nil {
		return judge.Result{Winner: -1, Summary: "legacy market data resolver is not configured"}
	}
	return r.legacy.Resolve(ctx, rule)
}

type goldHistoricalSource interface {
	historicalOHLCSource
	History(context.Context, string, string, time.Time, time.Time) ([]judge.Candle, error)
}

type historicalOHLCSource interface {
	Available() bool
	OHLC(context.Context, string, time.Time, time.Time) ([]judge.Candle, error)
}

func NewStructuredResolver(gold goldHistoricalSource, bitcoin historicalOHLCSource) *StructuredResolver {
	return &StructuredResolver{gold: gold, bitcoin: bitcoin}
}

func NewStructuredResolverWithSamples(gold goldHistoricalSource, bitcoin historicalOHLCSource, sampled *SampledGoldSource) *StructuredResolver {
	return &StructuredResolver{gold: gold, bitcoin: bitcoin, sampled: sampled}
}

func (r *StructuredResolver) Resolve(ctx context.Context, rule judge.Rule) judge.Result {
	if r == nil {
		return judge.Result{Winner: -1, Summary: "historical market data provider is not configured"}
	}
	start, end := time.Unix(rule.StartTimeSec, 0).UTC(), time.Unix(rule.EndTimeSec, 0).UTC()
	var evidence judge.Evidence
	var err error
	goldSource := r.gold
	evidenceSource := "HISTORICAL_GOLD_API"
	usingSamples := false
	if goldSource == nil || !goldSource.Available() {
		if !allowsSampledGold(rule.Type) || r.sampled == nil || !r.sampled.Available() {
			return judge.Result{Winner: -1, Summary: "historical market data provider is not configured"}
		}
		goldSource = sampledGoldAdapter{r.sampled}
		evidenceSource = "LOCAL_XAU_SAMPLES"
		usingSamples = true
	}

	switch rule.Type {
	case judge.TypeTechnical:
		groupBy := strings.ToLower(strings.TrimSpace(rule.Interval))
		if groupBy == "" {
			groupBy = "hour"
		}
		evidence.Primary, err = goldSource.History(ctx, firstNonEmpty(rule.Symbol, "XAU"), groupBy, start, end)
	case judge.TypeVolume:
		return judge.Result{Winner: -1, Summary: "volume markets require an exchange-specific volume provider"}
	case judge.TypePriceThreshold:
		if usingSamples {
			evidence.Primary, err = r.sampled.DeadlineClose(ctx, firstNonEmpty(rule.Symbol, "XAU"), start, end)
		} else {
			evidence.Primary, err = goldSource.OHLC(ctx, firstNonEmpty(rule.Symbol, "XAU"), start, end)
		}
	default:
		evidence.Primary, err = goldSource.OHLC(ctx, firstNonEmpty(rule.Symbol, "XAU"), start, end)
	}
	if err != nil {
		return judge.Result{Winner: -1, Summary: err.Error()}
	}
	if rule.Type == judge.TypeRelative {
		if !strings.EqualFold(strings.TrimSpace(rule.Benchmark), "BTC") {
			return judge.Result{Winner: -1, Summary: "unsupported relative benchmark " + rule.Benchmark}
		}
		if r.bitcoin == nil || !r.bitcoin.Available() {
			return judge.Result{Winner: -1, Summary: "Bitcoin historical market data provider is not configured"}
		}
		evidence.Benchmark, err = r.bitcoin.OHLC(ctx, rule.Benchmark, start, end)
		if err != nil {
			return judge.Result{Winner: -1, Summary: "benchmark: " + err.Error()}
		}
	}
	result := judge.EvaluateStructured(rule, evidence)
	result.Summary = "evidence_source=" + evidenceSource + "; " + result.Summary
	return result
}

func allowsSampledGold(ruleType string) bool {
	switch ruleType {
	case judge.TypePrice, judge.TypePriceThreshold, judge.TypeRelative:
		return true
	default:
		return false
	}
}

type sampledGoldAdapter struct{ historicalOHLCSource }

func (s sampledGoldAdapter) History(context.Context, string, string, time.Time, time.Time) ([]judge.Candle, error) {
	return nil, fmt.Errorf("local sampled evidence does not support indicator history")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
