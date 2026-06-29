// Package apiv1 implements the v1 HTTP API for the DApp cache layer.
//
// The DApp reads from MySQL (fast, <100ms) and falls back to chain+IPFS.
// Writes flow synchronously: chain → IPFS → backend DB (via sync endpoints).
package apiv1

import (
	"context"
	"math/big"
)

// ---------------------------------------------------------------------------
// DTOs — field names match DAPP_API_ALIGNMENT.md JSON spec exactly
// ---------------------------------------------------------------------------

// GameMetaDTO is the JSON shape returned by GET /api/v1/gold/games and
// GET /api/v1/gold/games/{id}.
type GameMetaDTO struct {
	GameID          int    `json:"game_id"`
	ContractAddress string `json:"contract_address"`
	IPFSCID         string `json:"ipfs_cid"`
	Desc            string `json:"desc"`
	Condition       string `json:"condition"`
	AvatarURL       string `json:"avatar_url"`
	DetailedInfo    string `json:"detailed_info"`
	OptionYes       string `json:"option_yes"`
	OptionNo        string `json:"option_no"`
	CreatorAddress  string `json:"creator_address"`
	DeadlineSec     int64  `json:"deadline_sec"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// SyncGameRequest is the request body for POST /api/v1/gold/games/sync.
// When GameID == 0 the handler resolves the real ID from the chain via IPFS CID.
type SyncGameRequest struct {
	GameID          int    `json:"game_id"`
	ContractAddress string `json:"contract_address"`
	IPFSCID         string `json:"ipfs_cid"`
	Desc            string `json:"desc"`
	Condition       string `json:"condition"`
	AvatarURL       string `json:"avatar_url"`
	DetailedInfo    string `json:"detailed_info"`
	OptionYes       string `json:"option_yes"`
	OptionNo        string `json:"option_no"`
	CreatorAddress  string `json:"creator_address"`
	DurationSec     int64  `json:"duration_sec"`
	DeadlineSec     int64  `json:"deadline_sec"`
	InitialLiquidity string `json:"initial_liquidity_wei"`
}

// SyncGameResponse is the JSON response for POST /api/v1/gold/games/sync.
type SyncGameResponse struct {
	Success bool `json:"success"`
	GameID  int  `json:"game_id"`
}

// ChainStateDTO is the response shape for GET .../{id}/chain-state and
// GET .../chain-states. It merges on-chain data with user position data.
type ChainStateDTO struct {
	GameID        int    `json:"game_id"`
	TotalPool     string `json:"total_pool"`
	IsResolved    bool   `json:"is_resolved"`
	IsRefunded    bool   `json:"is_refunded"`
	WinningOption int    `json:"winning_option"`
	DeadlineSec   int64  `json:"deadline_sec"`
	ReserveYes    string `json:"reserve_yes"`
	ReserveNo     string `json:"reserve_no"`
	MySharesYes   string `json:"my_shares_yes"`
	MySharesNo    string `json:"my_shares_no"`
	UpdatedAt     string `json:"updated_at"`
}

// SyncChainStateRequest is the request body for
// POST /api/v1/gold/games/{id}/chain-state/sync.
type SyncChainStateRequest struct {
	TotalPool     string `json:"total_pool"`
	IsResolved    bool   `json:"is_resolved"`
	IsRefunded    bool   `json:"is_refunded"`
	WinningOption int    `json:"winning_option"`
	DeadlineSec   int64  `json:"deadline_sec"`
	ReserveYes    string `json:"reserve_yes"`
	ReserveNo     string `json:"reserve_no"`
	MySharesYes   string `json:"my_shares_yes"`
	MySharesNo    string `json:"my_shares_no"`
	UserAddress   string `json:"user_address"`
}

// PricePointDTO is the JSON shape for GET /api/v1/gold/games/{id}/history.
type PricePointDTO struct {
	GameID       int     `json:"game_id"`
	TimestampSec int64   `json:"timestamp_sec"`
	YesPrice     float64 `json:"yes_price"`
	NoPrice      float64 `json:"no_price"`
	TotalPool    string  `json:"total_pool"`
}

// AddHistoryRequest is the request body for POST /api/v1/gold/games/{id}/history.
type AddHistoryRequest struct {
	GameID       int    `json:"game_id"`
	TimestampSec int64  `json:"timestamp_sec"`
	YesPrice     float64 `json:"yes_price"`
	NoPrice      float64 `json:"no_price"`
	TotalPool    string `json:"total_pool"`
}

// SyncTradeRequest is the request body for POST /api/v1/gold/trades/sync.
type SyncTradeRequest struct {
	GameID        int    `json:"game_id"`
	ContractAddr  string `json:"contract_address"`
	UserAddress   string `json:"user_address"`
	TradeType     string `json:"trade_type"`
	OptionID      int    `json:"option_id"`
	AmountWei     string `json:"amount_wei"`
	TxHash        string `json:"tx_hash"`
	IsSuccess     bool   `json:"is_success"`
	// Post-trade state fields (optional — when present, chain state and user
	// positions are also upserted).
	TotalPoolAfter    string `json:"total_pool_after"`
	ReserveYesAfter   string `json:"reserve_yes_after"`
	ReserveNoAfter    string `json:"reserve_no_after"`
	MySharesYesAfter  string `json:"my_shares_yes_after"`
	MySharesNoAfter   string `json:"my_shares_no_after"`
	// Trade detail fields (optional — stored for the position detail API).
	SharesWei     string  `json:"shares_wei"`
	PriceAtTrade  float64 `json:"price_at_trade"`
	TimestampSec  int64   `json:"timestamp_sec"`
	// v1.1 new fields.
	ShareAmountWei string `json:"share_amount_wei"`
	IsAiManaged    bool   `json:"is_ai_managed"`
}

// PositionDetailDTO is the response shape for
// GET /api/v1/gold/games/{id}/positions?user_address=...
type PositionDetailDTO struct {
	GameID        int               `json:"game_id"`
	Desc          string            `json:"desc"`
	Condition     string            `json:"condition"`
	AvatarURL     string            `json:"avatar_url"`
	OptionNames   []string          `json:"option_names"`
	IsResolved    bool              `json:"is_resolved"`
	IsRefunded    bool              `json:"is_refunded"`
	WinningOption int               `json:"winning_option"`
	DeadlineSec   int64             `json:"deadline_sec"`
	TotalPool     string            `json:"total_pool"`
	ReserveYes    string            `json:"reserve_yes"`
	ReserveNo     string            `json:"reserve_no"`
	MySharesYes   string            `json:"my_shares_yes"`
	MySharesNo    string            `json:"my_shares_no"`
	Trades        []TradeRecordDTO  `json:"trades"`
}

// TradeRecordDTO represents a single trade record in the position detail response.
type TradeRecordDTO struct {
	TradeID      int64   `json:"trade_id"`
	TradeType    string  `json:"trade_type"`
	OptionID     int     `json:"option_id"`
	OptionName   string  `json:"option_name"`
	AmountWei    string  `json:"amount_wei"`
	SharesWei    string  `json:"shares_wei"`
	PriceAtTrade float64 `json:"price_at_trade"`
	TxHash       string  `json:"tx_hash"`
	TimestampSec int64   `json:"timestamp_sec"`
	IsSuccess    bool    `json:"is_success"`
	IsAiManaged  bool    `json:"is_ai_managed"`
}

// TradeHistoryItemDTO is the JSON shape returned by GET /api/v1/gold/trades.
type TradeHistoryItemDTO struct {
	TradeType      string `json:"trade_type"`
	OptionID       int    `json:"option_id"`
	AmountWei      string `json:"amount_wei"`
	ShareAmountWei string `json:"share_amount_wei"`
	IsSuccess      bool   `json:"is_success"`
	IsAiManaged    bool   `json:"is_ai_managed"`
	TxHash         string `json:"tx_hash"`
	CreatedAt      string `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Internal row types (database representation, not exported as JSON)
// ---------------------------------------------------------------------------

type gameRow struct {
	GameID          int
	ContractAddress string
	IPFSCID         string
	Desc            string
	Condition       string
	AvatarURL       string
	DetailedInfo    string
	OptionYes       string
	OptionNo        string
	CreatorAddress  string
	DeadlineSec     int64
	CreatedAt       string
	UpdatedAt       string
}

type chainStateRow struct {
	GameID        int
	TotalPool     *big.Int
	IsResolved    bool
	IsRefunded    bool
	WinningOption int
	DeadlineSec   int64
	ReserveYes    *big.Int
	ReserveNo     *big.Int
	UpdatedAt     string
}

type userPositionRow struct {
	UserAddress string
	GameID      int
	MySharesYes  *big.Int
	MySharesNo   *big.Int
	UpdatedAt   string
}

type priceHistoryRow struct {
	ID           int64
	GameID       int
	TimestampSec int64
	YesPrice     float64
	NoPrice      float64
	TotalPool    *big.Int
}

type tradeRow struct {
	ID              int64
	GameID          int
	ContractAddress string
	UserAddress     string
	TradeType       string
	OptionID        int
	AmountWei       *big.Int
	SharesWei       *big.Int
	PriceAtTrade    float64
	TimestampSec    int64
	TxHash          string
	IsSuccess       bool
	IsAiManaged     bool
	CreatedAt       string
}

// ---------------------------------------------------------------------------
// Repository interfaces
// ---------------------------------------------------------------------------

// GameMetadataRepository persists game metadata synced from the DApp after
// game creation (the DApp uploads to IPFS first, then creates on chain, and
// finally syncs metadata here).
type GameMetadataRepository interface {
	ListAllGames(ctx context.Context) ([]GameMetaDTO, error)
	GetGameByID(ctx context.Context, gameID int) (*GameMetaDTO, error)
	UpsertGame(ctx context.Context, game *gameRow) (int, error)
	// InsertGameStub creates a minimal row (game_id, contract, ipfs_cid) if
	// one does not already exist. It never overwrites existing metadata.
	InsertGameStub(ctx context.Context, gameID int, contractAddress, ipfsCID string) error
}

// ChainStateRepository caches on-chain contract state so the DApp can read it
// from MySQL instead of making slow eth_call requests.
type ChainStateRepository interface {
	GetChainState(ctx context.Context, gameID int) (*chainStateRow, error)
	ListAllChainStates(ctx context.Context) ([]chainStateRow, error)
	UpsertChainState(ctx context.Context, state *chainStateRow) error
}

// UserPositionRepository persists per-user share balances so the DApp can
// list positions without calling getGameExtraData for every game.
type UserPositionRepository interface {
	GetUserPosition(ctx context.Context, userAddress string, gameID int) (*userPositionRow, error)
	ListUserPositions(ctx context.Context, userAddress string) ([]userPositionRow, error)
	UpsertUserPosition(ctx context.Context, pos *userPositionRow) error
}

// PriceHistoryRepository stores YES/NO price snapshots for the chart on the
// game detail page.
type PriceHistoryRepository interface {
	ListHistory(ctx context.Context, gameID int, limit int) ([]PricePointDTO, error)
	AppendHistory(ctx context.Context, point *priceHistoryRow) error
}

// TradeRepository records every buy/sell/claim/resolve trade synced by the
// DApp after transaction confirmation.
type TradeRepository interface {
	RecordTrade(ctx context.Context, trade *tradeRow) error
	ListTradesByGameAndUser(ctx context.Context, gameID int, userAddress string) ([]TradeRecordDTO, error)
}
