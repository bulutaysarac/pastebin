// Package views counts reads per paste. A read only adds to a counter in
// memory, and the counts go to the store in batches, so reading a paste never
// waits on a row lock. A crash loses the counts not written yet.
package views

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/bulutaysarac/pastebin/internal/metrics"
)

// Count is how many reads to add to one paste.
type Count struct {
	ID string
	N  int64
}

// Store writes counts. mysqlstore.Store implements it.
type Store interface {
	// AddViews adds the counts to their pastes. Ids that no longer exist are
	// ignored: the paste expired and was deleted.
	AddViews(ctx context.Context, batch []Count) error
}

const (
	// batchSize is the most pastes one write touches, so it holds few locks and
	// only until it commits.
	batchSize = 500

	// maxPending bounds the memory counts can take while the store is down.
	maxPending = 100_000

	// flushTimeout bounds one whole flush, the one at shutdown included.
	flushTimeout = 5 * time.Second
)

type Counter struct {
	store      Store
	log        *slog.Logger
	maxPending int

	mu      sync.Mutex
	pending map[string]int64
}

func New(store Store, log *slog.Logger) *Counter {
	return &Counter{store: store, log: log, maxPending: maxPending, pending: map[string]int64{}}
}

// Record counts one read of id.
func (c *Counter) Record(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.add(id, 1)
}

// add must be called with mu held.
func (c *Counter) add(id string, n int64) {
	if _, ok := c.pending[id]; !ok && len(c.pending) >= c.maxPending {
		metrics.ObserveViewsDropped(n)
		return
	}
	c.pending[id] += n
}

// Every writes the counts every interval until stop ends, and once more then.
func (c *Counter) Every(stop context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop.Done():
			c.flushAndReport()
			return
		case <-ticker.C:
			c.flushAndReport()
		}
	}
}

func (c *Counter) flushAndReport() {
	// Not derived from the shutdown signal: the last flush happens after it.
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	written, err := c.Flush(ctx)
	metrics.ObserveViewsFlush(written, err != nil)
	if err != nil {
		c.log.Error("views flush failed, will retry", "written", written, "err", err)
	}
}

// Flush writes the pending counts and returns how many reads it wrote. The
// batches go in id order: two instances writing the same pastes then lock them
// in the same order and can't deadlock. On an error the counts not written
// stay pending for the next flush.
func (c *Counter) Flush(ctx context.Context) (int64, error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[string]int64, len(pending))
	c.mu.Unlock()

	counts := make([]Count, 0, len(pending))
	for id, n := range pending {
		counts = append(counts, Count{ID: id, N: n})
	}
	slices.SortFunc(counts, func(a, b Count) int { return cmp.Compare(a.ID, b.ID) })

	var written int64
	for start := 0; start < len(counts); start += batchSize {
		batch := counts[start:min(start+batchSize, len(counts))]
		if err := c.store.AddViews(ctx, batch); err != nil {
			c.putBack(counts[start:])
			return written, err
		}
		for _, count := range batch {
			written += count.N
		}
	}
	return written, nil
}

func (c *Counter) putBack(counts []Count) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, count := range counts {
		c.add(count.ID, count.N)
	}
}
