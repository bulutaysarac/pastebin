package cache

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/bulutaysarac/pastebin/internal/paste"
)

// fakeStore counts the reads that reach it.
type fakeStore struct {
	mu     sync.Mutex
	pastes map[string]paste.Paste
	gets   int
	err    error         // what Get returns instead of working
	block  chan struct{} // Get waits for this to close
	hang   bool          // Get waits until its context ends
}

func (f *fakeStore) Create(_ context.Context, p paste.Paste) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pastes[p.ID] = p
	return nil
}

func (f *fakeStore) Get(ctx context.Context, id string) (paste.Paste, error) {
	f.mu.Lock()
	f.gets++
	block, hang, err := f.block, f.hang, f.err
	f.mu.Unlock()

	if block != nil {
		<-block
	}
	if hang {
		<-ctx.Done()
	}
	// Like a real database call, it fails once its context has ended.
	if err := ctx.Err(); err != nil {
		return paste.Paste{}, err
	}
	if err != nil {
		return paste.Paste{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pastes[id]
	if !ok {
		return paste.Paste{}, paste.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) Views(context.Context, string) (int64, *time.Time, error) {
	return 0, nil, nil
}

func (f *fakeStore) getCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets
}

type fixture struct {
	mr    *miniredis.Miniredis
	store *fakeStore
	cache *Store
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1, DialerRetries: 1})
	t.Cleanup(func() { rdb.Close() })

	f := &fixture{
		mr:    mr,
		store: &fakeStore{pastes: map[string]paste.Paste{}},
		now:   time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC),
	}
	f.cache = New(f.store, rdb, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.cache.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) get(t *testing.T, id string) (paste.Paste, error) {
	t.Helper()
	return f.cache.Get(context.Background(), id)
}

