package paste

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memStore struct {
	pastes    map[string]Paste
	collideFn func(id string) bool // forces ErrIDTaken for matching ids
}

func newMemStore() *memStore { return &memStore{pastes: map[string]Paste{}} }

func (m *memStore) Create(_ context.Context, p Paste) error {
	if _, ok := m.pastes[p.ID]; ok || (m.collideFn != nil && m.collideFn(p.ID)) {
		return ErrIDTaken
	}
	m.pastes[p.ID] = p
	return nil
}

func (m *memStore) Get(_ context.Context, id string) (Paste, error) {
	p, ok := m.pastes[id]
	if !ok {
		return Paste{}, ErrNotFound
	}
	return p, nil
}

func newTestService(store Store, now time.Time, ids ...string) *Service {
	s := NewService(store)
	s.now = func() time.Time { return now }
	if len(ids) > 0 {
		i := 0
		s.newID = func() string { id := ids[i%len(ids)]; i++; return id }
	}
	return s
}

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func TestCreateNeverExpires(t *testing.T) {
	s := newTestService(newMemStore(), t0, "AAAAAAAA")
	p, err := s.Create(context.Background(), "hello", 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "AAAAAAAA" || p.ExpiresAt != nil {
		t.Fatalf("got %+v, want id AAAAAAAA and no expiry", p)
	}
}

func TestCreateWithTTL(t *testing.T) {
	s := newTestService(newMemStore(), t0.Add(500*time.Millisecond), "AAAAAAAA")
	p, err := s.Create(context.Background(), "hello", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	want := t0.Add(time.Hour)
	if p.ExpiresAt == nil || !p.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", p.ExpiresAt, want)
	}
}

func TestCreateRetriesOnCollision(t *testing.T) {
	store := newMemStore()
	store.pastes["TAKEN000"] = Paste{ID: "TAKEN000"}
	s := newTestService(store, t0, "TAKEN000", "FREE0000")

	p, err := s.Create(context.Background(), "hello", 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "FREE0000" {
		t.Fatalf("ID = %q, want the second candidate FREE0000", p.ID)
	}
}

func TestCreateGivesUpAfterMaxAttempts(t *testing.T) {
	store := newMemStore()
	store.collideFn = func(string) bool { return true }
	s := newTestService(store, t0)

	if _, err := s.Create(context.Background(), "hello", 0); err == nil {
		t.Fatal("expected an error when every id collides")
	}
}

func TestGetHidesExpiredPastes(t *testing.T) {
	store := newMemStore()
	exp := t0
	store.pastes["EXPIRED0"] = Paste{ID: "EXPIRED0", Content: "old", ExpiresAt: &exp}

	s := newTestService(store, t0) // now == expires_at counts as expired
	if _, err := s.Get(context.Background(), "EXPIRED0"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	s = newTestService(store, t0.Add(-time.Second))
	if _, err := s.Get(context.Background(), "EXPIRED0"); err != nil {
		t.Fatalf("one second before expiry: err = %v, want nil", err)
	}
}
