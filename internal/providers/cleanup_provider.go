package providers

import (
	"context"
	"log/slog"

	"github.com/bulutaysarac/pastebin/environments"
	"github.com/bulutaysarac/pastebin/internal/cleanup"
	"github.com/bulutaysarac/pastebin/internal/mysqlstore"
)

// StartCleanup deletes expired pastes now and then every CLEANUP_INTERVAL, in
// the background. The returned stop waits for a batch under way to finish, so
// call it before closing the DB.
func StartCleanup(db *mysqlstore.Store) (stop func()) {
	interval := environments.GetApp().CleanupInterval

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		cleanup.New(db, slog.Default()).Every(ctx, interval)
	}()
	slog.Info("cleanup started", "interval", interval, "batch_size", cleanup.BatchSize)

	return func() {
		cancel()
		<-done
		slog.Info("cleanup stopped")
	}
}
