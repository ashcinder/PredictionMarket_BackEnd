package marketdata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"PredictionMarket/internal/chainlinkfeed"

	"github.com/ethereum/go-ethereum/common"
)

const (
	saveChainlinkRoundSQL = `INSERT INTO oracle_chainlink_rounds
		(feed_address, round_id, answer_raw, decimals, price_usd, started_at_sec,
		 updated_at_sec, answered_in_round)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		 answer_raw = VALUES(answer_raw), decimals = VALUES(decimals),
		 price_usd = VALUES(price_usd), started_at_sec = VALUES(started_at_sec),
		 updated_at_sec = VALUES(updated_at_sec), answered_in_round = VALUES(answered_in_round),
		 fetched_at = CURRENT_TIMESTAMP(6)`
	latestChainlinkRoundAtOrBeforeSQL = `SELECT feed_address, round_id, answer_raw, decimals,
		price_usd, started_at_sec, updated_at_sec, answered_in_round
		FROM oracle_chainlink_rounds
		WHERE feed_address = ? AND updated_at_sec <= ?
		ORDER BY updated_at_sec DESC LIMIT 1`
	chainlinkRoundByIDSQL = `SELECT feed_address, round_id, answer_raw, decimals,
		price_usd, started_at_sec, updated_at_sec, answered_in_round
		FROM oracle_chainlink_rounds
		WHERE feed_address = ? AND round_id = ? LIMIT 1`
)

type ChainlinkRoundRepository interface {
	Save(context.Context, chainlinkfeed.Round) error
	LatestAtOrBefore(context.Context, common.Address, time.Time) (chainlinkfeed.Round, error)
	ByID(context.Context, common.Address, *big.Int) (chainlinkfeed.Round, error)
}

type MySQLChainlinkRoundRepository struct {
	db *sql.DB
}

func NewMySQLChainlinkRoundRepository(db *sql.DB) *MySQLChainlinkRoundRepository {
	return &MySQLChainlinkRoundRepository{db: db}
}

func (r *MySQLChainlinkRoundRepository) Save(ctx context.Context, round chainlinkfeed.Round) error {
	if r == nil || r.db == nil {
		return errors.New("Chainlink round database is not configured")
	}
	if err := validateChainlinkRound(round); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, saveChainlinkRoundSQL,
		round.Feed.Hex(), round.ID.String(), round.Answer.String(), round.Decimals,
		round.Price, round.StartedAt.Unix(), round.UpdatedAt.Unix(), round.AnsweredInRound.String())
	if err != nil {
		return fmt.Errorf("save Chainlink round: %w", err)
	}
	return nil
}

func (r *MySQLChainlinkRoundRepository) LatestAtOrBefore(
	ctx context.Context, feed common.Address, boundary time.Time,
) (chainlinkfeed.Round, error) {
	if r == nil || r.db == nil {
		return chainlinkfeed.Round{}, errors.New("Chainlink round database is not configured")
	}
	if feed == (common.Address{}) || boundary.IsZero() {
		return chainlinkfeed.Round{}, errors.New("Chainlink round boundary lookup is invalid")
	}
	return scanChainlinkRound(r.db.QueryRowContext(
		ctx, latestChainlinkRoundAtOrBeforeSQL, feed.Hex(), boundary.Unix()))
}

func (r *MySQLChainlinkRoundRepository) ByID(
	ctx context.Context, feed common.Address, roundID *big.Int,
) (chainlinkfeed.Round, error) {
	if r == nil || r.db == nil {
		return chainlinkfeed.Round{}, errors.New("Chainlink round database is not configured")
	}
	if feed == (common.Address{}) || roundID == nil || roundID.Sign() <= 0 {
		return chainlinkfeed.Round{}, errors.New("Chainlink round id lookup is invalid")
	}
	return scanChainlinkRound(r.db.QueryRowContext(
		ctx, chainlinkRoundByIDSQL, feed.Hex(), roundID.String()))
}

type rowScanner interface {
	Scan(...any) error
}

func scanChainlinkRound(row rowScanner) (chainlinkfeed.Round, error) {
	var (
		round                              chainlinkfeed.Round
		feed, id, answer, answeredInRound  string
		startedAtSeconds, updatedAtSeconds int64
	)
	if err := row.Scan(
		&feed, &id, &answer, &round.Decimals, &round.Price,
		&startedAtSeconds, &updatedAtSeconds, &answeredInRound,
	); err != nil {
		return chainlinkfeed.Round{}, fmt.Errorf("scan Chainlink round: %w", err)
	}
	round.Feed = common.HexToAddress(feed)
	var ok bool
	round.ID, ok = new(big.Int).SetString(id, 10)
	if !ok {
		return chainlinkfeed.Round{}, errors.New("scan Chainlink round: invalid round id")
	}
	round.Answer, ok = new(big.Int).SetString(answer, 10)
	if !ok {
		return chainlinkfeed.Round{}, errors.New("scan Chainlink round: invalid answer")
	}
	round.AnsweredInRound, ok = new(big.Int).SetString(answeredInRound, 10)
	if !ok {
		return chainlinkfeed.Round{}, errors.New("scan Chainlink round: invalid answered round")
	}
	round.StartedAt = time.Unix(startedAtSeconds, 0).UTC()
	round.UpdatedAt = time.Unix(updatedAtSeconds, 0).UTC()
	if err := validateChainlinkRound(round); err != nil {
		return chainlinkfeed.Round{}, fmt.Errorf("scan Chainlink round: %w", err)
	}
	return round, nil
}

func validateChainlinkRound(round chainlinkfeed.Round) error {
	if round.Feed == (common.Address{}) || round.ID == nil || round.ID.Sign() <= 0 ||
		round.Answer == nil || round.Answer.Sign() <= 0 || round.Price <= 0 ||
		round.StartedAt.IsZero() || round.UpdatedAt.IsZero() ||
		round.AnsweredInRound == nil || round.AnsweredInRound.Cmp(round.ID) < 0 {
		return errors.New("Chainlink round is invalid")
	}
	return nil
}
