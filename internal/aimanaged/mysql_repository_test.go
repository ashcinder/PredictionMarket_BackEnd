package aimanaged

import (
	"context"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

const repositoryTestContract = "0xad4f9ed0f2b51a26314c9f83df588ccce26ae03c"

func TestMySQLRepositoryMergeAndListPersistsSeedsAndChainPoint(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewMySQLRepository(db)
	market := MarketIdentity{ContractAddress: repositoryTestContract, GameID: 1}
	seed := []HistoryObservation{{Time: 100, YesPercent: 51, NoPercent: 49, Source: historySourceIPFS}}
	current := HistoryObservation{
		Time: 120, YesPercent: 60, NoPercent: 40,
		ReserveNO: big.NewInt(40), ReserveYES: big.NewInt(60), Source: historySourceChain,
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT IGNORE INTO market_history").
		WithArgs(repositoryTestContract, 1, int64(100), "51.000000", "49.000000", nil, nil, historySourceIPFS).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO market_history").
		WithArgs(repositoryTestContract, 1, int64(120), "60.000000", "40.000000", []byte("40"), []byte("60"), historySourceChain).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM market_history").
		WithArgs(repositoryTestContract, 1, repositoryTestContract, 1, 3).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT observed_at, yes_percent, no_percent, reserve_no, reserve_yes, source").
		WithArgs(repositoryTestContract, 1, 3).
		WillReturnRows(sqlmock.NewRows([]string{
			"observed_at", "yes_percent", "no_percent", "reserve_no", "reserve_yes", "source",
		}).AddRow(120, "60.000000", "40.000000", []byte("40"), []byte("60"), "chain").
			AddRow(100, "51.000000", "49.000000", nil, nil, "ipfs"))
	mock.ExpectCommit()

	got, err := repository.MergeAndList(context.Background(), market, seed, current, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Time != 100 || got[1].Time != 120 ||
		got[1].ReserveYES.Cmp(big.NewInt(60)) != 0 {
		t.Fatalf("unexpected history: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryListRejectsUnsafeLimits(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewMySQLRepository(db)
	for _, limit := range []int{0, 1001} {
		if _, err := repository.List(context.Background(), MarketIdentity{ContractAddress: repositoryTestContract, GameID: 1}, limit); err == nil {
			t.Fatalf("limit %d was accepted", limit)
		}
	}
}

func TestMySQLRepositoryRejectsInvalidPercentageSum(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewMySQLRepository(db)
	_, err = repository.MergeAndList(context.Background(),
		MarketIdentity{ContractAddress: repositoryTestContract, GameID: 1}, nil,
		HistoryObservation{
			Time: 1, YesPercent: 70, NoPercent: 20,
			ReserveNO: big.NewInt(20), ReserveYES: big.NewInt(70), Source: historySourceChain,
		}, 1)
	if err == nil || !strings.Contains(err.Error(), "percentages") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMySQLRepositoryListRejectsOversizedStoredReserve(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewMySQLRepository(db)
	mock.ExpectQuery("SELECT observed_at, yes_percent, no_percent, reserve_no, reserve_yes, source").
		WithArgs(repositoryTestContract, 1, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"observed_at", "yes_percent", "no_percent", "reserve_no", "reserve_yes", "source",
		}).AddRow(1, "50.000000", "50.000000", make([]byte, 33), []byte{1}, "chain"))

	_, err = repository.List(context.Background(), MarketIdentity{ContractAddress: repositoryTestContract, GameID: 1}, 1)
	if err == nil || !strings.Contains(err.Error(), "reserve_no") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMySQLRepositoryRecordsAndFinalizesDecisions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewMySQLRepository(db)
	market := MarketIdentity{ContractAddress: repositoryTestContract, GameID: 1}

	mock.ExpectExec("INSERT INTO ai_decisions").
		WithArgs(repositoryTestContract, 1, repositoryTestContract, int64(100), "rule", "hold", "0.000000", "need history", 2, "history_insufficient").
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := repository.RecordRule(context.Background(), RuleDecisionRecord{
		Market: market, UserAddress: repositoryTestContract, ObservedAt: 100,
		Action: "hold", Reason: "need history", HistoryPoints: 2, Outcome: "history_insufficient",
	}); err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec("INSERT INTO ai_decisions").
		WithArgs(repositoryTestContract, 1, repositoryTestContract, int64(120), "model", "buy_yes", "0.900000", "strong", 3, "pending").
		WillReturnResult(sqlmock.NewResult(42, 1))
	id, err := repository.CreatePending(context.Background(), ModelDecisionRecord{
		Market: market, UserAddress: repositoryTestContract, ObservedAt: 120,
		Action: "buy_yes", Confidence: .9, Reason: "strong", HistoryPoints: 3,
	})
	if err != nil || id != 42 {
		t.Fatalf("id=%d error=%v", id, err)
	}

	mock.ExpectExec(regexp.QuoteMeta("UPDATE ai_decisions SET outcome=?, tx_hash=?, error_summary=? WHERE id=?")).
		WithArgs("traded", "0xtest", "", int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repository.Finalize(context.Background(), 42, "traded", "0xtest", ""); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryRecordsAIManagedNOTradeAndPositionAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewMySQLRepository(db)
	record := ManagedTradeRecord{
		Market:       MarketIdentity{ContractAddress: repositoryTestContract, GameID: 42},
		UserAddress:  repositoryTestContract,
		OptionID:     1, // YES=0, NO=1
		AmountWei:    big.NewInt(1000),
		SharesDelta:  big.NewInt(250),
		SharesYES:    big.NewInt(10),
		SharesNO:     big.NewInt(350),
		TxHash:       "0xmanaged",
		TimestampSec: 1782782000,
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE gold_trades SET").
		WithArgs(
			1, []byte("1000"), "250", []byte("250"), int64(1782782000),
			"10", "350", repositoryTestContract, 42, repositoryTestContract, "0xmanaged",
		).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT id FROM gold_trades").
		WithArgs(repositoryTestContract, 42, repositoryTestContract, "0xmanaged").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("INSERT INTO gold_trades").
		WithArgs(
			42, repositoryTestContract, repositoryTestContract, 1,
			[]byte("1000"), "250", []byte("250"), int64(1782782000),
			"0xmanaged", "10", "350",
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO gold_user_positions").
		WithArgs(repositoryTestContract, 42, []byte("10"), []byte("350")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repository.RecordManagedTrade(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryRecordsMarketSyncState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewMySQLRepository(db)
	market := MarketIdentity{ContractAddress: repositoryTestContract, GameID: 1}
	now := time.Unix(1000, 0).UTC()

	mock.ExpectExec("INSERT INTO market_sync_state").
		WithArgs(repositoryTestContract, 1, now, int64(960), now, "ok").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repository.RecordSyncSuccess(context.Background(), market, 960, now); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("SELECT last_success_at, last_observed_at, fail_count, next_poll_at, last_error, status FROM market_sync_state").
		WithArgs(repositoryTestContract, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"last_success_at", "last_observed_at", "fail_count", "next_poll_at", "last_error", "status",
		}).AddRow(now, int64(960), 0, now, "", syncStatusOK))
	mock.ExpectExec("INSERT INTO market_sync_state").
		WithArgs(repositoryTestContract, 1, 1, nextSyncPollTime(now, 1), "broker chain eth_call: HTTP 504 gateway timeout", "failed").
		WillReturnResult(sqlmock.NewResult(0, 1))
	state, err := repository.RecordSyncFailure(context.Background(), market, now, errors.New("broker chain eth_call: HTTP 504 gateway timeout"))
	if err != nil {
		t.Fatal(err)
	}
	if state.FailCount != 1 || state.Status != syncStatusFailed || !state.NextPollAt.Equal(nextSyncPollTime(now, 1)) {
		t.Fatalf("unexpected sync state: %+v", state)
	}

	mock.ExpectQuery("SELECT last_success_at, last_observed_at, fail_count, next_poll_at, last_error, status FROM market_sync_state").
		WithArgs(repositoryTestContract, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"last_success_at", "last_observed_at", "fail_count", "next_poll_at", "last_error", "status",
		}).AddRow(now, int64(960), 1, nextSyncPollTime(now, 1), "gateway timeout", syncStatusFailed))
	got, err := repository.GetSyncState(context.Background(), market)
	if err != nil {
		t.Fatal(err)
	}
	if got.FailCount != 1 || got.LastObservedAt != 960 || got.LastError != "gateway timeout" {
		t.Fatalf("unexpected fetched sync state: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeErrorSummaryRemovesLinesAndKeepsUTF8(t *testing.T) {
	raw := strings.Repeat("错", 200) + "\nsecret line"
	got := sanitizeErrorSummary(raw)
	if strings.ContainsAny(got, "\r\n") || len([]byte(got)) > 512 || !strings.HasPrefix(got, "错") {
		t.Fatalf("unexpected sanitized summary: %q (%d bytes)", got, len([]byte(got)))
	}
}
