package oracle

import (
	"context"
	"sync"
	"testing"
	"time"

	appcache "PredictionMarket/internal/cache"

	"github.com/alicebob/miniredis/v2"
)

type countingQuoteProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *countingQuoteProvider) FetchQuote() (*Quote, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return &Quote{
		PriceUSD:       2400 + float64(p.calls),
		QuoteSource:    "test",
		QuoteUpdatedAt: "2026-07-28T00:00:00Z",
	}, nil
}

func TestCachedQuoteProviderUsesRedisUntilTTLExpires(t *testing.T) {
	server := miniredis.RunT(t)
	store, err := appcache.NewRedisStore(context.Background(), appcache.RedisConfig{
		Address:          server.Addr(),
		KeyPrefix:        "quote-test",
		OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	upstream := &countingQuoteProvider{}
	provider := NewCachedQuoteProvider(upstream, store, 10*time.Second)
	first, err := provider.FetchQuote()
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.FetchQuote()
	if err != nil {
		t.Fatal(err)
	}
	if upstream.calls != 1 || first.PriceUSD != second.PriceUSD {
		t.Fatalf("cache miss: calls=%d first=%v second=%v", upstream.calls, first, second)
	}

	server.FastForward(11 * time.Second)
	third, err := provider.FetchQuote()
	if err != nil {
		t.Fatal(err)
	}
	if upstream.calls != 2 || third.PriceUSD == first.PriceUSD {
		t.Fatalf("expired quote was not refreshed: calls=%d third=%v", upstream.calls, third)
	}
}
