package aimanaged

import (
	"context"
	"math/big"
)

const (
	historySourceChain = "chain"
	historySourceIPFS  = "ipfs"
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
