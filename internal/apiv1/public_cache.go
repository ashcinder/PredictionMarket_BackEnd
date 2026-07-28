package apiv1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	appcache "PredictionMarket/internal/cache"
)

const (
	publicCacheVersionKey = "public-data:version"
	maxPublicCacheBody    = 2 << 20
)

type cachedHTTPResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"body"`
}

type PublicCacheMiddleware struct {
	store appcache.Store
	ttl   time.Duration
}

func NewPublicCacheMiddleware(store appcache.Store, ttl time.Duration) *PublicCacheMiddleware {
	return &PublicCacheMiddleware{store: store, ttl: ttl}
}

func (m *PublicCacheMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicCacheableGET(r) {
			m.serveCached(w, r, next)
			return
		}
		if isPublicCacheMutation(r) {
			recorder := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(recorder, r)
			if recorder.status >= 200 && recorder.status < 300 {
				if _, err := m.store.Increment(r.Context(), publicCacheVersionKey); err != nil {
					slog.Debug("redis public cache invalidation failed", "error", err)
				}
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *PublicCacheMiddleware) serveCached(
	w http.ResponseWriter,
	r *http.Request,
	next http.Handler,
) {
	version := m.publicVersion(r.Context())
	key := publicResponseKey(version, r.URL.RequestURI())
	if raw, hit, err := m.store.Get(r.Context(), key); err == nil && hit {
		var cached cachedHTTPResponse
		if json.Unmarshal(raw, &cached) == nil && cached.Status == http.StatusOK {
			if cached.ContentType != "" {
				w.Header().Set("Content-Type", cached.ContentType)
			}
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(cached.Status)
			_, _ = w.Write(cached.Body)
			slog.Info("redis cache hit", "component", "public_game_data", "path", r.URL.Path)
			return
		}
	} else if err != nil {
		slog.Debug("redis public cache read failed", "error", err)
	}

	w.Header().Set("X-Cache", "MISS")
	recorder := newCacheResponseRecorder(w, maxPublicCacheBody)
	next.ServeHTTP(recorder, r)
	if recorder.status != http.StatusOK || recorder.overflow {
		return
	}
	cached := cachedHTTPResponse{
		Status:      recorder.status,
		ContentType: recorder.Header().Get("Content-Type"),
		Body:        recorder.body.Bytes(),
	}
	raw, err := json.Marshal(cached)
	if err == nil {
		if err := m.store.Set(r.Context(), key, raw, m.ttl); err != nil {
			slog.Debug("redis public cache write failed", "error", err)
		}
	}
}

func (m *PublicCacheMiddleware) publicVersion(ctx context.Context) int64 {
	raw, hit, err := m.store.Get(ctx, publicCacheVersionKey)
	if err != nil || !hit {
		return 0
	}
	version, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || version < 0 {
		return 0
	}
	return version
}

func publicResponseKey(version int64, requestURI string) string {
	sum := sha256.Sum256([]byte(requestURI))
	return "public-data:v" + strconv.FormatInt(version, 10) + ":" + hex.EncodeToString(sum[:])
}

func isPublicCacheableGET(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	const prefix = "/api/v1/gold/games"
	if r.URL.Path == prefix {
		return true
	}
	if r.URL.Path == prefix+"/chain-states" {
		return strings.TrimSpace(r.URL.Query().Get("user_address")) == ""
	}
	if !strings.HasPrefix(r.URL.Path, prefix+"/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/"), "/")
	switch {
	case len(parts) == 1:
		return parts[0] != ""
	case len(parts) == 2 && parts[1] == "history":
		return parts[0] != ""
	case len(parts) == 2 && parts[1] == "chain-state":
		return parts[0] != "" && strings.TrimSpace(r.URL.Query().Get("user_address")) == ""
	default:
		return false
	}
}

func isPublicCacheMutation(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	path := r.URL.Path
	return path == "/api/v1/gold/games/sync" ||
		strings.HasSuffix(path, "/chain-state/sync") ||
		strings.HasSuffix(path, "/history")
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

type cacheResponseRecorder struct {
	http.ResponseWriter
	status   int
	body     bytes.Buffer
	maxBytes int
	overflow bool
}

func newCacheResponseRecorder(w http.ResponseWriter, maxBytes int) *cacheResponseRecorder {
	return &cacheResponseRecorder{ResponseWriter: w, maxBytes: maxBytes}
}

func (r *cacheResponseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *cacheResponseRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if !r.overflow {
		remaining := r.maxBytes - r.body.Len()
		if len(body) <= remaining {
			_, _ = r.body.Write(body)
		} else {
			r.overflow = true
			r.body.Reset()
		}
	}
	return r.ResponseWriter.Write(body)
}
