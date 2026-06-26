package apiv1

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"PredictionMarket/internal/chain"

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

	// 1. Record the trade.
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
		slog.Warn("apiv1: record trade failed", "game_id", req.GameID, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "failed to record trade")
		return
	}

	// 2. If post-trade state fields are present, update the chain state cache.
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
			// Non-fatal: the trade was recorded successfully.
		}
	}

	// 3. Update the user position cache.
	// When the DApp provides post-trade shares, use them directly.
	// Otherwise auto-fetch from chain so gold_user_positions stays populated.
	pos := &userPositionRow{
		UserAddress: req.UserAddress,
		GameID:      req.GameID,
		MySharesYes:  parseBigIntStr(req.MySharesYesAfter),
		MySharesNo:   parseBigIntStr(req.MySharesNoAfter),
	}
	if pos.MySharesYes == nil && pos.MySharesNo == nil && s.chain != nil {
		// DApp didn't provide post-trade state — fetch from chain.
		encoded, err := chain.EncodeGetGameExtraData(req.GameID, req.UserAddress)
		if err == nil {
			hexResult, ethErr := s.chain.EthCall(r.Context(), encoded)
			if ethErr == nil {
				extra, decErr := chain.DecodeGetGameExtraData(hexResult)
				if decErr == nil && len(extra.MySharesYESNO) >= 2 {
					pos.MySharesYes = extra.MySharesYESNO[0]
					pos.MySharesNo = extra.MySharesYESNO[1]
				}
			}
		}
	}
	if pos.MySharesYes != nil || pos.MySharesNo != nil {
		if err := s.positions.UpsertUserPosition(r.Context(), pos); err != nil {
			slog.Warn("apiv1: cascade update user position failed", "game_id", req.GameID, "user", req.UserAddress, "error", err)
			// Non-fatal.
		}
	}

	slog.Info("apiv1: trade synced", "game_id", req.GameID, "type", tradeType, "tx", req.TxHash)
	writeJSON(w, http.StatusCreated, map[string]bool{"success": true})
}
