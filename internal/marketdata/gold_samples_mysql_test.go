package marketdata

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMySQLGoldSampleRepositoryAppendsAndReadsRange(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewMySQLGoldSampleRepository(db)
	start := time.Date(2026, 7, 14, 7, 8, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta(appendGoldSampleSQL)).
		WithArgs("XAU", start.Unix(), 4016.8, "gold-api.com").
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := repository.Append(context.Background(), GoldSample{
		Symbol: "xau", ObservedAt: start, Price: 4016.8, Source: "gold-api.com",
	}); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(regexp.QuoteMeta(listGoldSamplesSQL)).
		WithArgs("XAU", start.Unix(), start.Add(time.Minute).Unix()).
		WillReturnRows(sqlmock.NewRows([]string{"symbol", "observed_at", "price_usd", "source"}).
			AddRow("XAU", start.Unix(), 4016.8, "gold-api.com").
			AddRow("XAU", start.Add(time.Minute).Unix(), 4018.2, "新浪财经"))
	samples, err := repository.Range(context.Background(), "XAU", start, start.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[1].Price != 4018.2 || !samples[1].ObservedAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("unexpected samples: %+v", samples)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
