package aimanaged

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"PredictionMarket/internal/chain"

	"github.com/ethereum/go-ethereum/accounts/abi"
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
	NewHistoryHandler(repository, 256, nil).Register(mux)
	target := "/api/gold/market-history?contract_address=" + url.QueryEscape(repositoryTestContract) + "&game_id=1"
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if repository.lastLimit != 256 || repository.lastMarket.GameID != 1 {
		t.Fatalf("unexpected query: market=%+v limit=%d", repository.lastMarket, repository.lastLimit)
	}
	// Response is now a bare JSON array, not {"history": [...]}
	var response []struct {
		ObservedAt int64   `json:"observed_at"`
		ReserveNO  *string `json:"reserve_no"`
		ReserveYES *string `json:"reserve_yes"`
		Source     string  `json:"source"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 2 || response[0].ObservedAt != 100 || response[0].ReserveNO != nil ||
		response[1].ReserveNO == nil || *response[1].ReserveNO != "40" ||
		response[1].ReserveYES == nil || *response[1].ReserveYES != "60" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestMarketHistoryHandlerValidatesParameters(t *testing.T) {
	repository := &handlerHistoryRepository{}
	mux := http.NewServeMux()
	NewHistoryHandler(repository, 256, nil).Register(mux)
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
	NewHistoryHandler(repository, 256, nil).Register(mux)
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
	NewHistoryHandler(repository, 256, nil).Register(mux)

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

// ---------- on-demand sampling tests ----------

const handlerTestABI = `[{"constant":true,"inputs":[{"name":"id","type":"uint256"},{"name":"user","type":"address"}],"name":"getGameExtraData","outputs":[{"name":"virtualReserves","type":"uint256[]"},{"name":"myShares","type":"uint256[]"}],"payable":false,"stateMutability":"view","type":"function"}]`

var handlerParsedABI abi.ABI

func init() {
	var err error
	handlerParsedABI, err = abi.JSON(strings.NewReader(handlerTestABI))
	if err != nil {
		panic("handler test ABI: " + err.Error())
	}
}

func encodeHandlerExtraData(extra *chain.GameExtraData) string {
	method := handlerParsedABI.Methods["getGameExtraData"]
	packed, err := method.Outputs.Pack(extra.VirtualReservesNOYES, extra.MySharesYESNO)
	if err != nil {
		panic("encode handler extra data: " + err.Error())
	}
	return "0x" + hex.EncodeToString(packed)
}

type mockHandlerChain struct {
	wallet    string
	ethCallFn func(ctx context.Context, data string) (string, error)
	mu        sync.Mutex
	calls     int
}

func (m *mockHandlerChain) EthCall(ctx context.Context, data string) (string, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	if m.ethCallFn != nil {
		return m.ethCallFn(ctx, data)
	}
	return "0x", nil
}

func (m *mockHandlerChain) WalletAddress() string { return m.wallet }

func (m *mockHandlerChain) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

type onDemandRepository struct {
	handlerHistoryRepository
	mergeCalls []mergeCallRec
}

type mergeCallRec struct {
	seedLen int
	current HistoryObservation
	limit   int
}

func (r *onDemandRepository) MergeAndList(ctx context.Context, market MarketIdentity, seed []HistoryObservation, current HistoryObservation, limit int) ([]HistoryObservation, error) {
	r.mergeCalls = append(r.mergeCalls, mergeCallRec{seedLen: len(seed), current: current, limit: limit})
	return []HistoryObservation{current}, nil
}

func TestHistoryHandlerOnDemandSampleWhenNoHistory(t *testing.T) {
	chainMock := &mockHandlerChain{
		wallet: "0x1111111111111111111111111111111111111111",
		ethCallFn: func(ctx context.Context, data string) (string, error) {
			extra := &chain.GameExtraData{
				VirtualReservesNOYES: []*big.Int{big.NewInt(300), big.NewInt(700)},
				MySharesYESNO:        []*big.Int{big.NewInt(0), big.NewInt(0)},
			}
			return encodeHandlerExtraData(extra), nil
		},
	}
	repo := &onDemandRepository{}
	repo.points = nil

	mux := http.NewServeMux()
	NewHistoryHandler(repo, 256, chainMock).Register(mux)

	target := "/api/gold/market-history?contract_address=" + url.QueryEscape(repositoryTestContract) + "&game_id=1"
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// Response is a bare JSON array.
	var response []historyResponsePoint
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 1 {
		t.Fatalf("expected 1 on-demand point, got %d: %+v", len(response), response)
	}
	p := response[0]
	if p.YesPercent < 29 || p.YesPercent > 31 {
		t.Fatalf("expected yes~30%%, got %.4f", p.YesPercent)
	}
	if p.Source != historySourceChain {
		t.Fatalf("expected chain source, got %q", p.Source)
	}
	if p.ReserveNO == nil || *p.ReserveNO != "300" {
		t.Fatalf("unexpected reserve_no: %v", p.ReserveNO)
	}
	if p.ReserveYES == nil || *p.ReserveYES != "700" {
		t.Fatalf("unexpected reserve_yes: %v", p.ReserveYES)
	}

	if len(repo.mergeCalls) != 1 {
		t.Fatalf("expected 1 MergeAndList call, got %d", len(repo.mergeCalls))
	}
	if repo.mergeCalls[0].seedLen != 0 {
		t.Fatalf("expected empty seed, got len=%d", repo.mergeCalls[0].seedLen)
	}
	if c := chainMock.callCount(); c != 1 {
		t.Fatalf("expected 1 eth_call, got %d", c)
	}
}

func TestHistoryHandlerNoOnDemandWhenHistoryExists(t *testing.T) {
	chainMock := &mockHandlerChain{
		wallet:    "0x1111111111111111111111111111111111111111",
		ethCallFn: func(ctx context.Context, data string) (string, error) { return "0x", nil },
	}
	repo := &onDemandRepository{}
	repo.points = []HistoryObservation{
		{Time: 100, YesPercent: 50, NoPercent: 50, Source: historySourceIPFS},
	}

	mux := http.NewServeMux()
	NewHistoryHandler(repo, 256, chainMock).Register(mux)

	target := "/api/gold/market-history?contract_address=" + url.QueryEscape(repositoryTestContract) + "&game_id=1"
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	if c := chainMock.callCount(); c != 0 {
		t.Fatalf("chain was called %d times but should not be called when history exists", c)
	}

	var response []historyResponsePoint
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 1 {
		t.Fatalf("expected existing history point, got %d", len(response))
	}
}

func TestHistoryHandlerEmptyWhenNoChainAndNoHistory(t *testing.T) {
	repo := &onDemandRepository{}
	repo.points = nil

	mux := http.NewServeMux()
	NewHistoryHandler(repo, 256, nil).Register(mux)

	target := "/api/gold/market-history?contract_address=" + url.QueryEscape(repositoryTestContract) + "&game_id=1"
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var response []historyResponsePoint
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 0 {
		t.Fatalf("expected empty history when no chain, got %d", len(response))
	}
}

func TestHistoryHandlerOnDemandDegradesGracefully(t *testing.T) {
	chainMock := &mockHandlerChain{
		wallet: "0x1111111111111111111111111111111111111111",
		ethCallFn: func(ctx context.Context, data string) (string, error) {
			return "0x", nil
		},
	}
	repo := &onDemandRepository{}
	repo.points = nil

	mux := http.NewServeMux()
	NewHistoryHandler(repo, 256, chainMock).Register(mux)

	target := "/api/gold/market-history?contract_address=" + url.QueryEscape(repositoryTestContract) + "&game_id=1"
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var response []historyResponsePoint
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 1 {
		t.Fatalf("expected 1 fallback point on chain failure, got %d", len(response))
	}
	p := response[0]
	if p.YesPercent != 50 || p.NoPercent != 50 {
		t.Fatalf("expected neutral 50/50 fallback, got yes=%.4f no=%.4f", p.YesPercent, p.NoPercent)
	}
	if p.Source != historySourceChain {
		t.Fatalf("expected chain source, got %q", p.Source)
	}
}
