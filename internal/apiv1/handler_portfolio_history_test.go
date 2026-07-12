package apiv1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const portfolioTestUser = "0x1111111111111111111111111111111111111111"

type memoryPortfolioHistory struct {
	points []PortfolioHistoryPointDTO
	saved  *portfolioHistoryRow
}

func (m *memoryPortfolioHistory) ListPortfolioHistory(_ context.Context, _ string, _ int) ([]PortfolioHistoryPointDTO, error) {
	return m.points, nil
}

func (m *memoryPortfolioHistory) UpsertPortfolioHistory(_ context.Context, point *portfolioHistoryRow) error {
	m.saved = point
	return nil
}

func TestPortfolioHistoryRoundTripHandlers(t *testing.T) {
	repository := &memoryPortfolioHistory{points: []PortfolioHistoryPointDTO{{
		TimestampSec: 100, TotalValueWei: "2000000000000000000", ActiveMarketCount: 2,
	}}}
	server := &Server{portfolioHistory: repository}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/gold/portfolio-history", server.handleGetPortfolioHistory)
	mux.HandleFunc("POST /api/v1/gold/portfolio-history", server.handleAddPortfolioHistory)

	body := []byte(`{"user_address":"` + portfolioTestUser + `","total_value_wei":"3500000000000000000","active_market_count":3}`)
	post := httptest.NewRecorder()
	mux.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/v1/gold/portfolio-history", bytes.NewReader(body)))
	if post.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body = %s", post.Code, post.Body.String())
	}
	if repository.saved == nil || repository.saved.TotalValueWei.String() != "3500000000000000000" || repository.saved.ActiveMarketCount != 3 {
		t.Fatalf("unexpected saved point: %+v", repository.saved)
	}
	if repository.saved.TimestampSec%portfolioSnapshotBucketSec != 0 {
		t.Fatalf("timestamp %d is not bucketed", repository.saved.TimestampSec)
	}

	get := httptest.NewRecorder()
	target := "/api/v1/gold/portfolio-history?user_address=" + portfolioTestUser
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, target, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", get.Code, get.Body.String())
	}
	var response struct {
		History []PortfolioHistoryPointDTO `json:"history"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.History) != 1 || response.History[0].TotalValueWei != "2000000000000000000" {
		t.Fatalf("unexpected history: %+v", response.History)
	}
}

func TestPortfolioHistoryRejectsInvalidInput(t *testing.T) {
	server := &Server{portfolioHistory: &memoryPortfolioHistory{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/gold/portfolio-history", server.handleGetPortfolioHistory)
	mux.HandleFunc("POST /api/v1/gold/portfolio-history", server.handleAddPortfolioHistory)

	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/gold/portfolio-history?user_address=bad", nil))
	if get.Code != http.StatusBadRequest {
		t.Fatalf("invalid GET status = %d", get.Code)
	}

	post := httptest.NewRecorder()
	body := []byte(`{"user_address":"` + portfolioTestUser + `","total_value_wei":"-1","active_market_count":1}`)
	mux.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/v1/gold/portfolio-history", bytes.NewReader(body)))
	if post.Code != http.StatusBadRequest {
		t.Fatalf("invalid POST status = %d", post.Code)
	}
}
