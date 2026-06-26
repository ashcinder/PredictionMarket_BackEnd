package apiv1

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// validTradeTypes contains the allowed trade_type values.
var validTradeTypes = map[string]bool{
	"BUY":     true,
	"SELL":    true,
	"CLAIM":   true,
	"RESOLVE": true,
}

// handleSyncTrade handles POST /api/v1/gold/trades/sync
func (s *Server) handleSyncTrade(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "POST,OPTIONS") {
		return
	}
	logRequest(r)

	var req SyncTradeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	// Validate required fields.
	if req.GameID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid game_id")
		return
	}
	if !common.IsHexAddress(req.ContractAddr) {
		writeJSONError(w, http.StatusBadRequest, "invalid contract_address")
		return
	}
	if !common.IsHexAddress(req.UserAddress) {
		writeJSONError(w, http.StatusBadRequest, "invalid user_address")
		return
	}
	tradeType := strings.ToUpper(strings.TrimSpace(req.TradeType))
	if !validTradeTypes[tradeType] {
		writeJSONError(w, http.StatusBadRequest, "trade_type must be BUY, SELL, CLAIM, or RESOLVE")
		return
	}

	// 1. Record the trade (best-effort, non-fatal).
	// Chain state and user position updates below are more important for
	// the read path — a missing trade record is a minor audit gap, but
	// missing chain state breaks the entire cache layer.
	trade := &tradeRow{
		GameID:          req.GameID,
		ContractAddress: req.ContractAddr,
		UserAddress:     req.UserAddress,
		TradeType:       tradeType,
		OptionID:        req.OptionID,
		AmountWei:       parseBigIntStr(req.AmountWei),
		TxHash:          req.TxHash,
		IsSuccess:       req.IsSuccess,
	}
	if err := s.trades.RecordTrade(r.Context(), trade); err != nil {
		slog.Warn("apiv1: record trade failed (non-fatal, continuing with state updates)", "game_id", req.GameID, "error", err)
		// Non-fatal — don't return, still update chain state and positions below.
	}

	// 2. Update chain state cache from post-trade fields.
	// The DApp sends these after every buy/sell/claim/resolve.
	if req.TotalPoolAfter != "" || req.ReserveYesAfter != "" || req.ReserveNoAfter != "" {
		state := &chainStateRow{
			GameID:     req.GameID,
			TotalPool:  parseBigIntStr(req.TotalPoolAfter),
			ReserveYes: parseBigIntStr(req.ReserveYesAfter),
			ReserveNo:  parseBigIntStr(req.ReserveNoAfter),
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		}
		if err := s.chainStates.UpsertChainState(r.Context(), state); err != nil {
			slog.Warn("apiv1: cascade update chain state failed", "game_id", req.GameID, "error", err)
		}
	}

	// 3. Update user position cache from post-trade shares.
	if req.MySharesYesAfter != "" || req.MySharesNoAfter != "" {
		pos := &userPositionRow{
			UserAddress: req.UserAddress,
			GameID:      req.GameID,
			MySharesYes:  parseBigIntStr(req.MySharesYesAfter),
			MySharesNo:   parseBigIntStr(req.MySharesNoAfter),
		}
		if err := s.positions.UpsertUserPosition(r.Context(), pos); err != nil {
			slog.Warn("apiv1: cascade update user position failed", "game_id", req.GameID, "user", req.UserAddress, "error", err)
		}
	}

	slog.Info("apiv1: trade synced", "game_id", req.GameID, "type", tradeType)
	writeJSON(w, http.StatusCreated, map[string]bool{"success": true})
}
