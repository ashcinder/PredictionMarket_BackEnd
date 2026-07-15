package marketdata

import (
	"context"
	"math/big"
	"regexp"
	"testing"
	"time"

	"PredictionMarket/internal/chainlinkfeed"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ethereum/go-ethereum/common"
)

var chainlinkXAUFeed = common.HexToAddress("0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6")

func fixtureChainlinkRound() chainlinkfeed.Round {
	roundID, _ := new(big.Int).SetString("92233720368547766366", 10)
	return chainlinkfeed.Round{
		Feed: chainlinkXAUFeed, ID: roundID, Answer: big.NewInt(407910500000),
		Decimals: 8, Price: 4079.105,
		StartedAt:       time.Unix(1784033500, 0).UTC(),
		UpdatedAt:       time.Unix(1784033519, 0).UTC(),
		AnsweredInRound: new(big.Int).Set(roundID),
	}
}

func TestMySQLChainlinkRoundRepositoryUpsertsByFeedAndRound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewMySQLChainlinkRoundRepository(db)
	round := fixtureChainlinkRound()

	mock.ExpectExec(regexp.QuoteMeta(saveChainlinkRoundSQL)).
		WithArgs(
			chainlinkXAUFeed.Hex(), round.ID.String(), round.Answer.String(),
			round.Decimals, round.Price, round.StartedAt.Unix(), round.UpdatedAt.Unix(),
			round.AnsweredInRound.String(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := repo.Save(context.Background(), round); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLChainlinkRoundRepositoryReadsBoundaryAndID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewMySQLChainlinkRoundRepository(db)
	want := fixtureChainlinkRound()
	columns := []string{
		"feed_address", "round_id", "answer_raw", "decimals", "price_usd",
		"started_at_sec", "updated_at_sec", "answered_in_round",
	}
	row := func() *sqlmock.Rows {
		return sqlmock.NewRows(columns).AddRow(
			want.Feed.Hex(), want.ID.String(), want.Answer.String(), want.Decimals,
			want.Price, want.StartedAt.Unix(), want.UpdatedAt.Unix(), want.AnsweredInRound.String(),
		)
	}

	mock.ExpectQuery(regexp.QuoteMeta(latestChainlinkRoundAtOrBeforeSQL)).
		WithArgs(chainlinkXAUFeed.Hex(), int64(1784033600)).WillReturnRows(row())
	got, err := repo.LatestAtOrBefore(context.Background(), chainlinkXAUFeed, time.Unix(1784033600, 0))
	if err != nil || got.ID.Cmp(want.ID) != 0 || got.Price != want.Price {
		t.Fatalf("LatestAtOrBefore() = %+v, %v", got, err)
	}

	mock.ExpectQuery(regexp.QuoteMeta(chainlinkRoundByIDSQL)).
		WithArgs(chainlinkXAUFeed.Hex(), want.ID.String()).WillReturnRows(row())
	got, err = repo.ByID(context.Background(), chainlinkXAUFeed, want.ID)
	if err != nil || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("ByID() = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