func TestSecondReadComesFromRedisWithAllItsFields(t *testing.T) {
	f := newFixture(t)
	exp := f.now.Add(24 * time.Hour)
	want := paste.Paste{ID: "aB3xK9pQ", Content: `<b>"hi"</b> 🚀`, Language: "sql", CreatedAt: f.now, ExpiresAt: &exp}
	f.store.pastes[want.ID] = want

	for range 2 {
		got, err := f.get(t, want.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != want.ID || got.Content != want.Content || got.Language != want.Language ||
			!got.CreatedAt.Equal(want.CreatedAt) || got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
	if n := f.store.getCount(); n != 1 {
		t.Fatalf("store read %d times, want 1", n)
	}
}

func TestAPasteThatNeverExpiresStaysNilAfterTheCache(t *testing.T) {
	f := newFixture(t)
	f.store.pastes["neverexp"] = paste.Paste{ID: "neverexp", Content: "x", CreatedAt: f.now}

	for range 2 {
		got, err := f.get(t, "neverexp")
		if err != nil {
			t.Fatal(err)
		}
		if got.ExpiresAt != nil {
			t.Fatalf("ExpiresAt = %v, want nil", got.ExpiresAt)
		}
	}
}

func TestTTLIsAboutAnHourWithJitter(t *testing.T) {
	f := newFixture(t)
	seen := map[time.Duration]bool{}
	for _, id := range []string{"aaaaaaaa", "bbbbbbbb", "cccccccc", "dddddddd", "eeeeeeee"} {
		f.store.pastes[id] = paste.Paste{ID: id, Content: "x", CreatedAt: f.now}
		if _, err := f.get(t, id); err != nil {
			t.Fatal(err)
		}
		ttl := f.mr.TTL(keyPrefix + id)
		if ttl < pasteTTL || ttl >= pasteTTL+pasteTTLJitter {
			t.Fatalf("ttl = %v, want within [%v, %v)", ttl, pasteTTL, pasteTTL+pasteTTLJitter)
		}
		seen[ttl] = true
	}
	if len(seen) == 1 {
		t.Fatal("five pastes got the same TTL: no jitter")
	}
}

func TestTTLNeverRunsPastTheExpiry(t *testing.T) {
	f := newFixture(t)
	exp := f.now.Add(10 * time.Minute)
	f.store.pastes["shortone"] = paste.Paste{ID: "shortone", Content: "x", CreatedAt: f.now, ExpiresAt: &exp}

	if _, err := f.get(t, "shortone"); err != nil {
		t.Fatal(err)
	}
	if ttl := f.mr.TTL(keyPrefix + "shortone"); ttl != 10*time.Minute {
		t.Fatalf("ttl = %v, want the 10m left", ttl)
	}
}

func TestARowPastItsExpiryIsCachedAsMissing(t *testing.T) {
	f := newFixture(t)
	exp := f.now.Add(-time.Minute)
	f.store.pastes["expired1"] = paste.Paste{ID: "expired1", Content: "x", CreatedAt: f.now, ExpiresAt: &exp}

	// The service turns the row into a 404, and the next read is answered as one.
	if _, err := f.get(t, "expired1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.get(t, "expired1"); !errors.Is(err, paste.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if n := f.store.getCount(); n != 1 {
		t.Fatalf("store read %d times, want 1", n)
	}
}

func TestUnknownIDIsCachedBriefly(t *testing.T) {
	f := newFixture(t)

	for range 3 {
		if _, err := f.get(t, "NOPE1234"); !errors.Is(err, paste.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	}
	if n := f.store.getCount(); n != 1 {
		t.Fatalf("store read %d times, want 1", n)
	}
	if ttl := f.mr.TTL(keyPrefix + "NOPE1234"); ttl < missingTTL || ttl >= missingTTL+missingTTLJitter {
		t.Fatalf("ttl = %v, want within [%v, %v)", ttl, missingTTL, missingTTL+missingTTLJitter)
	}

	f.mr.FastForward(missingTTL + missingTTLJitter)
	if _, err := f.get(t, "NOPE1234"); !errors.Is(err, paste.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if n := f.store.getCount(); n != 2 {
		t.Fatalf("store read %d times after the marker ended, want 2", n)
	}
}

func TestCreateClearsAMissingMarker(t *testing.T) {
	f := newFixture(t)
	if _, err := f.get(t, "aB3xK9pQ"); !errors.Is(err, paste.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	p := paste.Paste{ID: "aB3xK9pQ", Content: "now it exists", CreatedAt: f.now}
	if err := f.cache.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	got, err := f.get(t, "aB3xK9pQ")
	if err != nil {
		t.Fatalf("read after create: %v", err)
	}
	if got.Content != p.Content {
		t.Fatalf("content = %q", got.Content)
	}
}

func TestStoreErrorsAreNotCached(t *testing.T) {
	f := newFixture(t)
	f.store.pastes["aB3xK9pQ"] = paste.Paste{ID: "aB3xK9pQ", Content: "x", CreatedAt: f.now}

	boom := errors.New("database down")
	f.store.err = boom
	if _, err := f.get(t, "aB3xK9pQ"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if f.mr.Exists(keyPrefix + "aB3xK9pQ") {
		t.Fatal("a store error left a key in Redis")
	}

	f.store.err = nil
	if _, err := f.get(t, "aB3xK9pQ"); err != nil {
		t.Fatalf("read after the database came back: %v", err)
	}
}

func TestBadEntryIsReplacedFromTheStore(t *testing.T) {
	f := newFixture(t)
	f.store.pastes["aB3xK9pQ"] = paste.Paste{ID: "aB3xK9pQ", Content: "good", CreatedAt: f.now}
	if err := f.mr.Set(keyPrefix+"aB3xK9pQ", "{not json"); err != nil {
		t.Fatal(err)
	}

	got, err := f.get(t, "aB3xK9pQ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "good" {
		t.Fatalf("content = %q", got.Content)
	}
	if raw, _ := f.mr.Get(keyPrefix + "aB3xK9pQ"); raw == "{not json" {
		t.Fatal("the bad entry is still there")
	}
}

func TestRedisDownFallsBackAndLeavesRedisAloneForASecond(t *testing.T) {
	f := newFixture(t)
	f.store.pastes["aB3xK9pQ"] = paste.Paste{ID: "aB3xK9pQ", Content: "x", CreatedAt: f.now}

	addr := f.mr.Addr()
	f.mr.Close()
	if _, err := f.get(t, "aB3xK9pQ"); err != nil {
		t.Fatalf("read with Redis down: %v", err)
	}

	// Redis is back, but the cache doesn't look yet.
	if err := f.mr.Restart(); err != nil {
		t.Fatal(err)
	}
	if f.mr.Addr() != addr {
		t.Fatalf("miniredis moved from %s to %s", addr, f.mr.Addr())
	}
	if _, err := f.get(t, "aB3xK9pQ"); err != nil {
		t.Fatal(err)
	}
	if f.mr.Exists(keyPrefix + "aB3xK9pQ") {
		t.Fatal("the cache used Redis within a second of a failure")
	}

	f.now = f.now.Add(skipAfterFailure)
	if _, err := f.get(t, "aB3xK9pQ"); err != nil {
		t.Fatal(err)
	}
	if !f.mr.Exists(keyPrefix + "aB3xK9pQ") {
		t.Fatal("the cache didn't go back to Redis after the pause")
	}
}

func TestHungRedisCostsOneTimeout(t *testing.T) {
	// Accepts connections and never answers.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	rdb := redis.NewClient(&redis.Options{Addr: ln.Addr().String(), MaxRetries: -1, ContextTimeoutEnabled: true, DisableIdentity: true})
	defer rdb.Close()
	store := &fakeStore{pastes: map[string]paste.Paste{"aB3xK9pQ": {ID: "aB3xK9pQ", Content: "x"}}}
	c := New(store, rdb, slog.New(slog.NewTextHandler(io.Discard, nil)))

	start := time.Now()
	if _, err := c.Get(context.Background(), "aB3xK9pQ"); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("first read took %v, want about one %v timeout", took, redisTimeout)
	}

	start = time.Now()
	if _, err := c.Get(context.Background(), "aB3xK9pQ"); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > redisTimeout/2 {
		t.Fatalf("second read took %v: it should have skipped Redis", took)
	}
}

func TestConcurrentMissesShareOneLoad(t *testing.T) {
	f := newFixture(t)
	f.store.pastes["aB3xK9pQ"] = paste.Paste{ID: "aB3xK9pQ", Content: "x", CreatedAt: f.now}
	f.store.block = make(chan struct{})

	const readers = 50
	errs := make(chan error, readers)
	for range readers {
		go func() {
			_, err := f.cache.Get(context.Background(), "aB3xK9pQ")
			errs <- err
		}()
	}
	waitFor(t, func() bool { return f.store.getCount() > 0 })
	time.Sleep(50 * time.Millisecond) // lets the others join the load
	close(f.store.block)

	for range readers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if n := f.store.getCount(); n != 1 {
		t.Fatalf("store read %d times, want 1", n)
	}
}

func TestAClientLeavingDoesNotFailTheOthers(t *testing.T) {
	f := newFixture(t)
	f.store.pastes["aB3xK9pQ"] = paste.Paste{ID: "aB3xK9pQ", Content: "x", CreatedAt: f.now}
	f.store.block = make(chan struct{})

	ctx, leave := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := f.cache.Get(ctx, "aB3xK9pQ")
		first <- err
	}()
	waitFor(t, func() bool { return f.store.getCount() > 0 })

	second := make(chan error, 1)
	go func() {
		_, err := f.cache.Get(context.Background(), "aB3xK9pQ")
		second <- err
	}()
	time.Sleep(50 * time.Millisecond) // lets it join the load

	leave()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("the client that left got %v, want context.Canceled", err)
	}
	close(f.store.block)
	if err := <-second; err != nil {
		t.Fatalf("the client that stayed got %v", err)
	}
}

func TestALoadThatRunsOutIsATimeout(t *testing.T) {
	f := newFixture(t)
	f.store.hang = true
	f.cache.loadTimeout = 20 * time.Millisecond

	if _, err := f.get(t, "aB3xK9pQ"); !errors.Is(err, paste.ErrStoreTimeout) {
		t.Fatalf("err = %v, want ErrStoreTimeout", err)
	}
}

func TestLoadTimeoutStaysBetweenMySQLAndTheRequestDeadline(t *testing.T) {
	if loadTimeout <= 2*time.Second || loadTimeout >= 5*time.Second {
		t.Fatalf("loadTimeout = %v, want between MySQL's 2s and the request's 5s", loadTimeout)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(time.Millisecond)
	}
}
