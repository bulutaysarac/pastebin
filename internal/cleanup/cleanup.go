// Package cleanup deletes expired pastes in the background. Readers already
// get a 404 for them; this is what frees their rows.
package cleanup

import (
	"context"
	"log/slog"
	"time"

	"github.com/bulutaysarac/pastebin/internal/metrics"
)

// Store deletes expired pastes. mysqlstore.Store implements it.
type Store interface {
	// DeleteExpired deletes up to limit pastes that expired at or before now,
	// oldest first, and returns how many it deleted.
	DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error)
	// LockCleanup takes the lock that lets one instance run at a time, without
	// waiting. ok is false if another instance holds it. When ok, the caller
	// must call unlock.
	LockCleanup(ctx context.Context) (unlock func(), ok bool, err error)
}

// BatchSize is the most pastes one DELETE removes. Each one holds its locks
// only until it commits, so writes never wait for a whole run.
const BatchSize = 1000

// batchTimeout bounds one DELETE, so a stalled connection can't stop the job
// for good. Waiting for a lock already gives up after 2s, on MySQL's side.
const batchTimeout = 5 * time.Second

type Job struct {
	store Store
	now   func() time.Time
	log   *slog.Logger
}

func New(store Store, log *slog.Logger) *Job {
	return &Job{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		log:   log,
	}
}

// Every runs the job now and then every interval, until stop ends.
func (j *Job) Every(stop context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		j.runAndReport(stop)
		select {
		case <-stop.Done():
			return
		case <-ticker.C:
		}
	}
}

func (j *Job) runAndReport(stop context.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), batchTimeout)
	unlock, ok, err := j.store.LockCleanup(ctx)
	cancel()
	if err != nil {
		metrics.ObserveCleanup(0, true)
		j.log.Error("cleanup failed", "deleted", 0, "err", err)
		return
	}
	if !ok {
		return // another instance is running it, which is fine
	}
	defer unlock()

	start := time.Now()
	deleted, err := j.Run(stop)
	metrics.ObserveCleanup(deleted, err != nil)
	if err != nil {
		j.log.Error("cleanup failed", "deleted", deleted, "err", err)
		return
	}
	if deleted > 0 {
		j.log.Info("cleanup", "deleted", deleted, "duration", time.Since(start))
	}
}

// Run deletes, a batch at a time, the pastes that had expired when it started,
// and returns how many it deleted. It ends after a batch that comes back
// short, on an error, or between batches once stop ends. Pastes that expire
// while it runs are left for the next run, so a run always ends.
func (j *Job) Run(stop context.Context) (int64, error) {
	now := j.now()
	var deleted int64
	for {
		// Not derived from stop: on shutdown the batch under way finishes
		// instead of failing halfway.
		ctx, cancel := context.WithTimeout(context.Background(), batchTimeout)
		n, err := j.store.DeleteExpired(ctx, now, BatchSize)
		cancel()
		deleted += n
		if err != nil || n < BatchSize || stop.Err() != nil {
			return deleted, err
		}
	}
}
