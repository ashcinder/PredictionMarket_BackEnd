package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestRedisStorePrefixTTLAndIncrement(t *testing.T) {
	server := miniredis.RunT(t)
	store, err := NewRedisStore(context.Background(), RedisConfig{
		Address:          server.Addr(),
		KeyPrefix:        "prediction:test",
		OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Set(context.Background(), "quote", []byte("cached"), 10*time.Second); err != nil {
		t.Fatal(err)
	}
	value, hit, err := store.Get(context.Background(), "quote")
	if err != nil || !hit || string(value) != "cached" {
		t.Fatalf("Get()=(%q,%t,%v), want cached hit", value, hit, err)
	}
	if raw, err := server.Get("prediction:test:quote"); err != nil || raw != "cached" {
		t.Fatalf("prefixed Redis value=(%q,%v)", raw, err)
	}

	server.FastForward(11 * time.Second)
	if _, hit, err := store.Get(context.Background(), "quote"); err != nil || hit {
		t.Fatalf("expired Get() hit=%t err=%v", hit, err)
	}

	if value, err := store.Increment(context.Background(), "version"); err != nil || value != 1 {
		t.Fatalf("Increment()=(%d,%v), want 1", value, err)
	}
	if value, err := store.Increment(context.Background(), "version"); err != nil || value != 2 {
		t.Fatalf("Increment()=(%d,%v), want 2", value, err)
	}
}
