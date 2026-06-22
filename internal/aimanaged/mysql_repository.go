package aimanaged

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
)

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) MergeAndList(ctx context.Context, market MarketIdentity, seed []HistoryObservation, current HistoryObservation, limit int) ([]HistoryObservation, error) {
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
	if err := validateMarketAndLimit(market, limit); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryOperationTimeout)
	defer cancel()
	return queryHistory(ctx, r.db, normalizeAddress(market.ContractAddress), market.GameID, limit)
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
		if len(reserveNO) > 32 {
			return nil, errors.New("parse reserve_no: exceeds uint256")
		}
		if len(reserveYES) > 32 {
			return nil, errors.New("parse reserve_yes: exceeds uint256")
		}
		if reserveNO != nil {
			point.ReserveNO = new(big.Int).SetBytes(reserveNO)
		}
		if reserveYES != nil {
			point.ReserveYES = new(big.Int).SetBytes(reserveYES)
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

func reserveBytes(value *big.Int) ([]byte, error) {
	if value == nil || value.Sign() < 0 || value.BitLen() > 256 {
		return nil, errors.New("value is not uint256")
	}
	return value.Bytes(), nil
}

func formatPercentage(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
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
