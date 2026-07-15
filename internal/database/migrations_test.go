package database

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	mysql "github.com/go-sql-driver/mysql"
)

func TestSplitMigrationStatements(t *testing.T) {
	got := splitMigrationStatements(`
CREATE TABLE one (id INT);
-- migration:split

-- a useful comment
CREATE TABLE two (id INT);
-- migration:split

`)
	if len(got) != 2 {
		t.Fatalf("got %d statements: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "CREATE TABLE one") || !strings.Contains(got[1], "CREATE TABLE two") {
		t.Fatalf("unexpected statements: %#v", got)
	}
}

func TestRunMigrationSetAppliesPendingVersionAndReleasesLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, ?)")).
		WithArgs(migrationLockName, migrationLockTimeoutSeconds).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version FROM schema_migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}))
	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE one (id INT)")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE two (id INT)")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations").
		WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT RELEASE_LOCK(?)")).
		WithArgs(migrationLockName).
		WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))

	err = runMigrationSet(context.Background(), db, []migration{{
		Version: 1,
		SQL:     "CREATE TABLE one (id INT)\n-- migration:split\nCREATE TABLE two (id INT)",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRunMigrationSetStopsAfterStatementFailureAndReleasesLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, ?)")).
		WithArgs(migrationLockName, migrationLockTimeoutSeconds).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version FROM schema_migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}))
	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE broken (id INT)")).
		WillReturnError(errors.New("ddl failed"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT RELEASE_LOCK(?)")).
		WithArgs(migrationLockName).
		WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))

	err = runMigrationSet(context.Background(), db, []migration{{Version: 1, SQL: "CREATE TABLE broken (id INT)"}})
	if err == nil || !strings.Contains(err.Error(), "migration 1 statement 1") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddedMigrationDefinesPersistenceTables(t *testing.T) {
	migrations, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 1 || migrations[0].Version != 1 {
		t.Fatalf("unexpected migrations: %+v", migrations)
	}
	for _, table := range []string{"market_history", "ai_decisions"} {
		if !strings.Contains(migrations[0].SQL, table) {
			t.Fatalf("migration does not define %s", table)
		}
	}
	var foundSyncState, foundManagedEntries, foundIdempotentPriceHistory, foundProbabilityOrientationFix, foundOracleSamples, foundChainlinkRounds bool
	for _, migration := range migrations {
		if strings.Contains(migration.SQL, "market_sync_state") &&
			strings.Contains(migration.SQL, "sync_failed") {
			foundSyncState = true
		}
		if strings.Contains(migration.SQL, "ai_managed_entries") {
			foundManagedEntries = true
		}
		if strings.Contains(migration.SQL, "uq_gold_price_history_game_time") {
			foundIdempotentPriceHistory = true
		}
		if strings.Contains(migration.SQL, "100 - yes_percent") &&
			strings.Contains(migration.SQL, "100 - yes_price") {
			foundProbabilityOrientationFix = true
		}
		if strings.Contains(migration.SQL, "oracle_price_samples") &&
			strings.Contains(migration.SQL, "observed_at") {
			foundOracleSamples = true
		}
		if strings.Contains(migration.SQL, "oracle_chainlink_rounds") &&
			strings.Contains(migration.SQL, "round_id") &&
			strings.Contains(migration.SQL, "updated_at_sec") {
			foundChainlinkRounds = true
		}
	}
	if !foundSyncState {
		t.Fatalf("migrations do not define sync state and sync outcomes: %+v", migrations)
	}
	if !foundManagedEntries {
		t.Fatalf("migrations do not define ai-managed entries: %+v", migrations)
	}
	if !foundIdempotentPriceHistory {
		t.Fatalf("migrations do not make gold price history idempotent: %+v", migrations)
	}
	if !foundProbabilityOrientationFix {
		t.Fatalf("migrations do not repair historical probability orientation: %+v", migrations)
	}
	if !foundOracleSamples {
		t.Fatalf("migrations do not persist oracle price samples: %+v", migrations)
	}
	if !foundChainlinkRounds {
		t.Fatalf("migrations do not persist Chainlink rounds: %+v", migrations)
	}
}

func TestChainlinkRawAnswerUsesMySQLCompatibleStringColumn(t *testing.T) {
	migrations, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var migrationSQL string
	for _, item := range migrations {
		if item.Version == 17 {
			migrationSQL = item.SQL
			break
		}
	}
	if migrationSQL == "" {
		t.Fatal("migration 17 is missing")
	}
	for label, ddl := range map[string]string{
		"migration 17": migrationSQL,
		"EnsureTables": strings.Join(ensureTableDDLs, "\n"),
	} {
		if !strings.Contains(ddl, "answer_raw VARCHAR(80) NOT NULL") {
			t.Fatalf("%s must store the signed int256 answer as VARCHAR(80)", label)
		}
		if strings.Contains(ddl, "answer_raw DECIMAL(78,0)") {
			t.Fatalf("%s uses unsupported MySQL DECIMAL(78,0)", label)
		}
	}
}

func TestGoldChainStateContractAddressIsAddedOnlyByMigrationNine(t *testing.T) {
	migrations, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}

	var migrationThree, migrationNine string
	for _, item := range migrations {
		switch item.Version {
		case 3:
			migrationThree = item.SQL
		case 9:
			migrationNine = item.SQL
		}
	}
	if migrationThree == "" || migrationNine == "" {
		t.Fatalf("required migrations missing: v3=%t v9=%t",
			migrationThree != "", migrationNine != "")
	}

	var initialChainStateDDL string
	for _, statement := range splitMigrationStatements(migrationThree) {
		if strings.Contains(statement, "CREATE TABLE IF NOT EXISTS gold_chain_states") {
			initialChainStateDDL = statement
			break
		}
	}
	if initialChainStateDDL == "" {
		t.Fatal("migration 3 does not create gold_chain_states")
	}
	if strings.Contains(initialChainStateDDL, "contract_address") {
		t.Fatal("migration 3 must keep the historical schema; migration 9 adds contract_address")
	}
	if !strings.Contains(migrationNine, "ADD COLUMN contract_address") {
		t.Fatal("migration 9 does not add contract_address")
	}
}

func TestMigrationNineRecognizesOnlyKnownPartialMigrationErrors(t *testing.T) {
	tests := []struct {
		name      string
		version   int64
		statement int
		number    uint16
		want      bool
	}{
		{"duplicate contract column", 9, 0, 1060, true},
		{"primary key already dropped", 9, 2, 1091, true},
		{"duplicate game index", 9, 4, 1061, true},
		{"wrong statement", 9, 1, 1060, false},
		{"wrong migration", 8, 0, 1060, false},
		{"unknown mysql error", 9, 0, 1045, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := &mysql.MySQLError{Number: test.number, Message: "test"}
			if got := isRecoverableMigrationNineError(
				test.version, test.statement, err,
			); got != test.want {
				t.Fatalf("recoverable=%t, want %t", got, test.want)
			}
		})
	}
}
