package gameapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"PredictionMarket/internal/chain"

	"github.com/ethereum/go-ethereum/common"
)

// chainFetcher is the subset of chain.Client needed by the handler.
type chainFetcher interface {
	EthCall(ctx context.Context, data string) (string, error)
	RetryableEthCall(ctx context.Context, data string) (string, error)
	WalletAddress() string
}

// HistoryProvider is the subset of aimanaged needed for market history.
type HistoryProvider interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// Handler serves the Gold Game API endpoints defined in backend-api-spec.md.
type Handler struct {
	repo           *GameRepository
	chain          chainFetcher
	contractAddr   string
	historyHandler HistoryProvider
	syncMu         sync.Mutex
}

func NewHandler(repo *GameRepository, ch chainFetcher, contractAddr string, history HistoryProvider) *Handler {
	return &Handler{
		repo:           repo,
		chain:          ch,
		contractAddr:   contractAddr,
		historyHandler: history,
	}
}

// Register mounts all game-api routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/gold/health", h.handleHealth)
	mux.HandleFunc("/api/gold/games", h.handleGamesList)
	mux.HandleFunc("/api/gold/games/", h.handleGamesByPath)
	mux.HandleFunc("/api/gold/users/", h.handleUsers)
}

// ---------- health ----------

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	slog.Info("api request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
	})
}

// ---------- game list / detail / sync ----------

func (h *Handler) handleGamesList(w http.ResponseWriter, r *http.Request) {
	slog.Info("api request", "method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery, "remote", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost {
		// POST /api/gold/games/sync or /api/gold/games/batch-sync
		if strings.HasSuffix(r.URL.Path, "/sync") {
			h.handleSync(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/batch-sync") {
			h.handleBatchSync(w, r)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user := strings.TrimSpace(r.URL.Query().Get("user_address"))
	if !common.IsHexAddress(user) {
		writeError(w, http.StatusBadRequest, "user_address is invalid")
		return
	}

	games, err := h.repo.ListAll(r.Context(), user)
	if err != nil {
		slog.Warn("game list query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	resp := make([]gameResponse, len(games))
	for i, g := range games {
		resp[i] = gameRowToResponse(g)
	}
	slog.Info("api response", "path", r.URL.Path, "games", len(resp))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"games": resp})
}

// handleGamesByPath dispatches /api/gold/games/{id} and
// /api/gold/games/{id}/history.
func (h *Handler) handleGamesByPath(w http.ResponseWriter, r *http.Request) {
	slog.Info("api request", "method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery, "remote", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")

	// Strip prefix: /api/gold/games/
	rest := strings.TrimPrefix(r.URL.Path, "/api/gold/games/")
	parts := strings.SplitN(rest, "/", 2)

	gameID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || gameID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid game_id")
		return
	}

	// /api/gold/games/{id}/history — history handler expects game_id and
	// contract_address in query params, but the new URL puts game_id in
	// the path. Inject it into the query string before forwarding.
	if len(parts) == 2 && parts[1] == "history" {
		q := r.URL.Query()
		if q.Get("game_id") == "" {
			q.Set("game_id", parts[0])
			r.URL.RawQuery = q.Encode()
		}
		h.historyHandler.ServeHTTP(w, r)
		return
	}

	// /api/gold/games/{id}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := strings.TrimSpace(r.URL.Query().Get("user_address"))
	if !common.IsHexAddress(user) {
		writeError(w, http.StatusBadRequest, "user_address is invalid")
		return
	}

	g, err := h.repo.GetByID(r.Context(), gameID, user)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "game not found")
		return
	}
	if err != nil {
		slog.Warn("game detail query failed", "game_id", gameID, "error", err)
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	_ = json.NewEncoder(w).Encode(gameRowToResponse(*g))
}

// ---------- user positions ----------

func (h *Handler) handleUsers(w http.ResponseWriter, r *http.Request) {
	slog.Info("api request", "method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery, "remote", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// /api/gold/users/{address}/positions
	rest := strings.TrimPrefix(r.URL.Path, "/api/gold/users/")
	address, _, found := strings.Cut(rest, "/positions")
	address = strings.TrimSpace(address)
	if !found || !common.IsHexAddress(address) {
		writeError(w, http.StatusBadRequest, "invalid path or address")
		return
	}

	games, err := h.repo.GetPositions(r.Context(), address)
	if err != nil {
		slog.Warn("positions query failed", "user", address, "error", err)
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	resp := make([]gameResponse, len(games))
	for i, g := range games {
		resp[i] = gameRowToResponse(g)
	}
	slog.Info("api response", "path", r.URL.Path, "positions", len(resp))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"positions": resp})
}

// ---------- sync ----------

func (h *Handler) handleSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GameID          int64  `json:"game_id"`
		ContractAddress string `json:"contract_address"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.GameID <= 0 || !common.IsHexAddress(req.ContractAddress) {
		writeError(w, http.StatusBadRequest, "invalid game_id or contract_address")
		return
	}

	// Run sync in background; DApp doesn't wait.
	go func() {
		syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := h.syncOneGame(syncCtx, req.GameID, req.ContractAddress); err != nil {
			slog.Warn("sync game failed", "game_id", req.GameID, "error", err)
		}
	}()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "syncing",
		"game_id": req.GameID,
	})
}

func (h *Handler) handleBatchSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ContractAddress string `json:"contract_address"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !common.IsHexAddress(req.ContractAddress) {
		writeError(w, http.StatusBadRequest, "invalid contract_address")
		return
	}

	go func() {
		syncCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if err := h.syncAllGames(syncCtx); err != nil {
			slog.Warn("batch sync failed", "error", err)
		}
	}()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           "syncing",
		"contract_address": req.ContractAddress,
	})
}

