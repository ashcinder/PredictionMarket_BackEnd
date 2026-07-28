package cache

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store is the small cache surface used by the HTTP, quote and research
// layers. MySQL and the chain remain the sources of truth.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Increment(ctx context.Context, key string) (int64, error)
	Close() error
}

type RedisConfig struct {
	Address          string
	Password         string
	DB               int
	KeyPrefix        string
	OperationTimeout time.Duration
}

type RedisStore struct {
	client           *redis.Client
	keyPrefix        string
	operationTimeout time.Duration
}

func NewRedisStore(ctx context.Context, cfg RedisConfig) (*RedisStore, error) {
	timeout := cfg.OperationTimeout
	if timeout <= 0 {
		timeout = 300 * time.Millisecond
	}
	store := &RedisStore{
		client: redis.NewClient(&redis.Options{
			Addr:         strings.TrimSpace(cfg.Address),
			Password:     cfg.Password,
			DB:           cfg.DB,
			DialTimeout:  timeout,
			ReadTimeout:  timeout,
			WriteTimeout: timeout,
		}),
		keyPrefix:        strings.Trim(strings.TrimSpace(cfg.KeyPrefix), ":"),
		operationTimeout: timeout,
	}
	pingCtx, cancel := store.operationContext(ctx)
	defer cancel()
	if err := store.client.Ping(pingCtx).Err(); err != nil {
		_ = store.client.Close()
		return nil, err
	}
	return store, nil
}

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if s == nil || s.client == nil {
		return nil, false, errors.New("redis cache is not initialized")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	value, err := s.client.Get(opCtx, s.key(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func (s *RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return errors.New("redis cache is not initialized")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	return s.client.Set(opCtx, s.key(key), value, ttl).Err()
}

func (s *RedisStore) Increment(ctx context.Context, key string) (int64, error) {
	if s == nil || s.client == nil {
		return 0, errors.New("redis cache is not initialized")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	return s.client.Incr(opCtx, s.key(key)).Result()
}

func (s *RedisStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *RedisStore) key(key string) string {
	key = strings.TrimLeft(strings.TrimSpace(key), ":")
	if s.keyPrefix == "" {
		return key
	}
	return s.keyPrefix + ":" + key
}

func (s *RedisStore) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, s.operationTimeout)
}
