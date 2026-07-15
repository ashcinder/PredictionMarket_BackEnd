package marketdata

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"PredictionMarket/internal/judge"
	"PredictionMarket/internal/oracle"
)

type memoryGoldSamples struct {
	samples []GoldSample
}

func (m *memoryGoldSamples) Append(_ context.Context, sample GoldSample) error {
	m.samples = append(m.samples, sample)
	return nil
}

func (m *memoryGoldSamples) Range(_ context.Context, symbol string, start, end time.Time) ([]GoldSample, error) {
	var result []GoldSample
	for _, sample := range m.samples {
		if strings.EqualFold(sample.Symbol, symbol) && !sample.ObservedAt.Before(start) && !sample.ObservedAt.After(end) {
			result = append(result, sample)
		}
	}
	return result, nil
}

type fixedHistoricalSource struct {
	available bool
	candles   []judge.Candle
}

func (s fixedHistoricalSource) Available() bool { return s.available }

func (s fixedHistoricalSource) OHLC(_ context.Context, _ string, _, _ time.Time) ([]judge.Candle, error) {
	return s.candles, nil
}

func TestStructuredResolverUsesCoveredLocalSamplesForShortRelativeMarket(t *testing.T) {
	start := time.Date(2026, 7, 14, 7, 8, 26, 0, time.UTC)
	end := time.Date(2026, 7, 14, 7, 10, 0, 0, time.UTC)
	repository := &memoryGoldSamples{samples: []GoldSample{
		{Symbol: "XAU", ObservedAt: start.Add(3 * time.Second), Price: 4000, Source: "gold-api.com"},
		{Symbol: "XAU", ObservedAt: start.Add(50 * time.Second), Price: 4004, Source: "gold-api.com"},
		{Symbol: "XAU", ObservedAt: end.Add(-4 * time.Second), Price: 4008, Source: "gold-api.com"},
	}}
	btc := fixedHistoricalSource{available: true, candles: []judge.Candle{
		{Time: start, Open: 100, High: 100, Low: 100, Close: 100},
		{Time: end, Open: 100, High: 100.1, Low: 100, Close: 100.1},
	}}

	resolver := NewStructuredResolverWithSamples(
		NewGoldAPIClient("https://api.gold-api.com", "", time.Second),
		btc,
		NewSampledGoldSource(repository, 15*time.Second),
	)
	result := resolver.Resolve(context.Background(), judge.Rule{
		Type: judge.TypeRelative, Symbol: "XAU", Benchmark: "BTC", Source: "GOLD_API",
		StartTimeSec: start.Unix(), EndTimeSec: end.Unix(),
	})
	if !result.Determinate || result.Winner != 0 {
		t.Fatalf("covered local samples should resolve YES: %+v", result)
	}
	if !strings.Contains(result.Summary, "LOCAL_XAU_SAMPLES") {
		t.Fatalf("audit summary should identify sampled evidence: %s", result.Summary)
	}
}

func TestStructuredResolverRejectsRelativeSamplesThatMissWindowStart(t *testing.T) {
	start := time.Date(2026, 7, 14, 7, 8, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)
	repository := &memoryGoldSamples{samples: []GoldSample{
		{Symbol: "XAU", ObservedAt: start.Add(40 * time.Second), Price: 4000, Source: "gold-api.com"},
		{Symbol: "XAU", ObservedAt: end.Add(-2 * time.Second), Price: 4002, Source: "gold-api.com"},
	}}

	resolver := NewStructuredResolverWithSamples(
		NewGoldAPIClient("https://api.gold-api.com", "", time.Second),
		fixedHistoricalSource{available: true, candles: []judge.Candle{
			{Time: start, Open: 100, High: 100, Low: 100, Close: 100},
			{Time: end, Open: 100, High: 101, Low: 100, Close: 101},
		}},
		NewSampledGoldSource(repository, 15*time.Second),
	)
	result := resolver.Resolve(context.Background(), judge.Rule{
		Type: judge.TypeRelative, Symbol: "XAU", Benchmark: "BTC", Source: "GOLD_API",
		StartTimeSec: start.Unix(), EndTimeSec: end.Unix(),
	})
	if result.Determinate || !strings.Contains(result.Summary, "window start") {
		t.Fatalf("missing start coverage must remain unresolved: %+v", result)
	}
}

