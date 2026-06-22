package aimanaged

import (
	"context"
	"math/big"
	"regexp"
	"strings"
	"testing"

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
		WithArgs(repositoryTestContract, 1, int64(120), "60.000000", "40.000000", []byte{40}, []byte{60}, historySourceChain).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT observed_at, yes_percent, no_percent, reserve_no, reserve_yes, source").
		WithArgs(repositoryTestContract, 1, 3).
		WillReturnRows(sqlmock.NewRows([]string{
			"observed_at", "yes_percent", "no_percent", "reserve_no", "reserve_yes", "source",
		}).AddRow(120, "60.000000", "40.000000", []byte{40}, []byte{60}, "chain").
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

func TestSanitizeErrorSummaryRemovesLinesAndKeepsUTF8(t *testing.T) {
	raw := strings.Repeat("错", 200) + "\nsecret line"
	got := sanitizeErrorSummary(raw)
	if strings.ContainsAny(got, "\r\n") || len([]byte(got)) > 512 || !strings.HasPrefix(got, "错") {
		t.Fatalf("unexpected sanitized summary: %q (%d bytes)", got, len([]byte(got)))
	}
}
