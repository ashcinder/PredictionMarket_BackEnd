package research

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	appcache "PredictionMarket/internal/cache"

	"github.com/alicebob/miniredis/v2"
)

type countingResearcher struct {
	mu    sync.Mutex
	calls int
	delay time.Duration
}

func (r *countingResearcher) Research(
	_ context.Context,
	_ string,
	userMessage string,
) (string, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()
	time.Sleep(r.delay)
	return fmt.Sprintf("answer-%d:%s", call, userMessage), nil
}

func (r *countingResearcher) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func TestCachedResearcherReusesAnswerAndHidesPromptInKey(t *testing.T) {
	server := miniredis.RunT(t)
	store, err := appcache.NewRedisStore(context.Background(), appcache.RedisConfig{
		Address:          server.Addr(),
		KeyPrefix:        "research-test",
		OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	upstream := &countingResearcher{}
	researcher := NewCachedResearcher(upstream, store, 10*time.Minute)
	const prompt = "private market prompt"
	first, err := researcher.Research(context.Background(), "system", prompt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := researcher.Research(context.Background(), "system", prompt)
	if err != nil {
		t.Fatal(err)
	}
	if upstream.callCount() != 1 || first != second {
		t.Fatalf("duplicate prompt called upstream %d times", upstream.callCount())
	}
	for _, key := range server.Keys() {
		if strings.Contains(key, prompt) {
			t.Fatalf("raw prompt leaked into Redis key %q", key)
		}
	}
}

func TestCachedResearcherCoalescesConcurrentDuplicates(t *testing.T) {
	server := miniredis.RunT(t)
	store, err := appcache.NewRedisStore(context.Background(), appcache.RedisConfig{
		Address:          server.Addr(),
		KeyPrefix:        "research-concurrent-test",
		OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	upstream := &countingResearcher{delay: 40 * time.Millisecond}
	researcher := NewCachedResearcher(upstream, store, 10*time.Minute)
	const concurrentRequests = 8
	start := make(chan struct{})
	errs := make(chan error, concurrentRequests)
	var wait sync.WaitGroup
	for i := 0; i < concurrentRequests; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := researcher.Research(context.Background(), "system", "same snapshot")
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if upstream.callCount() != 1 {
		t.Fatalf("concurrent duplicate prompts called upstream %d times", upstream.callCount())
	}
}
