package oracle

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	appcache "PredictionMarket/internal/cache"
)

const goldQuoteCacheKey = "gold:quote:v1"

type QuoteProvider interface {
	FetchQuote() (*Quote, error)
}

// CachedQuoteProvider applies cache-aside only to the user-facing quote route.
// Trading, sampling and settlement continue to use the uncached GoldOracle.
type CachedQuoteProvider struct {
	next  QuoteProvider
	cache appcache.Store
	ttl   time.Duration
}

func NewCachedQuoteProvider(next QuoteProvider, store appcache.Store, ttl time.Duration) *CachedQuoteProvider {
	return &CachedQuoteProvider{next: next, cache: store, ttl: ttl}
}

func (p *CachedQuoteProvider) FetchQuote() (*Quote, error) {
	ctx := context.Background()
	if raw, hit, err := p.cache.Get(ctx, goldQuoteCacheKey); err == nil && hit {
		var quote Quote
		if json.Unmarshal(raw, &quote) == nil && quote.PriceUSD > 0 {
			slog.Info("redis cache hit", "component", "gold_quote")
			return &quote, nil
		}
	} else if err != nil {
		slog.Debug("redis cache read failed", "component", "gold_quote", "error", err)
	}

	quote, err := p.next.FetchQuote()
	if err != nil || quote == nil {
		return quote, err
	}
	if raw, marshalErr := json.Marshal(quote); marshalErr == nil {
		if cacheErr := p.cache.Set(ctx, goldQuoteCacheKey, raw, p.ttl); cacheErr != nil {
			slog.Debug("redis cache write failed", "component", "gold_quote", "error", cacheErr)
		}
	}
	return quote, nil
}
