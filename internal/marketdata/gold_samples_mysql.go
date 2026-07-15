package marketdata

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	appendGoldSampleSQL = `INSERT INTO oracle_price_samples
		(symbol, observed_at, price_usd, source)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE price_usd = VALUES(price_usd), source = VALUES(source)`
	listGoldSamplesSQL = `SELECT symbol, observed_at, price_usd, source
		FROM oracle_price_samples
		WHERE symbol = ? AND observed_at BETWEEN ? AND ?
		ORDER BY observed_at ASC`
)

type MySQLGoldSampleRepository struct {
	db *sql.DB
}

func NewMySQLGoldSampleRepository(db *sql.DB) *MySQLGoldSampleRepository {
	return &MySQLGoldSampleRepository{db: db}
}

func (r *MySQLGoldSampleRepository) Append(ctx context.Context, sample GoldSample) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("oracle price sample database is not configured")
	}
	symbol := strings.ToUpper(strings.TrimSpace(sample.Symbol))
	if symbol == "" || sample.ObservedAt.IsZero() || sample.Price <= 0 {
		return fmt.Errorf("oracle price sample is invalid")
	}
	_, err := r.db.ExecContext(ctx, appendGoldSampleSQL,
		symbol, sample.ObservedAt.Unix(), sample.Price, strings.TrimSpace(sample.Source))
	if err != nil {
		return fmt.Errorf("append oracle price sample: %w", err)
	}
	return nil
}

func (r *MySQLGoldSampleRepository) Range(ctx context.Context, symbol string, start, end time.Time) ([]GoldSample, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("oracle price sample database is not configured")
	}
	rows, err := r.db.QueryContext(ctx, listGoldSamplesSQL,
		strings.ToUpper(strings.TrimSpace(symbol)), start.Unix(), end.Unix())
	if err != nil {
		return nil, fmt.Errorf("list oracle price samples: %w", err)
	}
	defer rows.Close()
	var samples []GoldSample
	for rows.Next() {
		var sample GoldSample
		var observedAt int64
		if err := rows.Scan(&sample.Symbol, &observedAt, &sample.Price, &sample.Source); err != nil {
			return nil, fmt.Errorf("scan oracle price sample: %w", err)
		}
		sample.ObservedAt = time.Unix(observedAt, 0).UTC()
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oracle price samples: %w", err)
	}
	return samples, nil
}
