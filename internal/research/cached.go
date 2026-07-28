package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"

	appcache "PredictionMarket/internal/cache"

	"golang.org/x/sync/singleflight"
)

const researchCacheVersion = "v1"

// CachedResearcher stores only a SHA-256 key and the completed answer. Raw
// prompts are never written to Redis. singleflight also collapses concurrent
// identical requests in this process before they consume extra model tokens.
type CachedResearcher struct {
	next     Researcher
	cache    appcache.Store
	ttl      time.Duration
	identity string
	group    singleflight.Group
}

func NewCachedResearcher(
	next Researcher,
	store appcache.Store,
	ttl time.Duration,
	providerIdentity ...string,
) *CachedResearcher {
	return &CachedResearcher{
		next:     next,
		cache:    store,
		ttl:      ttl,
		identity: strings.Join(providerIdentity, "\x00"),
	}
}

func (r *CachedResearcher) Research(
	ctx context.Context,
	systemPrompt string,
	userMessage string,
) (string, error) {
	key := researchCacheKey(r.identity, systemPrompt, userMessage)
	if content, hit := r.readCache(ctx, key); hit {
		slog.Info("redis cache hit", "component", "ai_research", "request_hash", key)
		return content, nil
	}

	value, err, shared := r.group.Do(key, func() (interface{}, error) {
		// Another request may have filled Redis while this goroutine waited.
		if content, hit := r.readCache(ctx, key); hit {
			return content, nil
		}
		content, upstreamErr := r.next.Research(ctx, systemPrompt, userMessage)
		if upstreamErr != nil {
			return "", upstreamErr
		}
		content = strings.TrimSpace(content)
		if content != "" {
			if cacheErr := r.cache.Set(ctx, "research:"+key, []byte(content), r.ttl); cacheErr != nil {
				slog.Debug("redis cache write failed", "component", "ai_research", "error", cacheErr)
			}
		}
		return content, nil
	})
	if shared {
		slog.Info("duplicate AI research request coalesced", "request_hash", key)
	}
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

func (r *CachedResearcher) readCache(ctx context.Context, key string) (string, bool) {
	raw, hit, err := r.cache.Get(ctx, "research:"+key)
	if err != nil {
		slog.Debug("redis cache read failed", "component", "ai_research", "error", err)
		return "", false
	}
	content := strings.TrimSpace(string(raw))
	return content, hit && content != ""
}

func researchCacheKey(providerIdentity, systemPrompt, userMessage string) string {
	sum := sha256.Sum256([]byte(
		researchCacheVersion + "\x00" +
			strings.TrimSpace(providerIdentity) + "\x00" +
			strings.TrimSpace(systemPrompt) + "\x00" +
			strings.TrimSpace(userMessage),
	))
	return researchCacheVersion + ":" + hex.EncodeToString(sum[:])
}
