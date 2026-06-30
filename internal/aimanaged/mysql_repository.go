package aimanaged

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/database"
	"PredictionMarket/internal/ipfs"

	"github.com/ethereum/go-ethereum/common"
)

const repositoryOperationTimeout = 10 * time.Second

const (
	insertIPFSHistorySQL = `INSERT IGNORE INTO market_history
	(contract_address, game_id, observed_at, yes_percent, no_percent, reserve_no, reserve_yes, source)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	upsertChainHistorySQL = `INSERT INTO market_history
	(contract_address, game_id, observed_at, yes_percent, no_percent, reserve_no, reserve_yes, source)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE yes_percent=VALUES(yes_percent), no_percent=VALUES(no_percent),
	reserve_no=VALUES(reserve_no), reserve_yes=VALUES(reserve_yes), source='chain'`
	selectHistorySQL = `SELECT observed_at, yes_percent, no_percent, reserve_no, reserve_yes, source
	FROM market_history WHERE contract_address=? AND game_id=?
	ORDER BY observed_at DESC LIMIT ?`
	insertDecisionSQL = `INSERT INTO ai_decisions
	(contract_address, game_id, user_address, observed_at, decision_source, action,
	confidence, reason, history_points, outcome)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	finalizeDecisionSQL = `UPDATE ai_decisions SET outcome=?, tx_hash=?, error_summary=? WHERE id=?`
	pruneHistorySQL     = `DELETE FROM market_history
	WHERE contract_address = ? AND game_id = ?
	AND observed_at NOT IN (
		SELECT * FROM (
			SELECT observed_at FROM market_history
			WHERE contract_address = ? AND game_id = ?
			ORDER BY observed_at DESC LIMIT ?
		) keep
	)`
	selectSyncStateSQL = `SELECT last_success_at, last_observed_at, fail_count, next_poll_at, last_error, status
FROM market_sync_state WHERE contract_address=? AND game_id=?`
	upsertSyncSuccessSQL = `INSERT INTO market_sync_state
(contract_address, game_id, last_success_at, last_observed_at, fail_count, next_poll_at, last_error, status)
VALUES (?, ?, ?, ?, 0, ?, '', ?)
ON DUPLICATE KEY UPDATE last_success_at=VALUES(last_success_at),
last_observed_at=VALUES(last_observed_at), fail_count=0, next_poll_at=VALUES(next_poll_at),
last_error='', status=VALUES(status)`
	upsertSyncFailureSQL = `INSERT INTO market_sync_state
(contract_address, game_id, last_success_at, last_observed_at, fail_count, next_poll_at, last_error, status)
VALUES (?, ?, NULL, 0, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE fail_count=VALUES(fail_count), next_poll_at=VALUES(next_poll_at),
last_error=VALUES(last_error), status=VALUES(status)`
	selectManagedEntriesSQL = `SELECT contract_address, game_id, user_address, key_nonce, key_ciphertext,
enabled_at, last_trade_at, last_trade_option, last_trade_tx, last_error, last_decision_at, last_decision_text
FROM ai_managed_entries`
	selectCachedMarketSQL = "SELECT g.ipfs_cid, g.`desc`, g.`condition`, g.detailed_info, g.option_yes, g.option_no, " +
		"COALESCE(NULLIF(cs.deadline_sec, 0), g.deadline_sec) AS deadline_sec, " +
		"cs.total_pool, cs.is_resolved, cs.is_refunded, cs.winning_option, cs.reserve_yes, cs.reserve_no, cs.updated_at " +
		"FROM gold_games g LEFT JOIN gold_chain_states cs ON cs.contract_address = g.contract_address AND cs.game_id = g.game_id " +
		"WHERE g.contract_address = ? AND g.game_id = ?"
	upsertManagedEntrySQL = `INSERT INTO ai_managed_entries
(contract_address, game_id, user_address, key_nonce, key_ciphertext, enabled_at,
last_trade_at, last_trade_option, last_trade_tx, last_error, last_decision_at, last_decision_text)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE key_nonce=VALUES(key_nonce), key_ciphertext=VALUES(key_ciphertext),
enabled_at=VALUES(enabled_at), last_trade_at=VALUES(last_trade_at),
last_trade_option=VALUES(last_trade_option), last_trade_tx=VALUES(last_trade_tx),
last_error=VALUES(last_error), last_decision_at=VALUES(last_decision_at),
last_decision_text=VALUES(last_decision_text)`
	deleteManagedEntrySQL = `DELETE FROM ai_managed_entries
WHERE contract_address=? AND game_id=? AND user_address=?`
	insertManagedGoldTradeSQL = `INSERT INTO gold_trades
(game_id, contract_address, user_address, trade_type, option_id, amount_wei,
share_amount_wei, shares_wei, price_at_trade, timestamp_sec, tx_hash, is_success,
is_ai_managed, my_shares_yes_after, my_shares_no_after)
VALUES (?, ?, ?, 'BUY', ?, ?, ?, ?, 0, ?, ?, 1, 1, ?, ?)`
	updateManagedGoldTradeSQL = `UPDATE gold_trades SET
option_id=?, amount_wei=?, share_amount_wei=?, shares_wei=?, timestamp_sec=?,
is_success=1, is_ai_managed=1, my_shares_yes_after=?, my_shares_no_after=?
WHERE contract_address=? AND game_id=? AND user_address=? AND tx_hash=?`
	selectManagedGoldTradeSQL = `SELECT id FROM gold_trades
WHERE contract_address=? AND game_id=? AND user_address=? AND tx_hash=? LIMIT 1`
	upsertManagedUserPositionSQL = `INSERT INTO gold_user_positions
(user_address, game_id, my_shares_yes, my_shares_no)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
my_shares_yes=VALUES(my_shares_yes), my_shares_no=VALUES(my_shares_no)`
	upsertManagedChainStateSQL = `INSERT INTO gold_chain_states
(contract_address, game_id, total_pool, is_resolved, is_refunded, winning_option,
deadline_sec, reserve_yes, reserve_no)
VALUES (?, ?, ?, 0, 0, 0, 0, ?, ?)
ON DUPLICATE KEY UPDATE
total_pool=COALESCE(VALUES(total_pool), total_pool),
reserve_yes=VALUES(reserve_yes), reserve_no=VALUES(reserve_no)`
	incrementManagedChainStateSQL = `INSERT INTO gold_chain_states
(contract_address, game_id, total_pool, is_resolved, is_refunded, winning_option,
deadline_sec, reserve_yes, reserve_no)
VALUES (?, ?, ?, 0, 0, 0, 0, ?, ?)
ON DUPLICATE KEY UPDATE
total_pool=CAST(CAST(total_pool AS CHAR) AS DECIMAL(65,0)) + CAST(? AS DECIMAL(65,0)),
reserve_yes=VALUES(reserve_yes), reserve_no=VALUES(reserve_no)`
	upsertManagedPriceHistorySQL = `INSERT INTO gold_price_history
(game_id, timestamp_sec, yes_price, no_price, total_pool)
VALUES (?, ?, ?, ?, NULL)
ON DUPLICATE KEY UPDATE yes_price=VALUES(yes_price), no_price=VALUES(no_price)`
)

