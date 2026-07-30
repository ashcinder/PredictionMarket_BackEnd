package apiv1

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

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

	contractAddress := r.URL.Query().Get("contract_address")
	if strings.TrimSpace(contractAddress) == "" {
		contractAddress = s.activeContractAddress()
	}
	if contractAddress == "" {
		response := map[string]interface{}{"enabled": s.aiStore.IsEnabled(gameID, userAddress)}
		if strategy := s.aiStore.StrategyForContract(gameID, userAddress, ""); strategy != nil {
			response["strategy"] = strategy
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	if !common.IsHexAddress(contractAddress) || !s.acceptsContractAddress(contractAddress) {
		writeJSONError(w, http.StatusConflict, "contract_address is not the active market contract")
		return
	}
	enabled := s.aiStore.IsEnabled(gameID, userAddress)
	enabled = s.aiStore.IsEnabledForContract(gameID, userAddress, contractAddress)
	response := map[string]interface{}{"enabled": enabled}
	if strategy := s.aiStore.StrategyForContract(gameID, userAddress, contractAddress); strategy != nil {
		response["strategy"] = strategy
	}
	writeJSON(w, http.StatusOK, response)
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

	contractAddress := req.ContractAddress
	if strings.TrimSpace(contractAddress) == "" {
		contractAddress = s.activeContractAddress()
		req.ContractAddress = contractAddress
	}
	if !common.IsHexAddress(contractAddress) || !s.acceptsContractAddress(contractAddress) {
		writeJSONError(w, http.StatusConflict, "contract_address is not the active market contract")
		return
	}

	if req.Enabled {
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
		s.aiStore.DisableForContract(req.GameID, req.UserAddress, req.ContractAddress)
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleStrategiesGet(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "GET,OPTIONS") {
		return
	}
	logRequest(r)
	userAddress := strings.TrimSpace(r.URL.Query().Get("user_address"))
	if !common.IsHexAddress(userAddress) {
		writeJSONError(w, http.StatusBadRequest, "invalid user_address")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"strategies": s.aiStore.StrategiesForUser(userAddress),
	})
}
