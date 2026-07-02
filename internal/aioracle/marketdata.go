package aioracle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"PredictionMarket/internal/oracle"
)

// GoldEvidenceFetcher exposes a live gold quote as evidence for gold-related
// markets. It never decides the winner; the configured AI consensus still
// evaluates the market condition against this evidence.
type GoldEvidenceFetcher struct {
	oracle *oracle.GoldOracle
}

func NewGoldEvidenceFetcher(goldOracle *oracle.GoldOracle) *GoldEvidenceFetcher {
	return &GoldEvidenceFetcher{oracle: goldOracle}
}

func (f *GoldEvidenceFetcher) Fetch(_ context.Context, keywords []string, _ time.Time, maxArticles int) ([]NewsArticle, error) {
	if f == nil || f.oracle == nil || maxArticles <= 0 || !containsGoldKeyword(keywords) {
		return nil, nil
	}
	quote, err := f.oracle.FetchQuote()
	if err != nil {
		return nil, fmt.Errorf("fetch live gold evidence: %w", err)
	}
	return []NewsArticle{{
		Title:       "Live XAU/USD market quote",
		Source:      quote.QuoteSource,
		PublishedAt: time.Now(),
		Content: fmt.Sprintf(
			"XAU/USD current price is %.4f USD; change versus previous close is %.4f%%; source update time is %s.",
			quote.PriceUSD,
			quote.Change24h,
			quote.QuoteUpdatedAt,
		),
	}}, nil
}

func containsGoldKeyword(keywords []string) bool {
	for _, keyword := range keywords {
		lower := strings.ToLower(keyword)
		if strings.Contains(lower, "gold") ||
			strings.Contains(lower, "xau") ||
			strings.Contains(keyword, "黄金") ||
			strings.Contains(keyword, "金价") {
			return true
		}
	}
	return false
}
