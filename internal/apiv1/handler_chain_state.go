package apiv1

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// handleGetChainState handles GET /api/v1/gold/games/{id}/chain-state
func (s *Server) handleGetChainState(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "GET,OPTIONS") {
		return
	}
	logRequest(r)

	gameID, ok := parsePositiveIntFromPath(r, "id")
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid game_id")
		return
	}

	userAddress := r.URL.Query().Get("user_address")

	state, err := s.chainStates.GetChainState(r.Context(), gameID)
	if err != nil {
		slog.Warn("apiv1: get chain state failed", "game_id", gameID, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "unable to fetch chain state")
		return
	}
	if state == nil {
		writeJSONError(w, http.StatusNotFound, "chain state not found for game")
		return
	}

	dto := s.buildChainStateDTO(state)

	// Merge user position when user_address is provided.
	if common.IsHexAddress(userAddress) {
		pos, err := s.positions.GetUserPosition(r.Context(), userAddress, gameID)
		if err != nil {
			slog.Warn("apiv1: get user position failed", "game_id", gameID, "user", userAddress, "error", err)
			// Non-fatal: return chain state without position data.
		}
		if pos != nil {
			dto.MySharesYes = bigIntOrZero(pos.MySharesYes)
			dto.MySharesNo = bigIntOrZero(pos.MySharesNo)
		}
	}

	writeJSON(w, http.StatusOK, dto)
}

// handleListChainStates handles GET /api/v1/gold/games/chain-states
func (s *Server) handleListChainStates(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "GET,OPTIONS") {
		return
	}
	logRequest(r)

	userAddress := r.URL.Query().Get("user_address")

	states, err := s.chainStates.ListAllChainStates(r.Context())
	if err != nil {
		slog.Warn("apiv1: list chain states failed", "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "unable to list chain states")
		return
	}

	// Build a position lookup map when user_address is provided.
	var posMap map[int]*userPositionRow
	if common.IsHexAddress(userAddress) {
		positions, err := s.positions.ListUserPositions(r.Context(), userAddress)
		if err != nil {
			slog.Warn("apiv1: list user positions failed", "user", userAddress, "error", err)
		}
		posMap = make(map[int]*userPositionRow, len(positions))
		for i := range positions {
			posMap[positions[i].GameID] = &positions[i]
		}
	}

	dtos := make([]ChainStateDTO, 0, len(states))
	for i := range states {
		dto := s.buildChainStateDTO(&states[i])
		if posMap != nil {
			if pos, ok := posMap[states[i].GameID]; ok {
				dto.MySharesYes = bigIntOrZero(pos.MySharesYes)
				dto.MySharesNo = bigIntOrZero(pos.MySharesNo)
			}
		}
		dtos = append(dtos, dto)
	}

	slog.Info("apiv1: list chain states response", "count", len(dtos))
	writeJSON(w, http.StatusOK, map[string]interface{}{"states": dtos})
}

// handleSyncChainState handles POST /api/v1/gold/games/{id}/chain-state/sync
func (s *Server) handleSyncChainState(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "POST,OPTIONS") {
		return
	}
	logRequest(r)

	gameID, ok := parsePositiveIntFromPath(r, "id")
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid game_id")
		return
	}

	var req SyncChainStateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	row := &chainStateRow{
		GameID:        gameID,
		TotalPool:     parseBigIntStr(req.TotalPool),
		IsResolved:    req.IsResolved,
		IsRefunded:    req.IsRefunded,
		WinningOption: req.WinningOption,
		ReserveYes:    parseBigIntStr(req.ReserveYes),
		ReserveNo:     parseBigIntStr(req.ReserveNo),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.chainStates.UpsertChainState(r.Context(), row); err != nil {
		slog.Warn("apiv1: sync chain state failed", "game_id", gameID, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "failed to sync chain state")
		return
	}

	// Also update user position if shares are provided.
	if req.MySharesYes != "" || req.MySharesNo != "" {
		// NOTE: the sync-chain-state endpoint does not include user_address.
		// This is by design — user positions are primarily synced via
		// /trades/sync which includes the user_address field.
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// buildChainStateDTO converts a chainStateRow into the response DTO.
func (s *Server) buildChainStateDTO(state *chainStateRow) ChainStateDTO {
	return ChainStateDTO{
		GameID:        state.GameID,
		TotalPool:     bigIntOrZero(state.TotalPool),
		IsResolved:    state.IsResolved,
		IsRefunded:    state.IsRefunded,
		WinningOption: state.WinningOption,
		DeadlineSec:   state.DeadlineSec,
		ReserveYes:    bigIntOrZero(state.ReserveYes),
		ReserveNo:     bigIntOrZero(state.ReserveNo),
		MySharesYes:   "0",
		MySharesNo:    "0",
		UpdatedAt:     state.UpdatedAt,
	}
}

