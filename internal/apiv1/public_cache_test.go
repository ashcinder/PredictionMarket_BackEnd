package apiv1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	appcache "PredictionMarket/internal/cache"

	"github.com/alicebob/miniredis/v2"
)

func newPublicCacheTestStore(t *testing.T) appcache.Store {
	t.Helper()
	server := miniredis.RunT(t)
	store, err := appcache.NewRedisStore(context.Background(), appcache.RedisConfig{
		Address:          server.Addr(),
		KeyPrefix:        "public-test",
		OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestPublicCacheCachesOnlyPublicGameData(t *testing.T) {
	store := newPublicCacheTestStore(t)
	var mu sync.Mutex
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"games":[]}`))
	})
	handler := NewPublicCacheMiddleware(store, time.Minute).Wrap(next)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/gold/games", nil))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/v1/gold/games", nil))
	if first.Header().Get("X-Cache") != "MISS" || second.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("cache headers first=%q second=%q",
			first.Header().Get("X-Cache"), second.Header().Get("X-Cache"))
	}
	if calls != 1 || first.Body.String() != second.Body.String() {
		t.Fatalf("public response was not reused: calls=%d", calls)
	}

	personalizedURL := "/api/v1/gold/games/1/chain-state?user_address=0xabc"
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, personalizedURL, nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, personalizedURL, nil))
	if calls != 3 {
		t.Fatalf("personalized responses must bypass public cache: calls=%d", calls)
	}
}

func TestPublicCacheMutationInvalidatesPreviousGeneration(t *testing.T) {
	store := newPublicCacheTestStore(t)
	getCalls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			return
		}
		getCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"history":[]}`))
	})
	handler := NewPublicCacheMiddleware(store, time.Minute).Wrap(next)
	getURL := "/api/v1/gold/games/1/history?range=1h"

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, getURL, nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, getURL, nil))
	if getCalls != 1 {
		t.Fatalf("initial response was not cached: calls=%d", getCalls)
	}

	mutationURL := "/api/v1/gold/games/1/history"
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, mutationURL, nil))
	afterMutation := httptest.NewRecorder()
	handler.ServeHTTP(afterMutation, httptest.NewRequest(http.MethodGet, getURL, nil))
	if getCalls != 2 || afterMutation.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("mutation did not invalidate cache: calls=%d cache=%q",
			getCalls, afterMutation.Header().Get("X-Cache"))
	}
}
