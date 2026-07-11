package apiv1

import (
	"context"
	"regexp"
	"testing"

	"PredictionMarket/internal/chain"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestReconcileChainGamesDeletesOnlyStaleCacheRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	contract := "0xad4f9ed0f2b51a26314c9f83df588ccce26ae03c"
	repo := NewMySQLRepository(db, contract)
	chainGames := []chain.GameOnChain{{ID: 2}, {ID: 5}}

	mock.ExpectBegin()
	for _, table := range []string{
		"ai_managed_entries",
		"ai_decisions",
		"market_sync_state",
		"gold_trades",
		"market_history",
		"gold_chain_states",
	} {
		mock.ExpectExec("(?s)DELETE FROM "+table+".*NOT IN").
			WithArgs(contract, 2, 5).
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec("(?s)DELETE p FROM gold_user_positions.*NOT IN").
		WithArgs(contract, 2, 5).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("(?s)DELETE h FROM gold_price_history.*NOT IN").
		WithArgs(contract, 2, 5).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("(?s)DELETE FROM gold_games.*NOT IN").
		WithArgs(contract, 2, 5).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	removed, err := repo.ReconcileChainGames(context.Background(), contract, chainGames)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileChainGamesDeletesAllCacheRowsWhenChainIsEmpty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	contract := "0xad4f9ed0f2b51a26314c9f83df588ccce26ae03c"
	repo := NewMySQLRepository(db, contract)

	mock.ExpectBegin()
	for _, table := range []string{
		"ai_managed_entries",
		"ai_decisions",
		"market_sync_state",
		"gold_trades",
		"market_history",
		"gold_chain_states",
	} {
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM " + table + " WHERE contract_address = ?")).
			WithArgs(contract).
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec("DELETE p FROM gold_user_positions").WithArgs(contract).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE h FROM gold_price_history").WithArgs(contract).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM gold_games WHERE contract_address = ?")).
		WithArgs(contract).
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectCommit()

	removed, err := repo.ReconcileChainGames(context.Background(), contract, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 4 {
		t.Fatalf("removed = %d, want 4", removed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
