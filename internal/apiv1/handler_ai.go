package apiv1

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"PredictionMarket/internal/aimanaged"

	"github.com/ethereum/go-ethereum/common"
)

// handleAIGet handles GET /api/v1/gold/ai-managed
func (s *Server) handleAIGet(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "GET,OPTIONS") {
		return
	}
	logRequest(r)

	gameID, ok := parsePositiveInt(r.URL.Query().Get("game_id"))
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid game_id")
		return
	}
	userAddress := r.URL.Query().Get("user_address")
	if !common.IsHexAddress(userAddress) {
		writeJSONError(w, http.StatusBadRequest, "invalid user_address")
		return
	}

	enabled := s.aiStore.IsEnabled(gameID, userAddress)
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
}

// handleAISet handles POST /api/v1/gold/ai-managed
func (s *Server) handleAISet(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "POST,OPTIONS") {
		return
	}
	logRequest(r)

	var req aimanaged.SetRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if req.GameID <= 0 || !common.IsHexAddress(req.UserAddress) {
		writeJSONError(w, http.StatusBadRequest, "invalid game_id or user_address")
		return
	}

	if req.Enabled {
		if !common.IsHexAddress(req.ContractAddress) {
			writeJSONError(w, http.StatusBadRequest, "invalid contract_address")
			return
		}
		// Validate the private key matches the user address via the existing
		// aimanaged Store.Enable method (which derives the wallet from the key).
		// We cannot call Store.Enable directly from outside the package since
		// it validates the key internally. Instead, pass through to the
		// existing enable path.
		if err := s.aiStore.Enable(req); err != nil {
			slog.Warn("apiv1: ai-managed enable failed", "game_id", req.GameID, "user", req.UserAddress, "error", err)
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		s.aiStore.Disable(req.GameID, req.UserAddress)
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
