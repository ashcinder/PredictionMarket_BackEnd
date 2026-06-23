package aimanaged

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"PredictionMarket/internal/chain"

	"github.com/ethereum/go-ethereum/common"
)

// historyFetcher is the subset of chain.Client used by HistoryHandler for
// on-demand sampling when no market history exists yet.
type historyFetcher interface {
	EthCall(ctx context.Context, data string) (string, error)
	WalletAddress() string
}

type HistoryHandler struct {
	histories    HistoryRepository
	defaultLimit int
	chain        historyFetcher
}

type historyResponsePoint struct {
	ObservedAt int64   `json:"observed_at"`
	YesPercent float64 `json:"yes_percent"`
	NoPercent  float64 `json:"no_percent"`
	ReserveNO  *string `json:"reserve_no,omitempty"`
	ReserveYES *string `json:"reserve_yes,omitempty"`
	Source     string  `json:"source,omitempty"`
}

// NewHistoryHandler creates a handler that serves market history. If chain is
// non-nil the handler will sample a fresh data point on-the-fly when the
// database has no history for a game yet, so the frontend always gets at least
// one point even for brand-new pools.
func NewHistoryHandler(histories HistoryRepository, defaultLimit int, chain historyFetcher) *HistoryHandler {
	return &HistoryHandler{histories: histories, defaultLimit: defaultLimit, chain: chain}
}

func (h *HistoryHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/gold/market-history", h.handle)
}

func (h *HistoryHandler) handle(w http.ResponseWriter, r *http.Request) {
	slog.Info("api request", "method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery, "remote", r.RemoteAddr)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET,OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	contract := strings.TrimSpace(r.URL.Query().Get("contract_address"))
	if !common.IsHexAddress(contract) {
		writeJSONError(w, http.StatusBadRequest, "contract_address is invalid")
		return
	}
	gameID, ok := parsePositiveInt(r.URL.Query().Get("game_id"))
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "game_id must be positive")
		return
	}
	limit := h.defaultLimit
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = parsed
	}
	if limit < 1 || limit > 1000 {
		writeJSONError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
		return
	}

	market := MarketIdentity{ContractAddress: contract, GameID: gameID}
	points, err := h.histories.List(r.Context(), market, limit)
	if err != nil {
		slog.Warn("market history query failed", "game_id", gameID, "contract", contract, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "market history unavailable")
		return
	}

	// On-demand sampling: when the database has no history yet (e.g. brand-new
	// pool), immediately fetch the current reserves from chain, persist them,
	// and return that first data point to the frontend. This bridges the gap
	// between pool creation and the next sampler tick.
	if len(points) == 0 && h.chain != nil {
		if sampled := h.sampleOnDemand(r.Context(), market); sampled != nil {
			points = []HistoryObservation{*sampled}
		}
	}

	// Build response as a bare JSON array (frontend Java code expects [...]
	// at the top level, not {"history": [...]}).
	response := make([]historyResponsePoint, len(points))
	for i, point := range points {
		response[i] = historyResponsePoint{
			ObservedAt: point.Time, YesPercent: point.YesPercent, NoPercent: point.NoPercent,
			ReserveNO:  decimalStringPointer(point.ReserveNO),
			ReserveYES: decimalStringPointer(point.ReserveYES),
			Source:     point.Source,
		}
	}
	slog.Info("api response", "path", r.URL.Path, "game_id", gameID, "points", len(response))
	_ = json.NewEncoder(w).Encode(response)
}

// sampleOnDemand fetches the current reserves for a game from chain, persists
// a snapshot to market_history, and returns the observation. On any error it
// falls back to a neutral 50/50 placeholder so the frontend always has at
// least one data point to display; real percentages arrive on the next sampler
// tick. The fallback is deliberately NOT persisted — only real chain data is
// written to market_history.
func (h *HistoryHandler) sampleOnDemand(ctx context.Context, market MarketIdentity) *HistoryObservation {
	encoded, err := chain.EncodeGetGameExtraData(market.GameID, h.chain.WalletAddress())
	if err != nil {
		slog.Warn("history handler: on-demand encode failed", "game_id", market.GameID, "error", err)
		return h.fallbackObservation()
	}
	hexResult, err := h.chain.EthCall(ctx, encoded)
	if err != nil {
		slog.Warn("history handler: on-demand eth_call failed", "game_id", market.GameID, "error", err)
		return h.fallbackObservation()
	}
	extra, err := chain.DecodeGetGameExtraData(hexResult)
	if err != nil {
		slog.Warn("history handler: on-demand decode failed", "game_id", market.GameID, "error", err)
		return h.fallbackObservation()
	}
	obs, err := observationFromReserves(extra, time.Now())
	if err != nil {
		slog.Warn("history handler: on-demand observation failed", "game_id", market.GameID, "error", err)
		return h.fallbackObservation()
	}

	saved, err := h.histories.MergeAndList(ctx, market, nil, obs, h.defaultLimit)
	if err != nil {
		slog.Warn("history handler: on-demand persist failed", "game_id", market.GameID, "error", err)
		return h.fallbackObservation()
	}
	if len(saved) == 0 {
		return h.fallbackObservation()
	}
	// Return the most recent point (last in the returned slice, which is
	// ordered ascending in time).
	last := saved[len(saved)-1]
	return &last
}

// fallbackObservation returns a neutral 50/50 data point.
func (h *HistoryHandler) fallbackObservation() *HistoryObservation {
	return &HistoryObservation{
		Time:       time.Now().Unix(),
		YesPercent: 50,
		NoPercent:  50,
		Source:     historySourceChain,
	}
}

func decimalStringPointer(value *big.Int) *string {
	if value == nil {
		return nil
	}
	text := value.String()
	return &text
}
