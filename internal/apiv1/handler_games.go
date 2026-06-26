package apiv1

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"PredictionMarket/internal/chain"

	"github.com/ethereum/go-ethereum/common"
)

// handleListGames handles GET /api/v1/gold/games
func (s *Server) handleListGames(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "GET,OPTIONS") {
		return
	}
	logRequest(r)

	games, err := s.games.ListAllGames(r.Context())
	if err != nil {
		slog.Warn("apiv1: list games failed", "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "unable to list games: "+err.Error())
		return
	}
	if games == nil {
		games = []GameMetaDTO{}
	}
	s.hydrateGamesFromIPFS(games)
	slog.Info("apiv1: list games response", "count", len(games))
	writeJSON(w, http.StatusOK, map[string]interface{}{"games": games})
}

// handleGetGame handles GET /api/v1/gold/games/{id}
func (s *Server) handleGetGame(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "GET,OPTIONS") {
		return
	}
	logRequest(r)

	gameID, ok := parsePositiveIntFromPath(r, "id")
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid game_id")
		return
	}

	game, err := s.games.GetGameByID(r.Context(), gameID)
	if err != nil {
		slog.Warn("apiv1: get game failed", "game_id", gameID, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "unable to fetch game: "+err.Error())
		return
	}
	if game == nil {
		writeJSONError(w, http.StatusNotFound, "game not found")
		return
	}
	s.hydrateGameFromIPFS(game)
	writeJSON(w, http.StatusOK, game)
}

// handleSyncGame handles POST /api/v1/gold/games/sync
func (s *Server) handleSyncGame(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "POST,OPTIONS") {
		return
	}
	logRequest(r)

	var req SyncGameRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if req.IPFSCID == "" {
		writeJSONError(w, http.StatusBadRequest, "ipfs_cid is required")
		return
	}
	if !common.IsHexAddress(req.ContractAddress) {
		writeJSONError(w, http.StatusBadRequest, "invalid contract_address")
		return
	}

	gameID := req.GameID

	// When the DApp creates a new game it does not know the contract-assigned
	// game ID yet — it sends game_id=0. We resolve the real ID by scanning
	// the chain for a game whose IPFS CID matches.
	// Uses a detached context so a client timeout doesn't kill the chain call.
	if gameID == 0 {
		if s.chain == nil {
			writeJSONError(w, http.StatusBadRequest, "game_id is 0 but chain client is not available for lookup")
			return
		}
		bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		resolved, err := s.resolveGameIDByCID(bgCtx, req.IPFSCID)
		if err != nil {
			slog.Warn("apiv1: resolve game_id by CID failed", "cid", req.IPFSCID, "error", err)
			writeJSONError(w, http.StatusServiceUnavailable, "failed to resolve game_id from chain")
			return
		}
		if resolved == 0 {
			writeJSONError(w, http.StatusBadRequest, "game not found on chain for the given IPFS CID")
			return
		}
		gameID = resolved
	}

	row := &gameRow{
		GameID:          gameID,
		ContractAddress: req.ContractAddress,
		IPFSCID:         req.IPFSCID,
		Desc:            req.Desc,
		Condition:       req.Condition,
		AvatarURL:       req.AvatarURL,
		DetailedInfo:    req.DetailedInfo,
		OptionYes:       req.OptionYes,
		OptionNo:        req.OptionNo,
		CreatorAddress:  req.CreatorAddress,
	}
	id, err := s.games.UpsertGame(r.Context(), row)
	if err != nil {
		slog.Warn("apiv1: sync game failed", "game_id", gameID, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "failed to sync game metadata")
		return
	}

	slog.Info("apiv1: game synced", "game_id", id, "cid", req.IPFSCID)
	writeJSON(w, http.StatusOK, SyncGameResponse{Success: true, GameID: id})
}

// resolveGameIDByCID calls getAllGames on chain and returns the ID of the
// game whose IPFS CID matches the given cid. Returns 0 when no match is found.
func (s *Server) resolveGameIDByCID(ctx context.Context, cid string) (int, error) {
	encoded := chain.EncodeGetAllGames()
	hexResult, err := s.chain.EthCall(ctx, encoded)
	if err != nil {
		return 0, err
	}
	games, err := chain.DecodeGetAllGames(hexResult)
	if err != nil {
		return 0, err
	}
	for _, g := range games {
		if g.IPFSCID == cid {
			return g.ID, nil
		}
	}
	return 0, nil
}

func (s *Server) hydrateGamesFromIPFS(games []GameMetaDTO) {
	if s.metadata == nil {
		return
	}
	for i := range games {
		s.hydrateGameFromIPFS(&games[i])
	}
}

func (s *Server) hydrateGameFromIPFS(game *GameMetaDTO) {
	if s.metadata == nil || game == nil || strings.TrimSpace(game.IPFSCID) == "" || !needsMetadataHydration(game) {
		return
	}

	meta, err := s.metadata.DownloadMetadata(game.IPFSCID)
	if err != nil {
		slog.Warn("apiv1: ipfs metadata hydration failed", "game_id", game.GameID, "cid", game.IPFSCID, "error", err)
		return
	}
	if meta == nil {
		return
	}

	if strings.TrimSpace(game.Desc) == "" && strings.TrimSpace(meta.Desc) != "" {
		game.Desc = meta.Desc
	}
	if strings.TrimSpace(game.Condition) == "" && strings.TrimSpace(meta.Condition) != "" {
		game.Condition = meta.Condition
	}
	if strings.TrimSpace(game.AvatarURL) == "" && strings.TrimSpace(meta.AvatarURL) != "" {
		game.AvatarURL = meta.AvatarURL
	}
	if strings.TrimSpace(game.DetailedInfo) == "" && strings.TrimSpace(meta.DetailedInfo) != "" {
		game.DetailedInfo = meta.DetailedInfo
	}
	if isDefaultOption(game.OptionYes, "YES") && strings.TrimSpace(meta.OptionYES) != "" {
		game.OptionYes = meta.OptionYES
	}
	if isDefaultOption(game.OptionNo, "NO") && strings.TrimSpace(meta.OptionNO) != "" {
		game.OptionNo = meta.OptionNO
	}
}

func needsMetadataHydration(game *GameMetaDTO) bool {
	return strings.TrimSpace(game.Desc) == "" ||
		strings.TrimSpace(game.Condition) == "" ||
		strings.TrimSpace(game.DetailedInfo) == "" ||
		strings.TrimSpace(game.AvatarURL) == "" ||
		isDefaultOption(game.OptionYes, "YES") ||
		isDefaultOption(game.OptionNo, "NO")
}

func isDefaultOption(value, fallback string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, fallback)
}
