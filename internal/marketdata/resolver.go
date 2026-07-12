package marketdata

import (
	"context"
	"strings"
	"time"

	"PredictionMarket/internal/judge"
)

type StructuredResolver struct {
	gold *GoldAPIClient
}

func NewStructuredResolver(gold *GoldAPIClient) *StructuredResolver {
	return &StructuredResolver{gold: gold}
}

func (r *StructuredResolver) Resolve(ctx context.Context, rule judge.Rule) judge.Result {
	if r == nil || r.gold == nil || !r.gold.Available() {
		return judge.Result{Winner: -1, Summary: "historical market data provider is not configured"}
	}
	start, end := time.Unix(rule.StartTimeSec, 0).UTC(), time.Unix(rule.EndTimeSec, 0).UTC()
	var evidence judge.Evidence
	var err error

	switch rule.Type {
	case judge.TypeTechnical:
		groupBy := strings.ToLower(strings.TrimSpace(rule.Interval))
		if groupBy == "" {
			groupBy = "hour"
		}
		evidence.Primary, err = r.gold.History(ctx, firstNonEmpty(rule.Symbol, "XAU"), groupBy, start, end)
	case judge.TypeVolume:
		return judge.Result{Winner: -1, Summary: "volume markets require an exchange-specific volume provider"}
	default:
		evidence.Primary, err = r.gold.OHLC(ctx, firstNonEmpty(rule.Symbol, "XAU"), start, end)
	}
	if err != nil {
		return judge.Result{Winner: -1, Summary: err.Error()}
	}
	if rule.Type == judge.TypeRelative {
		evidence.Benchmark, err = r.gold.OHLC(ctx, rule.Benchmark, start, end)
		if err != nil {
			return judge.Result{Winner: -1, Summary: "benchmark: " + err.Error()}
		}
	}
	return judge.EvaluateStructured(rule, evidence)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
