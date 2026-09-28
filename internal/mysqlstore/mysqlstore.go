// Package mysqlstore is the MySQL implementation of paste.Store.
package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/bulutaysarac/pastebin/internal/paste"
)

// MySQL error 1062: ER_DUP_ENTRY, a primary/unique key violation.
const errDuplicateEntry = 1062

type Store struct {
	db *sql.DB
}

// Open connects to MySQL and keeps pinging until the server answers or ctx ends.
// On first start the database container takes a while to initialise, so the
// first few pings are expected to fail.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	// Scan DATETIME straight into time.Time, and read/write it as UTC.
	cfg.ParseTime = true
	cfg.Loc = time.UTC

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)

	for {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = db.PingContext(pingCtx)
		cancel()
		if err == nil {
			return &Store{db: db}, nil
		}
		select {
		case <-ctx.Done():
			db.Close()
			return nil, fmt.Errorf("mysql not reachable: %w", err)
		case <-time.After(time.Second):
		}
	}
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Create(ctx context.Context, p paste.Paste) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO pastes (id, content, language, expires_at) VALUES (?, ?, ?, ?)",
		p.ID, p.Content, p.Language, p.ExpiresAt)

	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) && myErr.Number == errDuplicateEntry {
		return paste.ErrIDTaken
	}
	return err
}

func (s *Store) Get(ctx context.Context, id string) (paste.Paste, error) {
	var (
		p   paste.Paste
		exp sql.NullTime
	)
	err := s.db.QueryRowContext(ctx,
		"SELECT id, content, language, expires_at FROM pastes WHERE id = ?", id,
	).Scan(&p.ID, &p.Content, &p.Language, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return paste.Paste{}, paste.ErrNotFound
	}
	if err != nil {
		return paste.Paste{}, err
	}
	if exp.Valid {
		t := exp.Time
		p.ExpiresAt = &t
	}
	return p, nil
}
