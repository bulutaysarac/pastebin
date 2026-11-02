package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const (
	perMinute = 30 // one token every 2s
	burst     = 10
)

func newLimiter(t *testing.T) (*Redis, *miniredis.Miniredis, *time.Time) {
	t.Helper()
	mr := miniredis.RunT(t)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	mr.SetTime(now)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1, DialerRetries: 1})
	t.Cleanup(func() { rdb.Close() })
	return New(rdb), mr, &now
}

func allowed(t *testing.T, l *Redis, id string) bool {
	t.Helper()
	ok, err := l.Allow(context.Background(), id, perMinute, burst)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestABurstThenRefused(t *testing.T) {
	l, _, _ := newLimiter(t)
	for i := range burst {
		if !allowed(t, l, "203.0.113.7") {
			t.Fatalf("request %d of the burst was refused", i+1)
		}
	}
	if allowed(t, l, "203.0.113.7") {
		t.Fatal("the request after the burst got through")
	}
}

func TestTokensComeBackAtTheRate(t *testing.T) {
	l, mr, now := newLimiter(t)
	for range burst {
		allowed(t, l, "203.0.113.7")
	}

	mr.SetTime(now.Add(1900 * time.Millisecond))
	if allowed(t, l, "203.0.113.7") {
		t.Fatal("a token came back before 2s")
	}
	mr.SetTime(now.Add(2100 * time.Millisecond))
	if !allowed(t, l, "203.0.113.7") {
		t.Fatal("no token after 2s")
	}
	if allowed(t, l, "203.0.113.7") {
		t.Fatal("two tokens came back after 2s")
	}
}

func TestARefusedRequestDoesNotPushTheRefillBack(t *testing.T) {
	l, mr, now := newLimiter(t)
	for range burst {
		allowed(t, l, "203.0.113.7")
	}
	for _, at := range []time.Duration{500, 1000, 1500} {
		mr.SetTime(now.Add(at * time.Millisecond))
		allowed(t, l, "203.0.113.7")
	}
	mr.SetTime(now.Add(2100 * time.Millisecond))
	if !allowed(t, l, "203.0.113.7") {
		t.Fatal("refused requests delayed the next token")
	}
}

func TestTheBucketNeverHoldsMoreThanTheBurst(t *testing.T) {
	l, mr, now := newLimiter(t)
	allowed(t, l, "203.0.113.7")
	mr.SetTime(now.Add(time.Minute))
	for i := range burst {
		if !allowed(t, l, "203.0.113.7") {
			t.Fatalf("request %d was refused", i+1)
		}
	}
	if allowed(t, l, "203.0.113.7") {
		t.Fatal("more than the burst got through")
	}
}

func TestClientsHaveTheirOwnBuckets(t *testing.T) {
	l, _, _ := newLimiter(t)
	for range burst + 1 {
		allowed(t, l, "203.0.113.7")
	}
	if !allowed(t, l, "203.0.113.8") {
		t.Fatal("another client was refused")
	}
}

func TestTheKeyAlwaysHasAnExpiry(t *testing.T) {
	l, mr, _ := newLimiter(t)
	key := keyPrefix + "203.0.113.7"

	allowed(t, l, "203.0.113.7")
	full := burst * time.Minute / perMinute
	if ttl := mr.TTL(key); ttl != full {
		t.Fatalf("ttl = %v, want %v, when a bucket is full again", ttl, full)
	}

	// The key expires between two calls and comes back with an expiry.
	mr.FastForward(full + time.Second)
	if mr.Exists(key) {
		t.Fatal("the key should have expired")
	}
	if !allowed(t, l, "203.0.113.7") {
		t.Fatal("a forgotten client should start with a full bucket")
	}
	if ttl := mr.TTL(key); ttl <= 0 {
		t.Fatalf("ttl = %v after the key came back: it would never expire", ttl)
	}
}

func TestRedisDownIsAnError(t *testing.T) {
	l, mr, _ := newLimiter(t)
	mr.Close()
	if _, err := l.Allow(context.Background(), "203.0.113.7", perMinute, burst); err == nil {
		t.Fatal("want an error, so the caller can fall back")
	}
}