type MySQLRepository struct {
	db       *sql.DB
	ensureMu sync.Mutex
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) GetCachedMarket(ctx context.Context, market MarketIdentity) (*CachedMarket, error) {
	cached, err := r.getCachedMarket(ctx, market)
	if err != nil && r.isTableNotFound(err) {
		slog.Warn("mysql repository: table missing, recreating and retrying", "op", "get_cached_market")
		r.ensureTables(ctx)
		cached, err = r.getCachedMarket(ctx, market)
	}
	return cached, err
}

func (r *MySQLRepository) getCachedMarket(ctx context.Context, market MarketIdentity) (*CachedMarket, error) {
	if err := validateMarketAndLimit(market, 1); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()

	var ipfsCID, desc, condition, detail, optionYES, optionNO sql.NullString
	var deadlineSec sql.NullInt64
	var totalPoolBytes, reserveYESBytes, reserveNOBytes []byte
	var isResolved, isRefunded, winningOption sql.NullInt64
	var updatedAt sql.NullTime
	contract := normalizeAddress(market.ContractAddress)
	err := r.db.QueryRowContext(ctx, selectCachedMarketSQL, contract, market.GameID).Scan(
		&ipfsCID, &desc, &condition, &detail, &optionYES, &optionNO,
		&deadlineSec, &totalPoolBytes, &isResolved, &isRefunded, &winningOption,
		&reserveYESBytes, &reserveNOBytes, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("cached game %s/%d not found", contract, market.GameID)
	}
	if err != nil {
		return nil, fmt.Errorf("query cached market: %w", err)
	}
	reserveYES, err := parseReserveBytes(reserveYESBytes)
	if err != nil {
		return nil, fmt.Errorf("parse cached reserve_yes: %w", err)
	}
	reserveNO, err := parseReserveBytes(reserveNOBytes)
	if err != nil {
		return nil, fmt.Errorf("parse cached reserve_no: %w", err)
	}
	if reserveYES == nil || reserveNO == nil {
		return nil, fmt.Errorf("cached reserves are missing")
	}
	totalPool, err := parseReserveBytes(totalPoolBytes)
	if err != nil {
		return nil, fmt.Errorf("parse cached total_pool: %w", err)
	}
	if totalPool == nil {
		totalPool = new(big.Int).Add(new(big.Int).Set(reserveYES), reserveNO)
	}

	info := &chain.GameInfo{
		ID:            market.GameID,
		IPFSCID:       ipfsCID.String,
		TotalPool:     totalPool,
		DeadlineRaw:   deadlineSec.Int64,
		IsResolved:    isResolved.Valid && isResolved.Int64 != 0,
		IsRefunded:    isRefunded.Valid && isRefunded.Int64 != 0,
		WinningOption: int(winningOption.Int64),
	}
	meta := &ipfs.Metadata{
		Desc:         desc.String,
		Condition:    condition.String,
		DetailedInfo: detail.String,
		OptionYES:    emptyDefault(optionYES.String, "YES"),
		OptionNO:     emptyDefault(optionNO.String, "NO"),
	}
	extra := &chain.GameExtraData{
		VirtualReservesNOYES: []*big.Int{reserveNO, reserveYES},
		MySharesYESNO:        []*big.Int{big.NewInt(0), big.NewInt(0)},
	}
	cached := &CachedMarket{
		Market:   MarketIdentity{ContractAddress: contract, GameID: market.GameID},
		Info:     info,
		Extra:    extra,
		Metadata: meta,
	}
	if updatedAt.Valid {
		cached.UpdatedAt = updatedAt.Time
	}
	return cached, nil
}

