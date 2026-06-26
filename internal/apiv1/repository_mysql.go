package apiv1

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"PredictionMarket/internal/database"
)

const repoTimeout = 10 * time.Second

// SQL constants.
const (
	// gold_games
	selectAllGamesSQL = `SELECT game_id, contract_address, ipfs_cid, ` + "`desc`" + `, ` + "`condition`" + `,
		avatar_url, detailed_info, option_yes, option_no, creator_address,
		created_at, updated_at
		FROM gold_games ORDER BY game_id`
	selectGameByIDSQL = `SELECT game_id, contract_address, ipfs_cid, ` + "`desc`" + `, ` + "`condition`" + `,
		avatar_url, detailed_info, option_yes, option_no, creator_address,
		created_at, updated_at
		FROM gold_games WHERE game_id = ?`
	upsertGameSQL = `INSERT INTO gold_games
		(game_id, contract_address, ipfs_cid, ` + "`desc`" + `, ` + "`condition`" + `,
		avatar_url, detailed_info, option_yes, option_no, creator_address)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		contract_address=VALUES(contract_address), ipfs_cid=VALUES(ipfs_cid),
		` + "`desc`" + `=VALUES(` + "`desc`" + `), ` + "`condition`" + `=VALUES(` + "`condition`" + `),
		avatar_url=VALUES(avatar_url), detailed_info=VALUES(detailed_info),
		option_yes=VALUES(option_yes), option_no=VALUES(option_no),
		creator_address=VALUES(creator_address)`
	insertGameIgnoreSQL = `INSERT IGNORE INTO gold_games
		(game_id, contract_address, ipfs_cid, ` + "`desc`" + `, ` + "`condition`" + `,
		avatar_url, detailed_info, option_yes, option_no, creator_address)
		VALUES (?, ?, ?, '', '', '', '', 'YES', 'NO', '')`

	// gold_chain_states
	selectChainStateSQL = `SELECT game_id, total_pool, is_resolved, is_refunded,
		winning_option, deadline_sec, reserve_yes, reserve_no, updated_at
		FROM gold_chain_states WHERE game_id = ?`
	selectAllChainStatesSQL = `SELECT game_id, total_pool, is_resolved, is_refunded,
		winning_option, deadline_sec, reserve_yes, reserve_no, updated_at
		FROM gold_chain_states ORDER BY game_id`
	upsertChainStateSQL = `INSERT INTO gold_chain_states
		(game_id, total_pool, is_resolved, is_refunded, winning_option, deadline_sec,
		reserve_yes, reserve_no)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		total_pool=VALUES(total_pool), is_resolved=VALUES(is_resolved),
		is_refunded=VALUES(is_refunded), winning_option=VALUES(winning_option),
		deadline_sec=VALUES(deadline_sec), reserve_yes=VALUES(reserve_yes),
		reserve_no=VALUES(reserve_no)`

	// gold_user_positions
	selectUserPositionSQL = `SELECT user_address, game_id, my_shares_yes, my_shares_no, updated_at
		FROM gold_user_positions WHERE user_address = ? AND game_id = ?`
	selectAllUserPositionsSQL = `SELECT user_address, game_id, my_shares_yes, my_shares_no, updated_at
		FROM gold_user_positions WHERE user_address = ? ORDER BY game_id`
	upsertUserPositionSQL = `INSERT INTO gold_user_positions
		(user_address, game_id, my_shares_yes, my_shares_no)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		my_shares_yes=VALUES(my_shares_yes), my_shares_no=VALUES(my_shares_no)`

	// gold_price_history
	selectPriceHistorySQL = `SELECT id, game_id, timestamp_sec, yes_price, no_price, total_pool
		FROM gold_price_history WHERE game_id = ? ORDER BY timestamp_sec DESC LIMIT ?`
	insertPriceHistorySQL = `INSERT INTO gold_price_history
		(game_id, timestamp_sec, yes_price, no_price, total_pool)
		VALUES (?, ?, ?, ?, ?)`

	// gold_trades
	insertTradeSQL = `INSERT INTO gold_trades
		(game_id, contract_address, user_address, trade_type, option_id, amount_wei,
		tx_hash, is_success)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
)

// MySQLRepository implements all five v1 repository interfaces on a single
// *sql.DB connection pool. Follows the same auto-recovery pattern as
// aimanaged.MySQLRepository: catches MySQL error 1146 (table not found) and
// error 1054 (unknown column, i.e. stale schema), recreates tables, and
// retries once.
type MySQLRepository struct {
	db       *sql.DB
	ensureMu sync.Mutex
}

// NewMySQLRepository creates a repository backed by db.
func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

// ensureTables recreates all required tables (CREATE TABLE IF NOT EXISTS).
// Serialises concurrent callers so only one goroutine runs the DDL.
func (r *MySQLRepository) ensureTables(ctx context.Context) {
	r.ensureMu.Lock()
	defer r.ensureMu.Unlock()
	if err := database.EnsureTables(ctx, r.db); err != nil {
		slog.Error("apiv1 mysql repository: ensure tables failed", "error", err)
	}
}

// recoverTable drops a single table and recreates it via EnsureTables.
// Used when the table exists but has a stale schema (error 1054).
func (r *MySQLRepository) recoverTable(ctx context.Context, tableName string) {
	r.ensureMu.Lock()
	defer r.ensureMu.Unlock()
	slog.Warn("apiv1: recovering stale table schema", "table", tableName)
	if _, err := r.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+tableName); err != nil {
		slog.Error("apiv1: drop stale table failed", "table", tableName, "error", err)
	}
	if err := database.EnsureTables(ctx, r.db); err != nil {
		slog.Error("apiv1: recreate table failed", "table", tableName, "error", err)
	}
}

func (r *MySQLRepository) isTableNotFound(err error) bool {
	return database.IsTableNotFound(err)
}

// isStaleSchema reports whether err is a MySQL "Unknown column" error (1054),
// which indicates the table exists but with an outdated column layout.
func (r *MySQLRepository) isStaleSchema(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Error 1054")
}

// retryAfterRecover attempts to fix a broken table and retries the operation.
// tableName identifies the specific table that needs recovery (used for DROP
// on 1054) or "" to recreate all tables (for 1146).
func (r *MySQLRepository) retryAfterRecover(err error, tableName string) bool {
	if r.isTableNotFound(err) {
		slog.Warn("apiv1: table missing, recreating all tables")
		r.ensureMu.Lock()
		database.EnsureTables(context.Background(), r.db)
		r.ensureMu.Unlock()
		return true
	}
	if r.isStaleSchema(err) && tableName != "" {
		slog.Warn("apiv1: stale schema, recovering table", "table", tableName)
		r.recoverTable(context.Background(), tableName)
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// GameMetadataRepository
// ---------------------------------------------------------------------------

func (r *MySQLRepository) ListAllGames(ctx context.Context) ([]GameMetaDTO, error) {
	games, err := r.listAllGames(ctx)
	if err != nil && r.retryAfterRecover(err, "gold_games") {
		games, err = r.listAllGames(ctx)
	}
	return games, err
}

func (r *MySQLRepository) listAllGames(ctx context.Context) ([]GameMetaDTO, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, selectAllGamesSQL)
	if err != nil {
		return nil, fmt.Errorf("list all games: %w", err)
	}
	defer rows.Close()
	return scanGames(rows)
}

func (r *MySQLRepository) GetGameByID(ctx context.Context, gameID int) (*GameMetaDTO, error) {
	game, err := r.getGameByID(ctx, gameID)
	if err != nil && r.retryAfterRecover(err, "gold_games") {
		game, err = r.getGameByID(ctx, gameID)
	}
	return game, err
}

func (r *MySQLRepository) getGameByID(ctx context.Context, gameID int) (*GameMetaDTO, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	row := r.db.QueryRowContext(ctx, selectGameByIDSQL, gameID)
	var g GameMetaDTO
	var desc, cond, avatar, detail, optYes, optNo, creator sql.NullString
	if err := row.Scan(
		&g.GameID, &g.ContractAddress, &g.IPFSCID,
		&desc, &cond, &avatar, &detail,
		&optYes, &optNo, &creator,
		&g.CreatedAt, &g.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get game by id %d: %w", gameID, err)
	}
	g.Desc = desc.String
	g.Condition = cond.String
	g.AvatarURL = avatar.String
	g.DetailedInfo = detail.String
	g.OptionYes = optYes.String
	g.OptionNo = optNo.String
	g.CreatorAddress = creator.String
	return &g, nil
}

// InsertGameStub creates a minimal gold_games row (INSERT IGNORE) so that
// the sampler can write chain_state and price_history without FK violations.
// It never overwrites rows already created by the DApp's POST /games/sync.
func (r *MySQLRepository) InsertGameStub(ctx context.Context, gameID int, contractAddress, ipfsCID string) error {
	err := r.insertGameStub(ctx, gameID, contractAddress, ipfsCID)
	if err != nil && r.retryAfterRecover(err, "gold_games") {
		err = r.insertGameStub(ctx, gameID, contractAddress, ipfsCID)
	}
	return err
}

func (r *MySQLRepository) insertGameStub(ctx context.Context, gameID int, contractAddress, ipfsCID string) error {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, insertGameIgnoreSQL,
		gameID,
		normalizeAddress(contractAddress),
		ipfsCID,
	)
	if err != nil {
		return fmt.Errorf("insert game stub %d: %w", gameID, err)
	}
	return nil
}

func (r *MySQLRepository) UpsertGame(ctx context.Context, game *gameRow) (int, error) {
	id, err := r.upsertGame(ctx, game)
	if err != nil && r.retryAfterRecover(err, "gold_games") {
		id, err = r.upsertGame(ctx, game)
	}
	return id, err
}

func (r *MySQLRepository) upsertGame(ctx context.Context, game *gameRow) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, upsertGameSQL,
		game.GameID,
		normalizeAddress(game.ContractAddress),
		game.IPFSCID,
		game.Desc, game.Condition,
		game.AvatarURL, game.DetailedInfo,
		game.OptionYes, game.OptionNo,
		normalizeAddress(game.CreatorAddress),
	)
	if err != nil {
		return 0, fmt.Errorf("upsert game %d: %w", game.GameID, err)
	}
	return game.GameID, nil
}

// ---------------------------------------------------------------------------
// ChainStateRepository
// ---------------------------------------------------------------------------

func (r *MySQLRepository) GetChainState(ctx context.Context, gameID int) (*chainStateRow, error) {
	state, err := r.getChainState(ctx, gameID)
	if err != nil && r.retryAfterRecover(err, "gold_chain_states") {
		state, err = r.getChainState(ctx, gameID)
	}
	return state, err
}

func (r *MySQLRepository) getChainState(ctx context.Context, gameID int) (*chainStateRow, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	row := r.db.QueryRowContext(ctx, selectChainStateSQL, gameID)
	var s chainStateRow
	var totalPool, reserveYes, reserveNo []byte
	var updatedAt sql.NullTime
	if err := row.Scan(
		&s.GameID, &totalPool, &s.IsResolved, &s.IsRefunded,
		&s.WinningOption, &s.DeadlineSec,
		&reserveYes, &reserveNo, &updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get chain state %d: %w", gameID, err)
	}
	s.TotalPool, _ = parseBigIntFromDB(totalPool)
	s.ReserveYes, _ = parseBigIntFromDB(reserveYes)
	s.ReserveNo, _ = parseBigIntFromDB(reserveNo)
	if updatedAt.Valid {
		s.UpdatedAt = updatedAt.Time.UTC().Format(time.RFC3339)
	}
	return &s, nil
}

func (r *MySQLRepository) ListAllChainStates(ctx context.Context) ([]chainStateRow, error) {
	states, err := r.listAllChainStates(ctx)
	if err != nil && r.retryAfterRecover(err, "gold_chain_states") {
		states, err = r.listAllChainStates(ctx)
	}
	return states, err
}

func (r *MySQLRepository) listAllChainStates(ctx context.Context) ([]chainStateRow, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, selectAllChainStatesSQL)
	if err != nil {
		return nil, fmt.Errorf("list all chain states: %w", err)
	}
	defer rows.Close()
	var out []chainStateRow
	for rows.Next() {
		var s chainStateRow
		var totalPool, reserveYes, reserveNo []byte
		var updatedAt sql.NullTime
		if err := rows.Scan(
			&s.GameID, &totalPool, &s.IsResolved, &s.IsRefunded,
			&s.WinningOption, &s.DeadlineSec,
			&reserveYes, &reserveNo, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan chain state: %w", err)
		}
		s.TotalPool, _ = parseBigIntFromDB(totalPool)
		s.ReserveYes, _ = parseBigIntFromDB(reserveYes)
		s.ReserveNo, _ = parseBigIntFromDB(reserveNo)
		if updatedAt.Valid {
			s.UpdatedAt = updatedAt.Time.UTC().Format(time.RFC3339)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *MySQLRepository) UpsertChainState(ctx context.Context, state *chainStateRow) error {
	err := r.upsertChainState(ctx, state)
	if err != nil && r.retryAfterRecover(err, "gold_chain_states") {
		err = r.upsertChainState(ctx, state)
	}
	return err
}

func (r *MySQLRepository) upsertChainState(ctx context.Context, state *chainStateRow) error {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, upsertChainStateSQL,
		state.GameID,
		bigIntToDBBytes(state.TotalPool),
		state.IsResolved, state.IsRefunded,
		state.WinningOption, state.DeadlineSec,
		bigIntToDBBytes(state.ReserveYes),
		bigIntToDBBytes(state.ReserveNo),
	)
	if err != nil {
		return fmt.Errorf("upsert chain state %d: %w", state.GameID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// UserPositionRepository
// ---------------------------------------------------------------------------

func (r *MySQLRepository) GetUserPosition(ctx context.Context, userAddress string, gameID int) (*userPositionRow, error) {
	pos, err := r.getUserPosition(ctx, userAddress, gameID)
	if err != nil && r.retryAfterRecover(err, "gold_user_positions") {
		pos, err = r.getUserPosition(ctx, userAddress, gameID)
	}
	return pos, err
}

func (r *MySQLRepository) getUserPosition(ctx context.Context, userAddress string, gameID int) (*userPositionRow, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	row := r.db.QueryRowContext(ctx, selectUserPositionSQL,
		normalizeAddress(userAddress), gameID)
	var p userPositionRow
	var sharesYes, sharesNo []byte
	var updatedAt sql.NullTime
	if err := row.Scan(&p.UserAddress, &p.GameID, &sharesYes, &sharesNo, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user position %s/%d: %w", userAddress, gameID, err)
	}
	p.MySharesYes, _ = parseBigIntFromDB(sharesYes)
	p.MySharesNo, _ = parseBigIntFromDB(sharesNo)
	if updatedAt.Valid {
		p.UpdatedAt = updatedAt.Time.UTC().Format(time.RFC3339)
	}
	return &p, nil
}

func (r *MySQLRepository) ListUserPositions(ctx context.Context, userAddress string) ([]userPositionRow, error) {
	positions, err := r.listUserPositions(ctx, userAddress)
	if err != nil && r.retryAfterRecover(err, "gold_user_positions") {
		positions, err = r.listUserPositions(ctx, userAddress)
	}
	return positions, err
}

func (r *MySQLRepository) listUserPositions(ctx context.Context, userAddress string) ([]userPositionRow, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, selectAllUserPositionsSQL, normalizeAddress(userAddress))
	if err != nil {
		return nil, fmt.Errorf("list user positions %s: %w", userAddress, err)
	}
	defer rows.Close()
	var out []userPositionRow
	for rows.Next() {
		var p userPositionRow
		var sharesYes, sharesNo []byte
		var updatedAt sql.NullTime
		if err := rows.Scan(&p.UserAddress, &p.GameID, &sharesYes, &sharesNo, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan user position: %w", err)
		}
		p.MySharesYes, _ = parseBigIntFromDB(sharesYes)
		p.MySharesNo, _ = parseBigIntFromDB(sharesNo)
		if updatedAt.Valid {
			p.UpdatedAt = updatedAt.Time.UTC().Format(time.RFC3339)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *MySQLRepository) UpsertUserPosition(ctx context.Context, pos *userPositionRow) error {
	err := r.upsertUserPosition(ctx, pos)
	if err != nil && r.retryAfterRecover(err, "gold_user_positions") {
		err = r.upsertUserPosition(ctx, pos)
	}
	return err
}

func (r *MySQLRepository) upsertUserPosition(ctx context.Context, pos *userPositionRow) error {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, upsertUserPositionSQL,
		normalizeAddress(pos.UserAddress),
		pos.GameID,
		bigIntToDBBytes(pos.MySharesYes),
		bigIntToDBBytes(pos.MySharesNo),
	)
	if err != nil {
		return fmt.Errorf("upsert user position %s/%d: %w", pos.UserAddress, pos.GameID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// PriceHistoryRepository
// ---------------------------------------------------------------------------

func (r *MySQLRepository) ListHistory(ctx context.Context, gameID int, limit int) ([]PricePointDTO, error) {
	points, err := r.listHistory(ctx, gameID, limit)
	if err != nil && r.retryAfterRecover(err, "gold_price_history") {
		points, err = r.listHistory(ctx, gameID, limit)
	}
	return points, err
}

func (r *MySQLRepository) listHistory(ctx context.Context, gameID int, limit int) ([]PricePointDTO, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, selectPriceHistorySQL, gameID, limit)
	if err != nil {
		return nil, fmt.Errorf("list price history %d: %w", gameID, err)
	}
	defer rows.Close()
	var out []PricePointDTO
	for rows.Next() {
		var p PricePointDTO
		var id int64
		var totalPool []byte
		if err := rows.Scan(&id, &p.GameID, &p.TimestampSec, &p.YesPrice, &p.NoPrice, &totalPool); err != nil {
			return nil, fmt.Errorf("scan price history: %w", err)
		}
		v, _ := parseBigIntFromDB(totalPool)
		p.TotalPool = bigIntOrZero(v)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate price history: %w", err)
	}
	// Reverse to ascending order (query returned DESC).
	for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	return out, nil
}

func (r *MySQLRepository) AppendHistory(ctx context.Context, point *priceHistoryRow) error {
	err := r.appendHistory(ctx, point)
	if err != nil && r.retryAfterRecover(err, "gold_price_history") {
		err = r.appendHistory(ctx, point)
	}
	return err
}

func (r *MySQLRepository) appendHistory(ctx context.Context, point *priceHistoryRow) error {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, insertPriceHistorySQL,
		point.GameID, point.TimestampSec,
		point.YesPrice, point.NoPrice,
		bigIntToDBBytes(point.TotalPool),
	)
	if err != nil {
		return fmt.Errorf("append price history %d: %w", point.GameID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// TradeRepository
// ---------------------------------------------------------------------------

func (r *MySQLRepository) RecordTrade(ctx context.Context, trade *tradeRow) error {
	err := r.recordTrade(ctx, trade)
	if err != nil && r.retryAfterRecover(err, "gold_trades") {
		err = r.recordTrade(ctx, trade)
	}
	return err
}

func (r *MySQLRepository) recordTrade(ctx context.Context, trade *tradeRow) error {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, insertTradeSQL,
		trade.GameID,
		normalizeAddress(trade.ContractAddress),
		normalizeAddress(trade.UserAddress),
		trade.TradeType,
		trade.OptionID,
		bigIntToDBBytes(trade.AmountWei),
		trade.TxHash,
		trade.IsSuccess,
	)
	if err != nil {
		return fmt.Errorf("record trade %d: %w", trade.GameID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func scanGames(rows *sql.Rows) ([]GameMetaDTO, error) {
	var out []GameMetaDTO
	for rows.Next() {
		var g GameMetaDTO
		var desc, cond, avatar, detail, optYes, optNo, creator sql.NullString
		if err := rows.Scan(
			&g.GameID, &g.ContractAddress, &g.IPFSCID,
			&desc, &cond, &avatar, &detail,
			&optYes, &optNo, &creator,
			&g.CreatedAt, &g.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan game row: %w", err)
		}
		g.Desc = desc.String
		g.Condition = cond.String
		g.AvatarURL = avatar.String
		g.DetailedInfo = detail.String
		g.OptionYes = optYes.String
		g.OptionNo = optNo.String
		g.CreatorAddress = creator.String
		out = append(out, g)
	}
	return out, rows.Err()
}
