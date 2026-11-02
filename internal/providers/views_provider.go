package providers

import (
	"context"
	"log/slog"

	"github.com/bulutaysarac/pastebin/environments"
	"github.com/bulutaysarac/pastebin/internal/mysqlstore"
	"github.com/bulutaysarac/pastebin/internal/views"
)

// StartViews counts reads and writes the counts every VIEWS_INTERVAL, in the
// background. The returned stop writes them once more and waits for that, so
// call it after the server has stopped and before closing the DB.
func StartViews(db *mysqlstore.Store) (*views.Counter, func()) {
	interval := environments.GetApp().ViewsInterval
	counter := views.New(db, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		counter.Every(ctx, interval)
	}()
	slog.Info("view counts started", "interval", interval)

	return counter, func() {
		cancel()
		<-done
		slog.Info("view counts stopped")
	}
}