func (r *MySQLRepository) ListManagedEntries(ctx context.Context) ([]PersistentManagedEntry, error) {
	rows, err := r.listManagedEntries(ctx)
	if err != nil && r.isTableNotFound(err) {
		slog.Warn("mysql repository: table missing, recreating and retrying", "op", "list_managed_entries")
		r.ensureTables(ctx)
		rows, err = r.listManagedEntries(ctx)
	}
	return rows, err
}

func (r *MySQLRepository) listManagedEntries(ctx context.Context) ([]PersistentManagedEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, selectManagedEntriesSQL)
	if err != nil {
		return nil, fmt.Errorf("list ai-managed entries: %w", err)
	}
	defer rows.Close()

	var out []PersistentManagedEntry
	for rows.Next() {
		var item PersistentManagedEntry
		var contract string
		var gameID int
		var enabledAt time.Time
		var lastTradeAt sql.NullTime
		var lastDecisionAt sql.NullTime
		var lastTradeTx, lastError, lastDecisionText sql.NullString
		if err := rows.Scan(
			&contract, &gameID, &item.UserAddress, &item.KeyNonce, &item.KeyCiphertext,
			&enabledAt, &lastTradeAt, &item.LastTradeOption, &lastTradeTx, &lastError,
			&lastDecisionAt, &lastDecisionText,
		); err != nil {
			return nil, fmt.Errorf("scan ai-managed entry: %w", err)
		}
		item.Market = MarketIdentity{ContractAddress: normalizeAddress(contract), GameID: gameID}
		item.UserAddress = common.HexToAddress(item.UserAddress).Hex()
		item.EnabledAt = enabledAt
		if lastTradeAt.Valid {
			item.LastTradeAt = lastTradeAt.Time
		}
		if lastDecisionAt.Valid {
			item.LastDecisionAt = lastDecisionAt.Time
		}
		item.LastTradeTx = lastTradeTx.String
		item.LastError = lastError.String
		item.LastDecisionText = lastDecisionText.String
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ai-managed entries: %w", err)
	}
	return out, nil
}