// SyncAllGames pulls all games from chain and upserts them into gold_games.
// Exported so main.go can call it on a timer.
func (h *Handler) SyncAllGames(ctx context.Context) {
	if err := h.syncAllGames(ctx); err != nil {
		slog.Warn("periodic game cache sync failed", "error", err)
	}
}

// ---------- sync helpers ----------

func (h *Handler) syncOneGame(ctx context.Context, gameID int64, contractAddr string) error {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()

	encoded, err := chain.EncodeGetGameInfo(int(gameID))
	if err != nil {
		return err
	}
	hexRes, err := h.chain.RetryableEthCall(ctx, encoded)
	if err != nil {
		return err
	}
	info, err := chain.DecodeGetGameInfo(int(gameID), hexRes)
	if err != nil {
		return err
	}

	extraEnc, err := chain.EncodeGetGameExtraData(int(gameID), h.chain.WalletAddress())
	if err != nil {
		return err
	}
	extraHex, err := h.chain.RetryableEthCall(ctx, extraEnc)
	if err != nil {
		return err
	}
	extra, err := chain.DecodeGetGameExtraData(extraHex)
	if err != nil {
		return err
	}

	// Upsert into gold_games.
	ipfsCID := info.IPFSCID
	if ipfsCID == "" {
		ipfsCID = "0x0"
	}
	totalPool := "0"
	if info.TotalPool != nil {
		totalPool = info.TotalPool.String()
	}
	yesReserve := "0"
	noReserve := "0"
	if len(extra.VirtualReservesNOYES) >= 2 {
		if extra.VirtualReservesNOYES[1] != nil {
			yesReserve = extra.VirtualReservesNOYES[1].String()
		}
		if extra.VirtualReservesNOYES[0] != nil {
			noReserve = extra.VirtualReservesNOYES[0].String()
		}
	}

	return h.repo.UpsertGame(ctx, GameRow{
		ID:              int64(info.ID),
		ContractAddress: contractAddr,
		IPFSCID:         ipfsCID,
		TotalPool:       totalPool,
		IsResolved:      info.IsResolved,
		IsRefunded:      info.IsRefunded,
		WinningOption:   info.WinningOption,
		DeadlineSec:     info.DeadlineRaw,
		ReserveYes:      yesReserve,
		ReserveNo:       noReserve,
	})
}

func (h *Handler) syncAllGames(ctx context.Context) error {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()

	data := chain.EncodeGetAllGames()
	hexResult, err := h.chain.RetryableEthCall(ctx, data)
	if err != nil {
		return err
	}
	games, err := chain.DecodeGetAllGames(hexResult)
	if err != nil {
		return err
	}

	for _, game := range games {
		ipfsCID := game.IPFSCID
		if ipfsCID == "" {
			ipfsCID = "0x0"
		}
		totalPool := "0"
		if game.TotalPool != nil {
			totalPool = game.TotalPool.String()
		}

		// Get extra data for reserves.
		extraEnc, extraErr := chain.EncodeGetGameExtraData(game.ID, h.chain.WalletAddress())
		yesReserve := "0"
		noReserve := "0"
		if extraErr == nil {
			extraHex, extraErr2 := h.chain.RetryableEthCall(ctx, extraEnc)
			if extraErr2 == nil {
				extra, extraErr3 := chain.DecodeGetGameExtraData(extraHex)
				if extraErr3 == nil && len(extra.VirtualReservesNOYES) >= 2 {
					if extra.VirtualReservesNOYES[1] != nil {
						yesReserve = extra.VirtualReservesNOYES[1].String()
					}
					if extra.VirtualReservesNOYES[0] != nil {
						noReserve = extra.VirtualReservesNOYES[0].String()
					}
				}
			}
		}

		if err := h.repo.UpsertGame(ctx, GameRow{
			ID:              int64(game.ID),
			ContractAddress: h.contractAddr,
			IPFSCID:         ipfsCID,
			TotalPool:       totalPool,
			IsResolved:      game.IsResolved,
			IsRefunded:      game.IsRefunded,
			WinningOption:   game.WinningOption,
			DeadlineSec:     game.DeadlineRaw,
			ReserveYes:      yesReserve,
			ReserveNo:       noReserve,
		}); err != nil {
			slog.Warn("batch sync: upsert game failed", "game_id", game.ID, "error", err)
		}
	}
	return nil
}

// ---------- helpers ----------

type gameResponse map[string]interface{}

func gameRowToResponse(g GameRow) gameResponse {
	return gameResponse{
		"id":               g.ID,
		"contract_address": g.ContractAddress,
		"ipfs_cid":         g.IPFSCID,
		"total_pool":       g.TotalPool,
		"is_resolved":      g.IsResolved,
		"is_refunded":      g.IsRefunded,
		"winning_option":   g.WinningOption,
		"deadline_sec":     g.DeadlineSec,
		"reserve_yes":      g.ReserveYes,
		"reserve_no":       g.ReserveNo,
		"my_shares_yes":    g.SharesYes,
		"my_shares_no":     g.SharesNo,
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

