package apiv1

import (
	"context"
	"net/http"

	"PredictionMarket/internal/aimanaged"
	"PredictionMarket/internal/ipfs"
)

// chainClient is the subset of chain.Client used by the v1 API handlers.
type chainClient interface {
	EthCall(ctx context.Context, data string) (string, error)
	WalletAddress() string
}

type metadataClient interface {
	DownloadMetadata(cid string) (*ipfs.Metadata, error)
}

// Server serves the /api/v1/gold/... HTTP endpoints that provide the DApp
// cache layer (MySQL-first reads with chain+IPFS fallback).
type Server struct {
	games        GameMetadataRepository
	chainStates  ChainStateRepository
	positions    UserPositionRepository
	history      PriceHistoryRepository
	trades       TradeRepository
	aiStore      *aimanaged.Store
	chain        chainClient    // optional, may be nil
	metadata     metadataClient // optional, may be nil
	contractAddr string
	historyMax   int
}

// NewServer creates a v1 API server. The same *MySQLRepository can be passed
// for all five repository arguments (it implements all interfaces). chain may
// be nil when no on-demand chain sampling is desired.
func NewServer(
	games GameMetadataRepository,
	chainStates ChainStateRepository,
	positions UserPositionRepository,
	history PriceHistoryRepository,
	trades TradeRepository,
	aiStore *aimanaged.Store,
	chainClient chainClient,
	metadataClient metadataClient,
	contractAddr string,
	historyMax int,
) *Server {
	return &Server{
		games:        games,
		chainStates:  chainStates,
		positions:    positions,
		history:      history,
		trades:       trades,
		aiStore:      aiStore,
		chain:        chainClient,
		metadata:     metadataClient,
		contractAddr: contractAddr,
		historyMax:   historyMax,
	}
}

// Register mounts all v1 routes on mux using Go 1.22+ pattern syntax.
func (s *Server) Register(mux *http.ServeMux) {
	// Game metadata
	mux.HandleFunc("GET /api/v1/gold/games", s.handleListGames)
	mux.HandleFunc("GET /api/v1/gold/games/{id}", s.handleGetGame)
	mux.HandleFunc("POST /api/v1/gold/games/sync", s.handleSyncGame)

	// Chain state cache (literal "chain-states" must be registered before
	// the wildcard "{id}/chain-state" so Go's most-specific-wins routing
	// dispatches correctly).
	mux.HandleFunc("GET /api/v1/gold/games/chain-states", s.handleListChainStates)
	mux.HandleFunc("GET /api/v1/gold/games/{id}/chain-state", s.handleGetChainState)
	mux.HandleFunc("POST /api/v1/gold/games/{id}/chain-state/sync", s.handleSyncChainState)

	// Position detail (game info + user shares + trade history).
	mux.HandleFunc("GET /api/v1/gold/games/{id}/positions", s.handleGetPositions)

	// Price history
	mux.HandleFunc("GET /api/v1/gold/games/{id}/history", s.handleGetHistory)
	mux.HandleFunc("POST /api/v1/gold/games/{id}/history", s.handleAddHistory)

	// Trade history & sync
	mux.HandleFunc("GET /api/v1/gold/trades", s.handleGetTrades)
	mux.HandleFunc("POST /api/v1/gold/trades/sync", s.handleSyncTrade)

	// AI-managed (delegates to existing aimanaged.Store)
	mux.HandleFunc("GET /api/v1/gold/ai-managed", s.handleAIGet)
	mux.HandleFunc("POST /api/v1/gold/ai-managed", s.handleAISet)
}