func (r *MySQLRepository) SaveManagedEntry(ctx context.Context, item PersistentManagedEntry) error {
	if err := validateMarketAndLimit(item.Market, 1); err != nil {
		return err
	}
	if !common.IsHexAddress(item.UserAddress) {
		return fmt.Errorf("invalid user address")
	}
	if len(item.KeyNonce) == 0 || len(item.KeyCiphertext) == 0 {
		return fmt.Errorf("missing encrypted private key")
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	contract := normalizeAddress(item.Market.ContractAddress)
	user := common.HexToAddress(item.UserAddress).Hex()
	if _, err := r.db.ExecContext(ctx, upsertManagedEntrySQL,
		contract, item.Market.GameID, user, item.KeyNonce, item.KeyCiphertext, item.EnabledAt.UTC(),
		nullableTime(item.LastTradeAt), item.LastTradeOption, item.LastTradeTx, item.LastError,
		nullableTime(item.LastDecisionAt), item.LastDecisionText,
	); err != nil {
		return fmt.Errorf("save ai-managed entry: %w", err)
	}
	return nil
}

func (r *MySQLRepository) DeleteManagedEntry(ctx context.Context, market MarketIdentity, userAddress string) error {
	if err := validateMarketAndLimit(market, 1); err != nil {
		return err
	}
	if !common.IsHexAddress(userAddress) {
		return fmt.Errorf("invalid user address")
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	if _, err := r.db.ExecContext(ctx, deleteManagedEntrySQL,
		normalizeAddress(market.ContractAddress), market.GameID, common.HexToAddress(userAddress).Hex(),
	); err != nil {
		return fmt.Errorf("delete ai-managed entry: %w", err)
	}
	return nil
}

func (r *MySQLRepository) RecordManagedTrade(ctx context.Context, record ManagedTradeRecord) error {
	err := r.recordManagedTrade(ctx, record)
	if err != nil && r.isTableNotFound(err) {
		slog.Warn("mysql repository: table missing, recreating and retrying", "op", "record_managed_trade")
		r.ensureTables(ctx)
		err = r.recordManagedTrade(ctx, record)
	}
	return err
}

func (r *MySQLRepository) recordManagedTrade(ctx context.Context, record ManagedTradeRecord) error {
	if err := validateManagedTrade(record); err != nil {
		return err
	}
	record.SharesDelta = nonNilBigInt(record.SharesDelta)
	record.SharesYES = nonNilBigInt(record.SharesYES)
	record.SharesNO = nonNilBigInt(record.SharesNO)
	amountWei, err := reserveBytes(record.AmountWei)
	if err != nil {
		return fmt.Errorf("amount_wei: %w", err)
	}
	sharesDelta, err := reserveBytes(record.SharesDelta)
	if err != nil {
		return fmt.Errorf("shares_delta: %w", err)
	}
	sharesYES, err := reserveBytes(record.SharesYES)
	if err != nil {
		return fmt.Errorf("shares_yes: %w", err)
	}
	sharesNO, err := reserveBytes(record.SharesNO)
	if err != nil {
		return fmt.Errorf("shares_no: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin managed trade sync: %w", err)
	}
	defer tx.Rollback()

	contract := normalizeAddress(record.Market.ContractAddress)
	user := normalizeAddress(record.UserAddress)
	result, err := tx.ExecContext(ctx, updateManagedGoldTradeSQL,
		record.OptionID,
		amountWei,
		decimalString(record.SharesDelta),
		sharesDelta,
		record.TimestampSec,
		decimalString(record.SharesYES),
		decimalString(record.SharesNO),
		contract,
		record.Market.GameID,
		user,
		record.TxHash,
	)
	if err != nil {
		return fmt.Errorf("update managed gold trade: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect managed gold trade update: %w", err)
	}
	insertedTrade := false
	if affected == 0 {
		var existingID int64
		findErr := tx.QueryRowContext(ctx, selectManagedGoldTradeSQL,
			contract, record.Market.GameID, user, record.TxHash,
		).Scan(&existingID)
		if findErr != nil && !errors.Is(findErr, sql.ErrNoRows) {
			return fmt.Errorf("find managed gold trade: %w", findErr)
		}
		if errors.Is(findErr, sql.ErrNoRows) {
			if _, err := tx.ExecContext(ctx, insertManagedGoldTradeSQL,
				record.Market.GameID,
				contract,
				user,
				record.OptionID,
				amountWei,
				decimalString(record.SharesDelta),
				sharesDelta,
				record.TimestampSec,
				record.TxHash,
				decimalString(record.SharesYES),
				decimalString(record.SharesNO),
			); err != nil {
				return fmt.Errorf("insert managed gold trade: %w", err)
			}
			insertedTrade = true
		}
	}
	if _, err := tx.ExecContext(ctx, upsertManagedUserPositionSQL,
		user,
		record.Market.GameID,
		sharesYES,
		sharesNO,
	); err != nil {
		return fmt.Errorf("upsert managed user position: %w", err)
	}
	if record.ReserveYES != nil && record.ReserveNO != nil {
		var totalPool interface{}
		if record.TotalPool != nil {
			totalPool, err = reserveBytes(record.TotalPool)
			if err != nil {
				return fmt.Errorf("total_pool: %w", err)
			}
		}
		reserveYES, reserveErr := reserveBytes(record.ReserveYES)
		if reserveErr != nil {
			return fmt.Errorf("reserve_yes: %w", reserveErr)
		}
		reserveNO, reserveErr := reserveBytes(record.ReserveNO)
		if reserveErr != nil {
			return fmt.Errorf("reserve_no: %w", reserveErr)
		}
		if insertedTrade {
			if _, err := tx.ExecContext(ctx, incrementManagedChainStateSQL,
				contract, record.Market.GameID, totalPool, reserveYES, reserveNO, amountWei,
			); err != nil {
				return fmt.Errorf("increment managed chain state: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, upsertManagedChainStateSQL,
				contract, record.Market.GameID, nil, reserveYES, reserveNO,
			); err != nil {
				return fmt.Errorf("upsert managed chain state: %w", err)
			}
		}
		if record.SharesDelta.Sign() > 0 {
			yesPercent, noPercent := percentagesFromManagedReserves(record.ReserveYES, record.ReserveNO)
			if _, err := tx.ExecContext(ctx, upsertManagedPriceHistorySQL,
				record.Market.GameID, record.TimestampSec,
				formatPercentage(yesPercent), formatPercentage(noPercent),
			); err != nil {
				return fmt.Errorf("upsert managed price history: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit managed trade sync: %w", err)
	}
	return nil
}

func percentagesFromManagedReserves(reserveYES, reserveNO *big.Int) (float64, float64) {
	total := new(big.Int).Add(nonNilBigInt(reserveYES), nonNilBigInt(reserveNO))
	if total.Sign() <= 0 {
		return 50, 50
	}
	yesRat := new(big.Rat).SetFrac(nonNilBigInt(reserveNO), total)
	yes, _ := yesRat.Float64()
	yes *= 100
	return yes, 100 - yes
}

// ensureTables recreates all required tables (CREATE TABLE IF NOT EXISTS).
// It serialises concurrent callers so only one goroutine runs the DDL while
// the others wait and then proceed.
func (r *MySQLRepository) ensureTables(ctx context.Context) {
	r.ensureMu.Lock()
	defer r.ensureMu.Unlock()
	if err := database.EnsureTables(ctx, r.db); err != nil {
		slog.Error("mysql repository: ensure tables failed", "error", err)
	}
}

// isTableNotFound reports whether err indicates a missing table.
func (r *MySQLRepository) isTableNotFound(err error) bool {
	return database.IsTableNotFound(err)
}

func (r *MySQLRepository) MergeAndList(ctx context.Context, market MarketIdentity, seed []HistoryObservation, current HistoryObservation, limit int) ([]HistoryObservation, error) {
	points, err := r.mergeAndList(ctx, market, seed, current, limit)
	if err != nil && r.isTableNotFound(err) {
		slog.Warn("mysql repository: table missing, recreating and retrying", "op", "merge_and_list")
		r.ensureTables(ctx)
		points, err = r.mergeAndList(ctx, market, seed, current, limit)
	}
	return points, err
}

func (r *MySQLRepository) mergeAndList(ctx context.Context, market MarketIdentity, seed []HistoryObservation, current HistoryObservation, limit int) ([]HistoryObservation, error) {
	if err := validateMarketAndLimit(market, limit); err != nil {
		return nil, err
	}
	for _, point := range seed {
		if err := validateObservation(point, historySourceIPFS); err != nil {
			return nil, fmt.Errorf("validate IPFS history: %w", err)
		}
	}
	if err := validateObservation(current, historySourceChain); err != nil {
		return nil, fmt.Errorf("validate chain history: %w", err)
	}
	reserveNO, err := reserveBytes(current.ReserveNO)
	if err != nil {
		return nil, fmt.Errorf("reserve_no: %w", err)
	}
	reserveYES, err := reserveBytes(current.ReserveYES)
	if err != nil {
		return nil, fmt.Errorf("reserve_yes: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin history merge: %w", err)
	}
	defer tx.Rollback()

	contract := normalizeAddress(market.ContractAddress)
	for _, point := range seed {
		if _, err := tx.ExecContext(ctx, insertIPFSHistorySQL,
			contract, market.GameID, point.Time,
			formatPercentage(point.YesPercent), formatPercentage(point.NoPercent),
			nil, nil, historySourceIPFS,
		); err != nil {
			return nil, fmt.Errorf("insert IPFS history: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, upsertChainHistorySQL,
		contract, market.GameID, current.Time,
		formatPercentage(current.YesPercent), formatPercentage(current.NoPercent),
		reserveNO, reserveYES, historySourceChain,
	); err != nil {
		return nil, fmt.Errorf("upsert chain history: %w", err)
	}
	// Prune old rows: keep only the most recent `limit` observations
	// per game so the table does not grow without bound.
	if _, err := tx.ExecContext(ctx, pruneHistorySQL,
		contract, market.GameID,
		contract, market.GameID, limit,
	); err != nil {
		return nil, fmt.Errorf("prune history: %w", err)
	}
	points, err := queryHistory(ctx, tx, contract, market.GameID, limit)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit history merge: %w", err)
	}
	return points, nil
}

func (r *MySQLRepository) List(ctx context.Context, market MarketIdentity, limit int) ([]HistoryObservation, error) {
	points, err := r.list(ctx, market, limit)
	if err != nil && r.isTableNotFound(err) {
		slog.Warn("mysql repository: table missing, recreating and retrying", "op", "list")
		r.ensureTables(ctx)
		points, err = r.list(ctx, market, limit)
	}
	return points, err
}

func (r *MySQLRepository) list(ctx context.Context, market MarketIdentity, limit int) ([]HistoryObservation, error) {
	if err := validateMarketAndLimit(market, limit); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	return queryHistory(ctx, r.db, normalizeAddress(market.ContractAddress), market.GameID, limit)
}

func (r *MySQLRepository) GetSyncState(ctx context.Context, market MarketIdentity) (MarketSyncState, error) {
	if err := validateMarketAndLimit(market, 1); err != nil {
		return MarketSyncState{}, err
	}
	contract := normalizeAddress(market.ContractAddress)
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	var lastSuccess sql.NullTime
	var lastObserved sql.NullInt64
	var nextPoll sql.NullTime
	state := MarketSyncState{Market: MarketIdentity{ContractAddress: contract, GameID: market.GameID}}
	err := r.db.QueryRowContext(ctx, selectSyncStateSQL, contract, market.GameID).Scan(
		&lastSuccess, &lastObserved, &state.FailCount, &nextPoll, &state.LastError, &state.Status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return MarketSyncState{}, fmt.Errorf("query market sync state: %w", err)
	}
	if lastSuccess.Valid {
		state.LastSuccessAt = lastSuccess.Time
	}
	if lastObserved.Valid {
		state.LastObservedAt = lastObserved.Int64
	}
	if nextPoll.Valid {
		state.NextPollAt = nextPoll.Time
	}
	return state, nil
}

func (r *MySQLRepository) RecordSyncSuccess(ctx context.Context, market MarketIdentity, observedAt int64, syncedAt time.Time) error {
	if err := validateMarketAndLimit(market, 1); err != nil {
		return err
	}
	if observedAt <= 0 {
		return errors.New("observed_at must be positive")
	}
	contract := normalizeAddress(market.ContractAddress)
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	if _, err := r.db.ExecContext(ctx, upsertSyncSuccessSQL,
		contract, market.GameID, syncedAt.UTC(), observedAt, syncedAt.UTC(), syncStatusOK,
	); err != nil {
		return fmt.Errorf("record market sync success: %w", err)
	}
	return nil
}

func (r *MySQLRepository) RecordSyncFailure(ctx context.Context, market MarketIdentity, failedAt time.Time, syncErr error) (MarketSyncState, error) {
	if err := validateMarketAndLimit(market, 1); err != nil {
		return MarketSyncState{}, err
	}
	if syncErr == nil {
		syncErr = errors.New("sync failed")
	}
	state, err := r.GetSyncState(ctx, market)
	if err != nil {
		return MarketSyncState{}, err
	}
	state.FailCount++
	state.Status = syncStatusFailed
	state.NextPollAt = nextSyncPollTime(failedAt.UTC(), state.FailCount)
	state.LastError = sanitizeErrorSummary(syncErr.Error())
	state.Market = MarketIdentity{ContractAddress: normalizeAddress(market.ContractAddress), GameID: market.GameID}

	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	if _, err := r.db.ExecContext(ctx, upsertSyncFailureSQL,
		state.Market.ContractAddress, state.Market.GameID, state.FailCount, state.NextPollAt.UTC(), state.LastError, syncStatusFailed,
	); err != nil {
		return MarketSyncState{}, fmt.Errorf("record market sync failure: %w", err)
	}
	return state, nil
}

type historyQueryer interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

func queryHistory(ctx context.Context, queryer historyQueryer, contract string, gameID, limit int) ([]HistoryObservation, error) {
	rows, err := queryer.QueryContext(ctx, selectHistorySQL, contract, gameID, limit)
	if err != nil {
		return nil, fmt.Errorf("query market history: %w", err)
	}
	defer rows.Close()
	points := make([]HistoryObservation, 0, limit)
	for rows.Next() {
		var point HistoryObservation
		var yesRaw, noRaw string
		var reserveNO, reserveYES []byte
		if err := rows.Scan(&point.Time, &yesRaw, &noRaw, &reserveNO, &reserveYES, &point.Source); err != nil {
			return nil, fmt.Errorf("scan market history: %w", err)
		}
		point.YesPercent, err = strconv.ParseFloat(yesRaw, 64)
		if err != nil {
			return nil, fmt.Errorf("parse yes_percent: %w", err)
		}
		point.NoPercent, err = strconv.ParseFloat(noRaw, 64)
		if err != nil {
			return nil, fmt.Errorf("parse no_percent: %w", err)
		}
		var parseErr error
		point.ReserveNO, parseErr = parseReserveBytes(reserveNO)
		if parseErr != nil {
			return nil, fmt.Errorf("parse reserve_no: %w", parseErr)
		}
		point.ReserveYES, parseErr = parseReserveBytes(reserveYES)
		if parseErr != nil {
			return nil, fmt.Errorf("parse reserve_yes: %w", parseErr)
		}
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate market history: %w", err)
	}
	for left, right := 0, len(points)-1; left < right; left, right = left+1, right-1 {
		points[left], points[right] = points[right], points[left]
	}
	return points, nil
}

func (r *MySQLRepository) RecordRule(ctx context.Context, record RuleDecisionRecord) error {
	err := r.recordRule(ctx, record)
	if err != nil && r.isTableNotFound(err) {
		slog.Warn("mysql repository: table missing, recreating and retrying", "op", "record_rule")
		r.ensureTables(ctx)
		err = r.recordRule(ctx, record)
	}
	return err
}

func (r *MySQLRepository) recordRule(ctx context.Context, record RuleDecisionRecord) error {
	if err := validateDecisionIdentity(record.Market, record.UserAddress); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, insertDecisionSQL,
		normalizeAddress(record.Market.ContractAddress), record.Market.GameID,
		normalizeAddress(record.UserAddress), record.ObservedAt,
		"rule", record.Action, "0.000000", record.Reason, record.HistoryPoints, record.Outcome,
	)
	if err != nil {
		return fmt.Errorf("record rule decision: %w", err)
	}
	return nil
}

func (r *MySQLRepository) CreatePending(ctx context.Context, record ModelDecisionRecord) (int64, error) {
	id, err := r.createPending(ctx, record)
	if err != nil && r.isTableNotFound(err) {
		slog.Warn("mysql repository: table missing, recreating and retrying", "op", "create_pending")
		r.ensureTables(ctx)
		id, err = r.createPending(ctx, record)
	}
	return id, err
}

func (r *MySQLRepository) createPending(ctx context.Context, record ModelDecisionRecord) (int64, error) {
	if err := validateDecisionIdentity(record.Market, record.UserAddress); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	result, err := r.db.ExecContext(ctx, insertDecisionSQL,
		normalizeAddress(record.Market.ContractAddress), record.Market.GameID,
		normalizeAddress(record.UserAddress), record.ObservedAt,
		"model", record.Action, formatPercentage(record.Confidence), record.Reason,
		record.HistoryPoints, "pending",
	)
	if err != nil {
		return 0, fmt.Errorf("create pending decision: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return 0, errors.New("create pending decision: missing insert id")
	}
	return id, nil
}

func (r *MySQLRepository) Finalize(ctx context.Context, id int64, outcome, txHash, errorSummary string) error {
	err := r.finalize(ctx, id, outcome, txHash, errorSummary)
	if err != nil && r.isTableNotFound(err) {
		slog.Warn("mysql repository: table missing, recreating and retrying", "op", "finalize")
		r.ensureTables(ctx)
		err = r.finalize(ctx, id, outcome, txHash, errorSummary)
	}
	return err
}

func (r *MySQLRepository) finalize(ctx context.Context, id int64, outcome, txHash, errorSummary string) error {
	if id <= 0 {
		return errors.New("decision id must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	result, err := r.db.ExecContext(ctx, finalizeDecisionSQL,
		outcome, strings.TrimSpace(txHash), sanitizeErrorSummary(errorSummary), id)
	if err != nil {
		return fmt.Errorf("finalize decision: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("finalize decision: expected one row, affected %d", affected)
	}
	return nil
}

func validateMarketAndLimit(market MarketIdentity, limit int) error {
	if !common.IsHexAddress(market.ContractAddress) {
		return errors.New("contract address is invalid")
	}
	if market.GameID <= 0 {
		return errors.New("game ID must be positive")
	}
	if limit < 1 || limit > 1000 {
		return errors.New("history limit must be between 1 and 1000")
	}
	return nil
}

func validateObservation(point HistoryObservation, source string) error {
	if point.Time <= 0 || point.Source != source {
		return errors.New("history time or source is invalid")
	}
	if math.IsNaN(point.YesPercent) || math.IsInf(point.YesPercent, 0) ||
		math.IsNaN(point.NoPercent) || math.IsInf(point.NoPercent, 0) ||
		point.YesPercent < 0 || point.YesPercent > 100 || point.NoPercent < 0 || point.NoPercent > 100 {
		return errors.New("history percentages are invalid")
	}
	if math.Abs(point.YesPercent+point.NoPercent-100) > 0.5 {
		return errors.New("history percentages must sum to 100")
	}
	if source == historySourceChain && (point.ReserveNO == nil || point.ReserveYES == nil) {
		return errors.New("chain reserves are required")
	}
	return nil
}

func validateDecisionIdentity(market MarketIdentity, user string) error {
	if err := validateMarketAndLimit(market, 1); err != nil {
		return err
	}
	if !common.IsHexAddress(user) {
		return errors.New("user address is invalid")
	}
	return nil
}

func validateManagedTrade(record ManagedTradeRecord) error {
	if err := validateDecisionIdentity(record.Market, record.UserAddress); err != nil {
		return err
	}
	if record.OptionID != 0 && record.OptionID != 1 {
		return errors.New("option_id must be 0 for YES or 1 for NO")
	}
	if strings.TrimSpace(record.TxHash) == "" {
		return errors.New("tx_hash is required")
	}
	if record.TimestampSec <= 0 {
		return errors.New("timestamp_sec must be positive")
	}
	if record.AmountWei == nil || record.AmountWei.Sign() <= 0 {
		return errors.New("amount_wei must be positive")
	}
	return nil
}

func reserveBytes(value *big.Int) ([]byte, error) {
	if value == nil || value.Sign() < 0 || value.BitLen() > 256 {
		return nil, errors.New("value is not uint256")
	}
	// Store as decimal string so the data is human-readable in the database.
	return []byte(value.String()), nil
}

func decimalString(value *big.Int) string {
	if value == nil {
		return "0"
	}
	return value.String()
}

func nonNilBigInt(value *big.Int) *big.Int {
	if value == nil {
		return big.NewInt(0)
	}
	return value
}

// parseReserveBytes decodes a reserve value from MySQL. It tries decimal
// string first (current format) and falls back to big-endian binary (for
// rows written before the decimal-string migration). Returns an error when
// the stored data is too large for uint256.
func parseReserveBytes(data []byte) (*big.Int, error) {
	if len(data) == 0 {
		return nil, nil
	}
	// Try decimal string format first.
	if v, ok := new(big.Int).SetString(string(data), 10); ok {
		if v.BitLen() > 256 {
			return nil, errors.New("reserve exceeds uint256")
		}
		return v, nil
	}
	// Fall back to legacy big-endian binary encoding.
	if len(data) > 32 {
		return nil, errors.New("reserve exceeds uint256")
	}
	return new(big.Int).SetBytes(data), nil
}

func formatPercentage(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func nullableTime(value time.Time) interface{} {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func normalizeAddress(value string) string {
	return strings.ToLower(common.HexToAddress(value).Hex())
}

func sanitizeErrorSummary(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 512 {
		return value
	}
	cut := 512
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut]
}
