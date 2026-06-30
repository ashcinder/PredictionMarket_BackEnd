package aimanaged

import (
	"context"
	"math/big"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/ipfs"
)

const (
	historySourceChain = "chain"
	historySourceIPFS  = "ipfs"
	syncStatusOK       = "ok"
	syncStatusFailed   = "failed"
)

type MarketIdentity struct {
	ContractAddress string
	GameID          int
}

type HistoryObservation struct {
	Time       int64    `json:"time"`
	YesPercent float64  `json:"yes_percent"`
	NoPercent  float64  `json:"no_percent"`
	ReserveNO  *big.Int `json:"-"`
	ReserveYES *big.Int `json:"-"`
	Source     string   `json:"source"`
}

type HistoryRepository interface {
	MergeAndList(context.Context, MarketIdentity, []HistoryObservation, HistoryObservation, int) ([]HistoryObservation, error)
	List(context.Context, MarketIdentity, int) ([]HistoryObservation, error)
}

type RuleDecisionRecord struct {
	Market        MarketIdentity
	UserAddress   string
	ObservedAt    int64
	Action        string
	Reason        string
	HistoryPoints int
	Outcome       string
}

type ModelDecisionRecord struct {
	Market        MarketIdentity
	UserAddress   string
	ObservedAt    int64
	Action        string
	Confidence    float64
	Reason        string
	HistoryPoints int
}

type DecisionRepository interface {
	RecordRule(context.Context, RuleDecisionRecord) error
	CreatePending(context.Context, ModelDecisionRecord) (int64, error)
	Finalize(context.Context, int64, string, string, string) error
}

type MarketSyncState struct {
	Market         MarketIdentity
	LastSuccessAt  time.Time
	LastObservedAt int64
	FailCount      int
	NextPollAt     time.Time
	LastError      string
	Status         string
}

type SyncStateRepository interface {
	GetSyncState(context.Context, MarketIdentity) (MarketSyncState, error)
	RecordSyncSuccess(context.Context, MarketIdentity, int64, time.Time) error
	RecordSyncFailure(context.Context, MarketIdentity, time.Time, error) (MarketSyncState, error)
}

type PersistentManagedEntry struct {
	Market           MarketIdentity
	UserAddress      string
	KeyNonce         []byte
	KeyCiphertext    []byte
	EnabledAt        time.Time
	LastTradeAt      time.Time
	LastTradeOption  int
	LastTradeTx      string
	LastError        string
	LastDecisionAt   time.Time
	LastDecisionText string
}

type ManagedEntryRepository interface {
	ListManagedEntries(context.Context) ([]PersistentManagedEntry, error)
	SaveManagedEntry(context.Context, PersistentManagedEntry) error
	DeleteManagedEntry(context.Context, MarketIdentity, string) error
}

type ManagedTradeRecord struct {
	Market       MarketIdentity
	UserAddress  string
	OptionID     int
	AmountWei    *big.Int
	SharesDelta  *big.Int
	SharesYES    *big.Int
	SharesNO     *big.Int
	TotalPool    *big.Int
	ReserveYES   *big.Int
	ReserveNO    *big.Int
	TxHash       string
	TimestampSec int64
}

type ManagedTradeRepository interface {
	RecordManagedTrade(context.Context, ManagedTradeRecord) error
}

type CachedMarket struct {
	Market    MarketIdentity
	Info      *chain.GameInfo
	Extra     *chain.GameExtraData
	Metadata  *ipfs.Metadata
	UpdatedAt time.Time
}

type CachedMarketRepository interface {
	GetCachedMarket(context.Context, MarketIdentity) (*CachedMarket, error)
}

func nextSyncPollTime(failedAt time.Time, failCount int) time.Time {
	if failCount <= 1 {
		return failedAt.Add(30 * time.Second)
	}
	if failCount == 2 {
		return failedAt.Add(2 * time.Minute)
	}
	return failedAt.Add(5 * time.Minute)
}
