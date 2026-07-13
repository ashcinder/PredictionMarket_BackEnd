package apiv1

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"PredictionMarket/internal/oracle"
)

type stubQuoteProvider struct {
	quote *oracle.Quote
	err   error
}

func (s stubQuoteProvider) FetchQuote() (*oracle.Quote, error) {
	return s.quote, s.err
}

func TestGoldQuoteEndpointReturnsPositiveQuote(t *testing.T) {
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, "", 0)
	srv.SetQuoteProvider(stubQuoteProvider{quote: &oracle.Quote{
		PriceUSD:       2412.35,
		Change24h:      1.25,
		QuoteSource:    "新浪财经",
		QuoteUpdatedAt: "2026-07-14 00:10:00",
	}})
	mux := http.NewServeMux()
	srv.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/gold/quote", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, expected := range []string{`"price_usd":2412.35`, `"change_24h":1.25`, `"source":"新浪财经"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response missing %s: %s", expected, body)
		}
	}
}

func TestGoldQuoteEndpointRejectsUnavailableQuote(t *testing.T) {
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, "", 0)
	srv.SetQuoteProvider(stubQuoteProvider{err: fmt.Errorf("all providers unavailable")})
	mux := http.NewServeMux()
	srv.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/gold/quote", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoldQuoteEndpointRejectsZeroPrice(t *testing.T) {
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, "", 0)
	srv.SetQuoteProvider(stubQuoteProvider{quote: &oracle.Quote{PriceUSD: 0}})
	mux := http.NewServeMux()
	srv.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/gold/quote", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