func TestStructuredResolverPriceThresholdOnlyNeedsDeadlineSample(t *testing.T) {
	start := time.Date(2026, 7, 14, 7, 8, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)
	repository := &memoryGoldSamples{samples: []GoldSample{
		{Symbol: "XAU", ObservedAt: end.Add(-3 * time.Second), Price: 4016.8, Source: "gold-api.com"},
	}}
	resolver := NewStructuredResolverWithSamples(
		NewGoldAPIClient("https://api.gold-api.com", "", time.Second),
		fixedHistoricalSource{},
		NewSampledGoldSource(repository, 15*time.Second),
	)
	result := resolver.Resolve(context.Background(), judge.Rule{
		Type: judge.TypePriceThreshold, Symbol: "XAU", Source: "GOLD_API",
		StartTimeSec: start.Unix(), EndTimeSec: end.Unix(), Operator: "GT", Threshold: 10,
	})
	if !result.Determinate || result.Winner != 0 {
		t.Fatalf("deadline quote should settle obvious threshold: %+v", result)
	}
}

func TestStructuredResolverDoesNotUsePointSamplesForTouchMarket(t *testing.T) {
	start := time.Date(2026, 7, 14, 7, 8, 0, 0, time.UTC)
	repository := &memoryGoldSamples{samples: []GoldSample{
		{Symbol: "XAU", ObservedAt: start, Price: 4000, Source: "gold-api.com"},
		{Symbol: "XAU", ObservedAt: start.Add(time.Minute), Price: 4010, Source: "gold-api.com"},
	}}
	resolver := NewStructuredResolverWithSamples(
		NewGoldAPIClient("https://api.gold-api.com", "", time.Second),
		fixedHistoricalSource{},
		NewSampledGoldSource(repository, 15*time.Second),
	)
	result := resolver.Resolve(context.Background(), judge.Rule{
		Type: judge.TypeTouch, Symbol: "XAU", Source: "GOLD_API",
		StartTimeSec: start.Unix(), EndTimeSec: start.Add(time.Minute).Unix(), Threshold: 4005,
	})
	if result.Determinate || !strings.Contains(result.Summary, "historical") {
		t.Fatalf("sampled points cannot prove intraperiod touch: %+v", result)
	}
}

type fixedGoldQuoteFetcher struct {
	quote *oracle.Quote
	err   error
}

func (f fixedGoldQuoteFetcher) FetchQuote() (*oracle.Quote, error) { return f.quote, f.err }

func TestGoldSampleRecorderPersistsValidQuote(t *testing.T) {
	repository := &memoryGoldSamples{}
	recorder := NewGoldSampleRecorder(repository, fixedGoldQuoteFetcher{quote: &oracle.Quote{
		PriceUSD: 4016.8, QuoteSource: "gold-api.com", QuoteUpdatedAt: "2026-07-14T09:39:31Z",
	}}, 10*time.Second)
	recorder.now = func() time.Time {
		return time.Date(2026, 7, 14, 9, 39, 32, 0, time.UTC)
	}
	if err := recorder.recordOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repository.samples) != 1 || repository.samples[0].Price != 4016.8 ||
		repository.samples[0].Source != "gold-api.com" ||
		!repository.samples[0].ObservedAt.Equal(time.Date(2026, 7, 14, 9, 39, 31, 0, time.UTC)) {
		t.Fatalf("unexpected persisted sample: %+v", repository.samples)
	}
}

func TestGoldSampleRecorderRejectsUnavailableQuote(t *testing.T) {
	recorder := NewGoldSampleRecorder(&memoryGoldSamples{}, fixedGoldQuoteFetcher{err: errors.New("offline")}, 10*time.Second)
	if err := recorder.recordOnce(context.Background()); err == nil {
		t.Fatal("expected quote error")
	}
}
