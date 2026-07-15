package apiv1

import (
	"log/slog"
	"net/http"
	"strings"
)

type goldQuoteDTO struct {
	PriceUSD        float64 `json:"price_usd"`
	Change24h       float64 `json:"change_24h"`
	ChangeAvailable bool    `json:"change_available"`
	Source          string  `json:"source"`
	UpdatedAt       string  `json:"updated_at"`
}

func (s *Server) handleGetQuote(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "GET,OPTIONS") {
		return
	}
	logRequest(r)
	if s.quote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "gold quote service is unavailable")
		return
	}
	quote, err := s.quote.FetchQuote()
	if err != nil || quote == nil || quote.PriceUSD <= 0 {
		slog.Warn("apiv1: gold quote unavailable", "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "gold quote is temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusOK, goldQuoteDTO{
		PriceUSD:        quote.PriceUSD,
		Change24h:       quote.Change24h,
		ChangeAvailable: quote.ChangeAvailable,
		Source:          strings.TrimSpace(quote.QuoteSource),
		UpdatedAt:       strings.TrimSpace(quote.QuoteUpdatedAt),
	})
}
