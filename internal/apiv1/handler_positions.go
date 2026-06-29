package apiv1

import (
	"log/slog"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
)

// handleGetPositions handles GET /api/v1/gold/games/{id}/positions?user_address=...
//
// Returns a PositionDetailDTO that combines game metadata, chain state, user
// position (shares), and full trade history for the given user in the
// specified game.
func (s *Server) handleGetPositions(w http.ResponseWriter, r *http.Request) {
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
	if !common.IsHexAddress(userAddress) {
		writeJSONError(w, http.StatusBadRequest, "invalid or missing user_address")
		return
	}

	// 1. Fetch game metadata.
	game, err := s.games.GetGameByID(r.Context(), gameID)
	if err != nil {
		slog.Warn("apiv1: get positions - fetch game failed", "game_id", gameID, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "unable to fetch game metadata")
		return
	}
	if game == nil {
		writeJSONError(w, http.StatusNotFound, "game not found")
		return
	}
	s.hydrateGameFromIPFS(game)

	// 2. Fetch chain state.
	state, err := s.chainStates.GetChainState(r.Context(), gameID)
	if err != nil {
		slog.Warn("apiv1: get positions - fetch chain state failed", "game_id", gameID, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "unable to fetch chain state")
		return
	}
	if state == nil {
		writeJSONError(w, http.StatusNotFound, "chain state not found for game")
		return
	}

	// 3. Fetch user position (non-fatal — defaults to zero shares).
	var sharesYes, sharesNo string
	pos, err := s.positions.GetUserPosition(r.Context(), userAddress, gameID)
	if err != nil {
		slog.Warn("apiv1: get positions - fetch user position failed", "game_id", gameID, "user", userAddress, "error", err)
	}
	if pos != nil {
		sharesYes = bigIntOrZero(pos.MySharesYes)
		sharesNo = bigIntOrZero(pos.MySharesNo)
	} else {
		sharesYes = "0"
		sharesNo = "0"
	}

	// 4. Fetch trade history (non-fatal — defaults to empty array).
	trades, err := s.trades.ListTradesByGameAndUser(r.Context(), gameID, userAddress)
	if err != nil {
		slog.Warn("apiv1: get positions - fetch trades failed", "game_id", gameID, "user", userAddress, "error", err)
		trades = []TradeRecordDTO{}
	}

	// 5. Populate option_name on each trade from game metadata.
	optionNames := []string{game.OptionYes, game.OptionNo}
	for i := range trades {
		if trades[i].OptionID == 0 {
			trades[i].OptionName = game.OptionYes
		} else {
			trades[i].OptionName = game.OptionNo
		}
	}

	// 6. Assemble response.
	dto := PositionDetailDTO{
		GameID:        gameID,
		Desc:          game.Desc,
		Condition:     game.Condition,
		AvatarURL:     game.AvatarURL,
		OptionNames:   optionNames,
		IsResolved:    state.IsResolved,
		IsRefunded:    state.IsRefunded,
		WinningOption: state.WinningOption,
		DeadlineSec:   state.DeadlineSec,
		TotalPool:     bigIntOrZero(state.TotalPool),
		ReserveYes:    bigIntOrZero(state.ReserveYes),
		ReserveNo:     bigIntOrZero(state.ReserveNo),
		MySharesYes:   sharesYes,
		MySharesNo:    sharesNo,
		Trades:        trades,
	}

	slog.Info("apiv1: get positions response", "game_id", gameID, "user", userAddress, "trades", len(trades))
	writeJSON(w, http.StatusOK, dto)
}
