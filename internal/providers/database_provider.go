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

	store, err := mysqlstore.Open(ctx, db.DSN())
	if err != nil {
		slog.Error("database connection failed", "host", db.Host, "err", err)
		os.Exit(1)
	}

	slog.Info("database connected", "host", db.Host, "database", db.Name)

	return store
}
