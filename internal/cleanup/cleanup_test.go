package cleanup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type call struct {
	now    time.Time
	limit  int
	ctxErr error
}

// fakeStore holds a number of expired pastes and deletes them like the real
// store: up to limit per call.
type fakeStore struct {
	expired int64
	calls   []call
	failOn  int         // the call (1, 2, ...) that returns errBoom; 0 = none
	onCall  func(n int) // runs during call n, before it looks at ctx
}

var errBoom = errors.New("boom")

func (f *fakeStore) DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	if f.onCall != nil {
		f.onCall(len(f.calls) + 1)
	}
	f.calls = append(f.calls, call{now: now, limit: limit, ctxErr: ctx.Err()})
	if len(f.calls) == f.failOn {
		return 0, errBoom
	}
	n := min(f.expired, int64(limit))
	f.expired -= n
	return n, nil
}

var t0 = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

// newTestJob returns a job whose clock moves a second every time it is read.
func newTestJob(store Store) *Job {
	j := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tick := t0
	j.now = func() time.Time { tick = tick.Add(time.Second); return tick }
	return j
}

func TestRunDeletesInBatchesFromOneInstant(t *testing.T) {
	store := &fakeStore{expired: 2500}
	deleted, err := newTestJob(store).Run(context.Background())
	if err != nil || deleted != 2500 {
		t.Fatalf("Run = %d, %v; want 2500, nil", deleted, err)
	}
	if len(store.calls) != 3 {
		t.Fatalf("%d batches, want 3: two full and one short", len(store.calls))
	}
	for i, c := range store.calls {
		if c.limit != BatchSize {
			t.Errorf("batch %d: limit %d, want %d", i+1, c.limit, BatchSize)
		}
		if !c.now.Equal(store.calls[0].now) {
			t.Errorf("batch %d: now %v, want %v like the first batch", i+1, c.now, store.calls[0].now)
		}
	}
}

func TestRunEndsOnAnEmptyBatchAfterFullOnes(t *testing.T) {
	store := &fakeStore{expired: 2 * BatchSize}
	deleted, err := newTestJob(store).Run(context.Background())
	if err != nil || deleted != 2*BatchSize || len(store.calls) != 3 {
		t.Fatalf("Run = %d, %v in %d batches; want %d, nil in 3", deleted, err, len(store.calls), 2*BatchSize)
	}
}

func TestRunStopsOnAnError(t *testing.T) {
	store := &fakeStore{expired: 5 * BatchSize, failOn: 2}
	deleted, err := newTestJob(store).Run(context.Background())
	if !errors.Is(err, errBoom) || deleted != BatchSize || len(store.calls) != 2 {
		t.Fatalf("Run = %d, %v in %d batches; want %d, errBoom in 2", deleted, err, len(store.calls), BatchSize)
	}
}

func TestRunFinishesTheBatchUnderWayWhenStopped(t *testing.T) {
	stop, cancel := context.WithCancel(context.Background())
	store := &fakeStore{expired: 5 * BatchSize, onCall: func(int) { cancel() }}

	deleted, err := newTestJob(store).Run(stop)
	if err != nil || deleted != BatchSize || len(store.calls) != 1 {
		t.Fatalf("Run = %d, %v in %d batches; want %d, nil in 1", deleted, err, len(store.calls), BatchSize)
	}
	if store.calls[0].ctxErr != nil {
		t.Fatalf("the batch under way saw %v; stopping must not cancel it", store.calls[0].ctxErr)
	}
}

func TestEveryRunsAtOnceAndReturnsWhenStopped(t *testing.T) {
	stop, cancel := context.WithCancel(context.Background())
	store := &fakeStore{expired: 10, onCall: func(int) { cancel() }}

	done := make(chan struct{})
	go func() {
		newTestJob(store).Every(stop, time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Every did not return after stop")
	}
	if len(store.calls) != 1 || store.expired != 0 {
		t.Fatalf("%d batches, %d left; want one run at start that deleted all 10", len(store.calls), store.expired)
	}
}
