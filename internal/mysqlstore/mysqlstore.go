// Package mysqlstore is the MySQL implementation of paste.Store.
package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/bulutaysarac/pastebin/internal/paste"
)

// MySQL error numbers the store translates.
const (
	errDuplicateEntry  = 1062 // ER_DUP_ENTRY: a primary/unique key violation
	errLockWaitTimeout = 1205 // ER_LOCK_WAIT_TIMEOUT: lock_wait_timeout or innodb_lock_wait_timeout
	errQueryTimeout    = 3024 // ER_QUERY_TIMEOUT: max_execution_time
)

// StatementTimeout is how long MySQL runs one statement, or waits for a lock
// for it, before giving up. It must stay under the request deadline:
// cancelling a request only closes the app's end of the connection, and
// MySQL would keep waiting on its end, holding one of its max_connections.
const StatementTimeout = 2 * time.Second

type Store struct {
	db *sql.DB
}

// Pool sizes the connection pool. Every field must be positive: database/sql
// reads a zero as "unlimited", "keep none" and "forever".
type Pool struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// Open connects to MySQL and keeps pinging until the server answers or ctx ends.
// On first start the database container takes a while to initialise, so the
// first few pings are expected to fail.
func Open(ctx context.Context, dsn string, pool Pool) (*Store, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	// Scan DATETIME straight into time.Time, and read/write it as UTC.
	cfg.ParseTime = true
	cfg.Loc = time.UTC

	// Sent as SET on every new connection.
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	secs := strconv.Itoa(int(StatementTimeout / time.Second))
	cfg.Params["max_execution_time"] = strconv.FormatInt(StatementTimeout.Milliseconds(), 10) // SELECTs, in ms
	cfg.Params["lock_wait_timeout"] = secs                                                    // table and metadata locks
	cfg.Params["innodb_lock_wait_timeout"] = secs                                             // row locks

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)

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

// Ping checks that MySQL answers, using a connection from the pool.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// DB exposes the connection pool so its stats can be reported.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Create(ctx context.Context, p paste.Paste) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO pastes (id, content, language, expires_at) VALUES (?, ?, ?, ?)",
		p.ID, p.Content, p.Language, p.ExpiresAt)
	return translate(err)
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
		return paste.Paste{}, translate(err)
	}
	if exp.Valid {
		t := exp.Time
		p.ExpiresAt = &t
	}
	return p, nil
}

// translate turns the MySQL errors paste.Store names into its own.
func translate(err error) error {
	var myErr *mysql.MySQLError
	if !errors.As(err, &myErr) {
		return err
	}
	switch myErr.Number {
	case errDuplicateEntry:
		return paste.ErrIDTaken
	case errLockWaitTimeout, errQueryTimeout:
		return fmt.Errorf("%w: %w", paste.ErrStoreTimeout, err)
	}
	return err
}
