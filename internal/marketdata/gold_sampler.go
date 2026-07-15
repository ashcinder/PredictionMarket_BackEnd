package marketdata

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"PredictionMarket/internal/oracle"
)

type goldQuoteFetcher interface {
	FetchQuote() (*oracle.Quote, error)
}

// GoldSampleRecorder continuously persists XAU/USD quotes for short-window
// settlement when a separate historical-data subscription is unavailable.
type GoldSampleRecorder struct {
	repository GoldSampleRepository
	fetcher    goldQuoteFetcher
	interval   time.Duration
	now        func() time.Time
}

func NewGoldSampleRecorder(repository GoldSampleRepository, fetcher goldQuoteFetcher, interval time.Duration) *GoldSampleRecorder {
	return &GoldSampleRecorder{
		repository: repository,
		fetcher:    fetcher,
		interval:   interval,
		now:        time.Now,
	}
}

func (r *GoldSampleRecorder) Run(ctx context.Context) error {
	if r == nil || r.repository == nil || r.fetcher == nil || r.interval <= 0 {
		return fmt.Errorf("gold oracle sampler is not configured")
	}
	slog.Info("gold oracle sampler started",
		"interval", r.interval,
		"symbol", "XAU",
		"logic_summary", "持续保存 XAU/USD 实时报价，为短周期价格和跑赢率市场提供可复算起止证据",
	)
	if err := r.recordOnce(ctx); err != nil {
		slog.Warn("gold oracle sample failed", "error", err)
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("gold oracle sampler stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := r.recordOnce(ctx); err != nil {
				slog.Warn("gold oracle sample failed", "error", err)
			}
		}
	}
}

func (r *GoldSampleRecorder) recordOnce(ctx context.Context) error {
	quote, err := r.fetcher.FetchQuote()
	if err != nil {
		return fmt.Errorf("fetch XAU/USD quote: %w", err)
	}
	if quote == nil || quote.PriceUSD <= 0 || math.IsNaN(quote.PriceUSD) || math.IsInf(quote.PriceUSD, 0) {
		return fmt.Errorf("fetch XAU/USD quote: invalid price")
	}
	return r.repository.Append(ctx, GoldSample{
		Symbol:     "XAU",
		ObservedAt: quoteObservedAt(quote.QuoteUpdatedAt, r.now()),
		Price:      quote.PriceUSD,
		Source:     strings.TrimSpace(quote.QuoteSource),
	})
}

func quoteObservedAt(raw string, fallback time.Time) time.Time {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed.UTC()
		}
	}
	return fallback.UTC()
}
