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
	Time       int64   `json:"time"`
	YesPercent float64 `json:"yes_percent"`
	NoPercent  float64 `json:"no_percent"`
	ReserveNO  *string `json:"reserve_no"`
	ReserveYES *string `json:"reserve_yes"`
	Source     string  `json:"source"`
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

	response := make([]historyResponsePoint, len(points))
	for i, point := range points {
		response[i] = historyResponsePoint{
			Time: point.Time, YesPercent: point.YesPercent, NoPercent: point.NoPercent,
			ReserveNO:  decimalStringPointer(point.ReserveNO),
			ReserveYES: decimalStringPointer(point.ReserveYES),
			Source:     point.Source,
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"history": response})
}

// sampleOnDemand fetches the current reserves for a game from chain, persists
// a snapshot to market_history, and returns the observation. It returns nil on
// any error so the API degrades gracefully to an empty response.
func (h *HistoryHandler) sampleOnDemand(ctx context.Context, market MarketIdentity) *HistoryObservation {
	encoded, err := chain.EncodeGetGameExtraData(market.GameID, h.chain.WalletAddress())
	if err != nil {
		slog.Warn("history handler: on-demand encode failed", "game_id", market.GameID, "error", err)
		return nil
	}
	hexResult, err := h.chain.EthCall(ctx, encoded)
	if err != nil {
		slog.Warn("history handler: on-demand eth_call failed", "game_id", market.GameID, "error", err)
		return nil
	}
	extra, err := chain.DecodeGetGameExtraData(hexResult)
	if err != nil {
		slog.Warn("history handler: on-demand decode failed", "game_id", market.GameID, "error", err)
		return nil
	}
	obs, err := observationFromReserves(extra, time.Now())
	if err != nil {
		slog.Warn("history handler: on-demand observation failed", "game_id", market.GameID, "error", err)
		return nil
	}

	saved, err := h.histories.MergeAndList(ctx, market, nil, obs, h.defaultLimit)
	if err != nil {
		slog.Warn("history handler: on-demand persist failed", "game_id", market.GameID, "error", err)
		return nil
	}
	if len(saved) == 0 {
		return nil
	}
	// Return the most recent point (last in the returned slice, which is
	// ordered ascending in time).
	last := saved[len(saved)-1]
	return &last
}

func decimalStringPointer(value *big.Int) *string {
	if value == nil {
		return nil
	}
	text := value.String()
	return &text
}
