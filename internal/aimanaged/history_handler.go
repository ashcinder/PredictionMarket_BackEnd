package aimanaged

import (
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

type HistoryHandler struct {
	histories    HistoryRepository
	defaultLimit int
}

type historyResponsePoint struct {
	Time       int64   `json:"time"`
	YesPercent float64 `json:"yes_percent"`
	NoPercent  float64 `json:"no_percent"`
	ReserveNO  *string `json:"reserve_no"`
	ReserveYES *string `json:"reserve_yes"`
	Source     string  `json:"source"`
}

func NewHistoryHandler(histories HistoryRepository, defaultLimit int) *HistoryHandler {
	return &HistoryHandler{histories: histories, defaultLimit: defaultLimit}
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

	points, err := h.histories.List(r.Context(), MarketIdentity{
		ContractAddress: contract,
		GameID:          gameID,
	}, limit)
	if err != nil {
		slog.Warn("market history query failed", "game_id", gameID, "contract", contract, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "market history unavailable")
		return
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

func decimalStringPointer(value *big.Int) *string {
	if value == nil {
		return nil
	}
	text := value.String()
	return &text
}
