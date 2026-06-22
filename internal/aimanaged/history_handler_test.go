package aimanaged

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type handlerHistoryRepository struct {
	points     []HistoryObservation
	err        error
	lastMarket MarketIdentity
	lastLimit  int
}

func (r *handlerHistoryRepository) MergeAndList(context.Context, MarketIdentity, []HistoryObservation, HistoryObservation, int) ([]HistoryObservation, error) {
	return nil, errors.New("not used")
}

func (r *handlerHistoryRepository) List(_ context.Context, market MarketIdentity, limit int) ([]HistoryObservation, error) {
	r.lastMarket = market
	r.lastLimit = limit
	return cloneObservations(r.points), r.err
}

func TestMarketHistoryHandlerReturnsAscendingHistoryWithStringReserves(t *testing.T) {
	repository := &handlerHistoryRepository{points: []HistoryObservation{
		{Time: 100, YesPercent: 51, NoPercent: 49, Source: historySourceIPFS},
		{Time: 200, YesPercent: 60, NoPercent: 40, ReserveNO: big.NewInt(40), ReserveYES: big.NewInt(60), Source: historySourceChain},
	}}
	mux := http.NewServeMux()
	NewHistoryHandler(repository, 256).Register(mux)
	target := "/api/gold/market-history?contract_address=" + url.QueryEscape(repositoryTestContract) + "&game_id=1"
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if repository.lastLimit != 256 || repository.lastMarket.GameID != 1 {
		t.Fatalf("unexpected query: market=%+v limit=%d", repository.lastMarket, repository.lastLimit)
	}
	var response struct {
		History []struct {
			Time       int64   `json:"time"`
			ReserveNO  *string `json:"reserve_no"`
			ReserveYES *string `json:"reserve_yes"`
			Source     string  `json:"source"`
		} `json:"history"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.History) != 2 || response.History[0].Time != 100 || response.History[0].ReserveNO != nil ||
		response.History[1].ReserveNO == nil || *response.History[1].ReserveNO != "40" ||
		response.History[1].ReserveYES == nil || *response.History[1].ReserveYES != "60" {
		t.Fatalf("unexpected response: %+v", response.History)
	}
}

func TestMarketHistoryHandlerValidatesParameters(t *testing.T) {
	repository := &handlerHistoryRepository{}
	mux := http.NewServeMux()
	NewHistoryHandler(repository, 256).Register(mux)
	tests := []string{
		"/api/gold/market-history",
		"/api/gold/market-history?contract_address=bad&game_id=1",
		"/api/gold/market-history?contract_address=" + repositoryTestContract + "&game_id=0",
		"/api/gold/market-history?contract_address=" + repositoryTestContract + "&game_id=1&limit=0",
		"/api/gold/market-history?contract_address=" + repositoryTestContract + "&game_id=1&limit=1001",
	}
	for _, target := range tests {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("target=%s status=%d body=%s", target, recorder.Code, recorder.Body.String())
		}
	}
}

func TestMarketHistoryHandlerReturnsServiceUnavailableWithoutDetails(t *testing.T) {
	repository := &handlerHistoryRepository{err: errors.New("mysql password secret")}
	mux := http.NewServeMux()
	NewHistoryHandler(repository, 256).Register(mux)
	target := "/api/gold/market-history?contract_address=" + repositoryTestContract + "&game_id=1&limit=1"
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusServiceUnavailable || recorder.Body.String() == "" {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if contains := recorder.Body.String(); contains != "{\"error\":\"market history unavailable\"}\n" {
		t.Fatalf("response leaked internal details: %s", contains)
	}
}

func TestMarketHistoryHandlerSupportsCORSAndMethods(t *testing.T) {
	repository := &handlerHistoryRepository{}
	mux := http.NewServeMux()
	NewHistoryHandler(repository, 256).Register(mux)

	options := httptest.NewRecorder()
	mux.ServeHTTP(options, httptest.NewRequest(http.MethodOptions, "/api/gold/market-history", nil))
	if options.Code != http.StatusNoContent || options.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("unexpected OPTIONS response: status=%d headers=%v", options.Code, options.Header())
	}

	post := httptest.NewRecorder()
	mux.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/gold/market-history", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", post.Code)
	}
}
