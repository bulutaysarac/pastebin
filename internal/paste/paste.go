// Package paste holds the core rules: creating a paste under a fresh unique
// ID, and reading one back only while it has not expired.
package paste

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bulutaysarac/pastebin/internal/idgen"
)

var (
	// ErrNotFound is returned for unknown IDs and for pastes that have expired.
	// The reader cannot tell the two apart.
	ErrNotFound = errors.New("paste not found")

	// ErrIDTaken is returned by a Store when the ID is already in use.
	ErrIDTaken = errors.New("paste id already taken")
)

// maxIDAttempts bounds how many fresh IDs Create tries before giving up.
// With 62^8 possible IDs a single retry is already extremely rare.
const maxIDAttempts = 5

type Paste struct {
	ID        string
	Content   string
	Language  string     // highlight.js language name, e.g. "sql" or "plaintext"
	ExpiresAt *time.Time // nil means the paste never expires
}

// Expired reports whether p is past its expiry at the given instant.
func (p Paste) Expired(now time.Time) bool {
	return p.ExpiresAt != nil && !now.Before(*p.ExpiresAt)
}

type Store interface {
	// Create inserts p, returning ErrIDTaken if p.ID already exists.
	Create(ctx context.Context, p Paste) error
	// Get returns the paste with the given ID, or ErrNotFound.
	Get(ctx context.Context, id string) (Paste, error)
}

type Service struct {
	store Store
	now   func() time.Time
	newID func() string
}

func NewService(store Store) *Service {
	return &Service{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		newID: idgen.New,
	}
}

// Create stores content under a new unique ID. A zero ttl means "never expires".
func (s *Service) Create(ctx context.Context, content, language string, ttl time.Duration) (Paste, error) {
	p := Paste{Content: content, Language: language}
	if ttl > 0 {
		// Truncate to whole seconds: that is all a DATETIME column keeps.
		exp := s.now().Add(ttl).Truncate(time.Second)
		p.ExpiresAt = &exp
	}

	for range maxIDAttempts {
		p.ID = s.newID()
		err := s.store.Create(ctx, p)
		if errors.Is(err, ErrIDTaken) {
			continue
		}
		if err != nil {
			return Paste{}, err
		}
		return p, nil
	}
	return Paste{}, fmt.Errorf("no free id after %d attempts", maxIDAttempts)
}

// Get returns a live paste. Expired pastes still sit in the store but are
// reported as ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (Paste, error) {
	p, err := s.store.Get(ctx, id)
	if err != nil {
		return Paste{}, err
	}
	if p.Expired(s.now()) {
		return Paste{}, ErrNotFound
	}
	return p, nil
}

// Now exposes the service clock so HTTP code computes cache lifetimes against
// the same instant used for expiry checks.
func (s *Service) Now() time.Time { return s.now() }
