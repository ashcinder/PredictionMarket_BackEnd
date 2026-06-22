package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestConfigurePoolAppliesLimits(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	configurePool(db, Config{
		MaxOpenConnections:    10,
		MaxIdleConnections:    5,
		ConnectionMaxLifetime: 5 * time.Minute,
	})
	if got := db.Stats().MaxOpenConnections; got != 10 {
		t.Fatalf("max open connections=%d, want 10", got)
	}
}

func TestOpenMySQLRejectsMalformedDSNWithoutLeakingIt(t *testing.T) {
	const dsn = "prediction:top-secret::bad-dsn"
	_, err := OpenMySQL(context.Background(), Config{DSN: dsn})
	if err == nil {
		t.Fatal("expected malformed DSN error")
	}
	if strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), dsn) {
		t.Fatalf("error leaked DSN credentials: %v", err)
	}
}
