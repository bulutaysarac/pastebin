package providers

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/bulutaysarac/pastebin/environments"
	"github.com/bulutaysarac/pastebin/internal/mysqlstore"
	"github.com/bulutaysarac/pastebin/migrations"
)

// ConnectDatabase waits up to a minute for MySQL (it is slow on first start)
// and exits the process if it never answers. It then applies the pending
// migrations.
func ConnectDatabase() *mysqlstore.Store {
	db := environments.GetDatabase()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	store, err := mysqlstore.Open(ctx, db.DSN(), mysqlstore.Pool{
		MaxOpenConns:    db.MaxOpenConns,
		MaxIdleConns:    db.MaxIdleConns,
		ConnMaxLifetime: db.ConnMaxLifetime,
	})
	if err != nil {
		slog.Error("database connection failed", "host", db.Host, "err", err)
		os.Exit(1)
	}

	slog.Info("database connected", "host", db.Host, "database", db.Name,
		"max_open_conns", db.MaxOpenConns, "max_idle_conns", db.MaxIdleConns, "conn_max_lifetime", db.ConnMaxLifetime)

	migrate(store)
	return store
}

// migrateTimeout bounds the whole run of pending migrations. A failed run
// exits the process, and Compose restarts the app to try again.
const migrateTimeout = 5 * time.Minute

// migrate brings the schema up to date before anything is served, so no
// request reads a column that isn't there yet.
func migrate(store *mysqlstore.Store) {
	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	start := time.Now()
	version, applied, err := store.Migrate(ctx, migrations.FS)
	for _, m := range applied {
		slog.Info("migration applied", "version", m.Version, "file", m.File, "duration", m.Duration)
	}
	if err != nil {
		slog.Error("migration failed", "err", err)
		os.Exit(1)
	}
	slog.Info("schema up to date", "version", version, "applied", len(applied), "duration", time.Since(start))
}
