//go:build integration

package aimanaged

import (
	"context"
	"math/big"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	appdb "PredictionMarket/internal/database"
	mysql "github.com/go-sql-driver/mysql"
)

func TestMySQLIntegration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MYSQL_TEST_DSN"))
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(parsed.DBName, "_test") {
		t.Fatalf("refusing integration test database %q: name must end in _test", parsed.DBName)
	}
	db, err := appdb.OpenMySQL(context.Background(), appdb.Config{
		DSN: dsn, MaxOpenConnections: 10, MaxIdleConnections: 5,
		ConnectionMaxLifetime: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"ai_decisions", "market_history"} {
		if _, err := db.Exec("TRUNCATE TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
	repository := NewMySQLRepository(db)
	market := MarketIdentity{ContractAddress: repositoryTestContract, GameID: 1}
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	first := HistoryObservation{
		Time: 100, YesPercent: 60, NoPercent: 40,
		ReserveNO: maxUint256, ReserveYES: big.NewInt(1), Source: historySourceChain,
	}
	if _, err := repository.MergeAndList(context.Background(), market, nil, first, 256); err != nil {
		t.Fatal(err)
	}
	second := HistoryObservation{
		Time: 120, YesPercent: 55, NoPercent: 45,
		ReserveNO: big.NewInt(45), ReserveYES: big.NewInt(55), Source: historySourceChain,
	}
	history, err := repository.MergeAndList(context.Background(), market, []HistoryObservation{{
		Time: 100, YesPercent: 99, NoPercent: 1, Source: historySourceIPFS,
	}}, second, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].YesPercent != 60 || history[0].ReserveNO.Cmp(maxUint256) != 0 {
		t.Fatalf("chain point was overwritten or uint256 changed: %+v", history)
	}

	concurrent := HistoryObservation{
		Time: 180, YesPercent: 52, NoPercent: 48,
		ReserveNO: big.NewInt(48), ReserveYES: big.NewInt(52), Source: historySourceChain,
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repository.MergeAndList(context.Background(), market, nil, concurrent, 256)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM market_history
WHERE contract_address=? AND game_id=? AND observed_at=?`, repositoryTestContract, 1, 180).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("same-bucket row count=%d, want 1", count)
	}

	id, err := repository.CreatePending(context.Background(), ModelDecisionRecord{
		Market: market, UserAddress: repositoryTestContract, ObservedAt: 180,
		Action: "buy_yes", Confidence: .9, Reason: "integration", HistoryPoints: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Finalize(context.Background(), id, "traded", "0xtest", ""); err != nil {
		t.Fatal(err)
	}
	var outcome, txHash string
	if err := db.QueryRow("SELECT outcome, tx_hash FROM ai_decisions WHERE id=?", id).Scan(&outcome, &txHash); err != nil {
		t.Fatal(err)
	}
	if outcome != "traded" || txHash != "0xtest" {
		t.Fatalf("outcome=%q tx=%q", outcome, txHash)
	}
}
