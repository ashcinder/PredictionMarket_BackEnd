package gameapi

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const repoTimeout = 10 * time.Second

// GameRow mirrors the gold_games table, with optional user-position fields
// set by some repository methods.
type GameRow struct {
	ID              int64
	ContractAddress string
	IPFSCID         string
	TotalPool       string
	IsResolved      bool
	IsRefunded      bool
	WinningOption   int
	DeadlineSec     int64
	ReserveYes      string
	ReserveNo       string
	SharesYes       string // populated by ListAll / GetByID / GetPositions
	SharesNo        string
}

// PositionRow mirrors the gold_user_positions table.
type PositionRow struct {
	GameID      int64
	UserAddress string
	SharesYes   string
	SharesNo    string
}

// GameRepository provides read/write access to gold_games and
// gold_user_positions.
type GameRepository struct {
	db *sql.DB
}

func NewGameRepository(db *sql.DB) *GameRepository {
	return &GameRepository{db: db}
}

func normalise(addr string) string {
	return strings.ToLower(common.HexToAddress(addr).Hex())
}

// UpsertGame inserts or updates a row in gold_games.
func (r *GameRepository) UpsertGame(ctx context.Context, g GameRow) error {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO gold_games
			(id, contract_address, ipfs_cid, total_pool, is_resolved, is_refunded,
			 winning_option, deadline_sec, reserve_yes, reserve_no)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			ipfs_cid        = VALUES(ipfs_cid),
			total_pool      = VALUES(total_pool),
			is_resolved     = VALUES(is_resolved),
			is_refunded     = VALUES(is_refunded),
			winning_option  = VALUES(winning_option),
			deadline_sec    = VALUES(deadline_sec),
			reserve_yes     = VALUES(reserve_yes),
			reserve_no      = VALUES(reserve_no)`,
		g.ID, normalise(g.ContractAddress), g.IPFSCID, g.TotalPool,
		g.IsResolved, g.IsRefunded, g.WinningOption, g.DeadlineSec,
		g.ReserveYes, g.ReserveNo,
	)
	return err
}

// UpsertPosition inserts or updates user position.
func (r *GameRepository) UpsertPosition(ctx context.Context, p PositionRow) error {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO gold_user_positions (game_id, user_address, shares_yes, shares_no)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			shares_yes = VALUES(shares_yes),
			shares_no  = VALUES(shares_no)`,
		p.GameID, normalise(p.UserAddress), p.SharesYes, p.SharesNo,
	)
	return err
}

// ListAll returns every active game (not resolved, not refunded, deadline
// not passed) from gold_games, joined with the user's position.
func (r *GameRepository) ListAll(ctx context.Context, userAddress string) ([]GameRow, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	now := time.Now().Unix()
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			g.id, g.contract_address, g.ipfs_cid, g.total_pool,
			g.is_resolved, g.is_refunded, g.winning_option, g.deadline_sec,
			g.reserve_yes, g.reserve_no,
			COALESCE(p.shares_yes, '0') AS shares_yes,
			COALESCE(p.shares_no,  '0') AS shares_no
		FROM gold_games g
		LEFT JOIN gold_user_positions p
			ON p.game_id = g.id AND p.user_address = ?
		WHERE g.is_resolved = 0 AND g.is_refunded = 0 AND g.deadline_sec > ?
		ORDER BY g.id ASC`, normalise(userAddress), now)
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	defer rows.Close()
	var out []GameRow
	for rows.Next() {
		var g GameRow
		if err := rows.Scan(
			&g.ID, &g.ContractAddress, &g.IPFSCID, &g.TotalPool,
			&g.IsResolved, &g.IsRefunded, &g.WinningOption, &g.DeadlineSec,
			&g.ReserveYes, &g.ReserveNo,
			&g.SharesYes, &g.SharesNo,
		); err != nil {
			return nil, fmt.Errorf("scan game: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetByID returns a single game with user position.

// GetByID returns a single game with user position.
func (r *GameRepository) GetByID(ctx context.Context, gameID int64, userAddress string) (*GameRow, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	var g GameRow
	err := r.db.QueryRowContext(ctx, `
		SELECT
			g.id, g.contract_address, g.ipfs_cid, g.total_pool,
			g.is_resolved, g.is_refunded, g.winning_option, g.deadline_sec,
			g.reserve_yes, g.reserve_no,
			COALESCE(p.shares_yes, '0') AS shares_yes,
			COALESCE(p.shares_no,  '0') AS shares_no
		FROM gold_games g
		LEFT JOIN gold_user_positions p
			ON p.game_id = g.id AND p.user_address = ?
		WHERE g.id = ?`, normalise(userAddress), gameID).
		Scan(
			&g.ID, &g.ContractAddress, &g.IPFSCID, &g.TotalPool,
			&g.IsResolved, &g.IsRefunded, &g.WinningOption, &g.DeadlineSec,
			&g.ReserveYes, &g.ReserveNo,
			&g.SharesYes, &g.SharesNo,
		)
	if err == sql.ErrNoRows {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("get game %d: %w", gameID, err)
	}
	return &g, nil
}

// GetPositions returns all games where the user has non-zero shares.
func (r *GameRepository) GetPositions(ctx context.Context, userAddress string) ([]GameRow, error) {
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			g.id, g.contract_address, g.ipfs_cid, g.total_pool,
			g.is_resolved, g.is_refunded, g.winning_option, g.deadline_sec,
			g.reserve_yes, g.reserve_no,
			COALESCE(p.shares_yes, '0') AS shares_yes,
			COALESCE(p.shares_no,  '0') AS shares_no
		FROM gold_games g
		INNER JOIN gold_user_positions p
			ON p.game_id = g.id AND p.user_address = ?
		WHERE p.shares_yes > 0 OR p.shares_no > 0
		ORDER BY g.id ASC`, normalise(userAddress))
	if err != nil {
		return nil, fmt.Errorf("list positions: %w", err)
	}
	defer rows.Close()
	var out []GameRow
	for rows.Next() {
		var g GameRow
		if err := rows.Scan(
			&g.ID, &g.ContractAddress, &g.IPFSCID, &g.TotalPool,
			&g.IsResolved, &g.IsRefunded, &g.WinningOption, &g.DeadlineSec,
			&g.ReserveYes, &g.ReserveNo,
			&g.SharesYes, &g.SharesNo,
		); err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
