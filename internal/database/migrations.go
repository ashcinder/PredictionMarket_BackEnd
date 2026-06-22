package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	migrationLockName           = "prediction_market_schema_migrations"
	migrationLockTimeoutSeconds = 10
	migrationStatementSeparator = "-- migration:split"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	Version int64
	SQL     string
}

func RunMigrations(ctx context.Context, db *sql.DB) error {
	migrations, err := embeddedMigrations()
	if err != nil {
		return err
	}
	return runMigrationSet(ctx, db, migrations)
}

func embeddedMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	result := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		data, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		result = append(result, migration{Version: version, SQL: string(data)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	for i := 1; i < len(result); i++ {
		if result[i-1].Version == result[i].Version {
			return nil, fmt.Errorf("duplicate migration version %d", result[i].Version)
		}
	}
	return result, nil
}

func runMigrationSet(ctx context.Context, db *sql.DB, migrations []migration) (err error) {
	var locked sql.NullInt64
	if scanErr := db.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", migrationLockName, migrationLockTimeoutSeconds).Scan(&locked); scanErr != nil {
		return fmt.Errorf("acquire migration lock: %w", scanErr)
	}
	if !locked.Valid || locked.Int64 != 1 {
		return fmt.Errorf("acquire migration lock: unavailable")
	}
	defer func() {
		var released sql.NullInt64
		releaseErr := db.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", migrationLockName).Scan(&released)
		if err == nil && (releaseErr != nil || !released.Valid || released.Int64 != 1) {
			if releaseErr != nil {
				err = fmt.Errorf("release migration lock: %w", releaseErr)
			} else {
				err = fmt.Errorf("release migration lock: unavailable")
			}
		}
	}()

	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
version BIGINT NOT NULL PRIMARY KEY,
applied_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	applied := make(map[int64]struct{})
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close applied migrations: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate applied migrations: %w", err)
	}

	for _, item := range migrations {
		if _, ok := applied[item.Version]; ok {
			continue
		}
		for index, statement := range splitMigrationStatements(item.SQL) {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("migration %d statement %d: %w", item.Version, index+1, err)
			}
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", item.Version); err != nil {
			return fmt.Errorf("record migration %d: %w", item.Version, err)
		}
	}
	return nil
}

func splitMigrationStatements(raw string) []string {
	parts := strings.Split(raw, migrationStatementSeparator)
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		if statement := strings.TrimSpace(part); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}
