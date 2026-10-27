package providers

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/bulutaysarac/pastebin/environments"
	"github.com/bulutaysarac/pastebin/internal/mysqlstore"
)

// ConnectDatabase waits up to a minute for MySQL (it is slow on first start)
// and exits the process if it never answers.
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

	return store
}
