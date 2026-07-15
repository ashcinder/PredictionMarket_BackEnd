package marketdata

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"PredictionMarket/internal/judge"
)

// GoldSample is one auditable live XAU/USD observation persisted by the backend.
type GoldSample struct {
	Symbol     string
	ObservedAt time.Time
	Price      float64
	Source     string
}

type GoldSampleRepository interface {
	Append(context.Context, GoldSample) error
	Range(context.Context, string, time.Time, time.Time) ([]GoldSample, error)
}

// SampledGoldSource reconstructs an open/close observation from live samples.
// It is suitable for close-price and return markets, but not for proving an
// intraperiod high/low touch between two samples.
type SampledGoldSource struct {
	repository GoldSampleRepository
	maxGap     time.Duration
}

func NewSampledGoldSource(repository GoldSampleRepository, maxGap time.Duration) *SampledGoldSource {
	return &SampledGoldSource{repository: repository, maxGap: maxGap}
}

func (s *SampledGoldSource) Available() bool {
	return s != nil && s.repository != nil && s.maxGap > 0
}

func (s *SampledGoldSource) OHLC(ctx context.Context, symbol string, start, end time.Time) ([]judge.Candle, error) {
	if !s.Available() {
		return nil, fmt.Errorf("local XAU sample repository is not configured")
	}
	if !end.After(start) {
		return nil, fmt.Errorf("local XAU sample window is invalid")
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		symbol = "XAU"
	}
	samples, err := s.repository.Range(ctx, symbol, start.Add(-s.maxGap), end.Add(s.maxGap))
	if err != nil {
		return nil, fmt.Errorf("read local XAU samples: %w", err)
	}
	valid := make([]GoldSample, 0, len(samples))
	for _, sample := range samples {
		if !strings.EqualFold(strings.TrimSpace(sample.Symbol), symbol) || sample.Price <= 0 ||
			math.IsNaN(sample.Price) || math.IsInf(sample.Price, 0) {
			continue
		}
		valid = append(valid, sample)
	}
	if len(valid) < 2 {
		return nil, fmt.Errorf("local XAU samples need at least two observations")
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].ObservedAt.Before(valid[j].ObservedAt) })
	first := nearestSample(valid, start)
	last := nearestSample(valid, end)
	if distance(first.ObservedAt, start) > s.maxGap {
		return nil, fmt.Errorf("local XAU samples do not cover window start (nearest %s, max gap %s)",
			first.ObservedAt.UTC().Format(time.RFC3339), s.maxGap)
	}
	if distance(last.ObservedAt, end) > s.maxGap {
		return nil, fmt.Errorf("local XAU samples do not cover window end (nearest %s, max gap %s)",
			last.ObservedAt.UTC().Format(time.RFC3339), s.maxGap)
	}
	if !last.ObservedAt.After(first.ObservedAt) {
		return nil, fmt.Errorf("local XAU samples do not contain distinct start and end observations")
	}

	high, low := first.Price, first.Price
	for _, sample := range valid {
		if sample.ObservedAt.Before(first.ObservedAt) || sample.ObservedAt.After(last.ObservedAt) {
			continue
		}
		high = math.Max(high, sample.Price)
		low = math.Min(low, sample.Price)
	}
	return []judge.Candle{{
		Time: start, Open: first.Price, High: high, Low: low, Close: last.Price,
	}}, nil
}

// DeadlineClose returns the nearest quote to the committed deadline. A
// threshold market only depends on this value, so no start observation is
// required.
func (s *SampledGoldSource) DeadlineClose(ctx context.Context, symbol string, start, end time.Time) ([]judge.Candle, error) {
	if !s.Available() {
		return nil, fmt.Errorf("local XAU sample repository is not configured")
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		symbol = "XAU"
	}
	samples, err := s.repository.Range(ctx, symbol, end.Add(-s.maxGap), end.Add(s.maxGap))
	if err != nil {
		return nil, fmt.Errorf("read local XAU samples: %w", err)
	}
	valid := make([]GoldSample, 0, len(samples))
	for _, sample := range samples {
		if strings.EqualFold(sample.Symbol, symbol) && sample.Price > 0 &&
			!math.IsNaN(sample.Price) && !math.IsInf(sample.Price, 0) {
			valid = append(valid, sample)
		}
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("local XAU samples do not cover window end")
	}
	nearest := nearestSample(valid, end)
	if distance(nearest.ObservedAt, end) > s.maxGap {
		return nil, fmt.Errorf("local XAU samples do not cover window end (nearest %s, max gap %s)",
			nearest.ObservedAt.UTC().Format(time.RFC3339), s.maxGap)
	}
	return []judge.Candle{{
		Time: start, Open: nearest.Price, High: nearest.Price, Low: nearest.Price, Close: nearest.Price,
	}}, nil
}

func nearestSample(samples []GoldSample, target time.Time) GoldSample {
	nearest := samples[0]
	best := distance(nearest.ObservedAt, target)
	for _, sample := range samples[1:] {
		if candidate := distance(sample.ObservedAt, target); candidate < best {
			nearest, best = sample, candidate
		}
	}
	return nearest
}

func distance(left, right time.Time) time.Duration {
	delta := left.Sub(right)
	if delta < 0 {
		return -delta
	}
	return delta
}
