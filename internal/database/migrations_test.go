package database

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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
}
