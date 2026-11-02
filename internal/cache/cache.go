// Package cache keeps pastes in Redis in front of a paste.Store, so a paste
// many people read costs MySQL one query an hour instead of one per read.
//
// A paste never changes after it is created, so nothing can go stale: an entry
// only has to end by the time the paste expires.
package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/bulutaysarac/pastebin/internal/metrics"
	"github.com/bulutaysarac/pastebin/internal/paste"
)

const (
	keyPrefix = "paste:"

	// How long a paste stays cached, at most. The jitter keeps pastes read for
	// the first time together from all coming back to MySQL together.
	pasteTTL       = time.Hour
	pasteTTLJitter = 6 * time.Minute

	// How long an unknown id stays cached, so requests for ids that don't exist
	// can't each cost a query.
	missingTTL       = 30 * time.Second
	missingTTLJitter = 5 * time.Second

	// missingMarker is the value of an unknown id. A cached paste starts with {.
	missingMarker = "-"

	// redisTimeout bounds one Redis call.
	redisTimeout = 100 * time.Millisecond

	// skipAfterFailure is how long the cache leaves Redis alone after a call
	// fails. With Redis hung, every read of a different paste would otherwise
	// pay the whole timeout.
	skipAfterFailure = time.Second

	// loadTimeout bounds one load for everyone waiting on it. It stays under the
	// request deadline, so a load that runs out is answered as a timeout, and
	// over MySQL's own 2s, so MySQL gives up first.
	loadTimeout = 4 * time.Second
)

// Store is a paste.Store with Get and Create going through Redis.
type Store struct {
	paste.Store

	rdb         *redis.Client
	log         *slog.Logger
	now         func() time.Time
	loadTimeout time.Duration
	flight      singleflight.Group
	skipUntil   atomic.Int64 // unix nanoseconds
}

func New(next paste.Store, rdb *redis.Client, log *slog.Logger) *Store {
	return &Store{
		Store:       next,
		rdb:         rdb,
		log:         log,
		now:         func() time.Time { return time.Now().UTC() },
		loadTimeout: loadTimeout,
	}
}

// Get returns the paste from Redis, or from the store on a miss. Concurrent
// reads of one id share one load, so a paste that just left the cache costs
// one query however many are reading it. Any Redis failure falls back to the
// store. Store errors aren't cached.
func (s *Store) Get(ctx context.Context, id string) (paste.Paste, error) {
	ch := s.flight.DoChan(id, func() (any, error) {
		// Not the caller's context: the others waiting on this load must not
		// fail because the client that started it went away.
		ctx, cancel := context.WithTimeout(context.Background(), s.loadTimeout)
		defer cancel()
		return s.load(ctx, id)
	})

	select {
	case <-ctx.Done():
		return paste.Paste{}, ctx.Err()
	case r := <-ch:
		if errors.Is(r.Err, context.DeadlineExceeded) {
			return paste.Paste{}, fmt.Errorf("%w: %w", paste.ErrStoreTimeout, r.Err)
		}
		if r.Err != nil {
			return paste.Paste{}, r.Err
		}
		return r.Val.(paste.Paste), nil
	}
}

// Create stores p and clears any marker saying its id doesn't exist.
func (s *Store) Create(ctx context.Context, p paste.Paste) error {
	if err := s.Store.Create(ctx, p); err != nil {
		return err
	}
	if s.skipping() {
		return nil
	}
	rctx, cancel := context.WithTimeout(context.Background(), redisTimeout)
	defer cancel()
	if err := s.rdb.Del(rctx, keyPrefix+p.ID).Err(); err != nil {
		s.redisFailed("del", err)
	}
	return nil
}

func (s *Store) load(ctx context.Context, id string) (paste.Paste, error) {
	if raw, ok := s.read(id); ok {
		if raw == missingMarker {
			return paste.Paste{}, paste.ErrNotFound
		}
		var p paste.Paste
		err := json.Unmarshal([]byte(raw), &p)
		if err == nil {
			return p, nil
		}
		// Not Redis's fault, so it isn't skipped. The load below replaces it.
		metrics.ObserveCacheRequest(metrics.CacheError)
		s.log.Warn("cache: bad entry", "id", id, "err", err)
	}

	p, err := s.Store.Get(ctx, id)
	switch {
	case err == nil:
		s.remember(p)
	case errors.Is(err, paste.ErrNotFound):
		s.rememberMissing(id)
	}
	return p, err
}

// read returns the cached value of id, if Redis has one and answered.
func (s *Store) read(id string) (string, bool) {
	if s.skipping() {
		metrics.ObserveCacheRequest(metrics.CacheError)
		return "", false
	}
	rctx, cancel := context.WithTimeout(context.Background(), redisTimeout)
	defer cancel()

	raw, err := s.rdb.Get(rctx, keyPrefix+id).Result()
	switch {
	case err == nil:
		metrics.ObserveCacheRequest(metrics.CacheHit)
		return raw, true
	case errors.Is(err, redis.Nil):
		metrics.ObserveCacheRequest(metrics.CacheMiss)
	default:
		s.redisFailed("get", err)
	}
	return "", false
}

func (s *Store) remember(p paste.Paste) {
	ttl := jittered(pasteTTL, pasteTTLJitter)
	if p.ExpiresAt != nil {
		left := p.ExpiresAt.Sub(s.now())
		if left <= 0 {
			// The row is waiting for the cleanup job. Readers get a 404 for it.
			s.rememberMissing(p.ID)
			return
		}
		ttl = min(ttl, left)
	}

	// Without the HTML escaping, which would make a paste of HTML up to six
	// times larger here.
	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		s.log.Warn("cache: can't encode paste", "id", p.ID, "err", err)
		return
	}
	s.write(p.ID, raw.Bytes(), ttl)
}

func (s *Store) rememberMissing(id string) {
	s.write(id, missingMarker, jittered(missingTTL, missingTTLJitter))
}

func (s *Store) write(id string, value any, ttl time.Duration) {
	if s.skipping() {
		return
	}
	rctx, cancel := context.WithTimeout(context.Background(), redisTimeout)
	defer cancel()
	if err := s.rdb.Set(rctx, keyPrefix+id, value, ttl).Err(); err != nil {
		s.redisFailed("set", err)
	}
}

func (s *Store) skipping() bool {
	return s.now().UnixNano() < s.skipUntil.Load()
}

func (s *Store) redisFailed(op string, err error) {
	s.skipUntil.Store(s.now().Add(skipAfterFailure).UnixNano())
	metrics.ObserveCacheRequest(metrics.CacheError)
	s.log.Warn("cache: redis failed, using the database", "op", op, "err", err)
}

func jittered(base, jitter time.Duration) time.Duration {
	return base + rand.N(jitter)
}
