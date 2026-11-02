package views

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu      sync.Mutex
	batches [][]Count
	totals  map[string]int64
	failOn  int // the 1-based write that fails; 0 = none
	writes  int
	ctxErr  error // the context's state at the last write
}

func (f *fakeStore) AddViews(ctx context.Context, batch []Count) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	f.ctxErr = ctx.Err()
	if f.writes == f.failOn {
		return errors.New("database down")
	}
	f.batches = append(f.batches, batch)
	if f.totals == nil {
		f.totals = map[string]int64{}
	}
	for _, c := range batch {
		f.totals[c.ID] += c.N
	}
	return nil
}

func (f *fakeStore) total(id string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.totals[id]
}

func newCounter(store Store) *Counter {
	return New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestFlushWritesTheCountsOnce(t *testing.T) {
	store := &fakeStore{}
	c := newCounter(store)
	c.Record("aaaaaaaa")
	c.Record("aaaaaaaa")
	c.Record("bbbbbbbb")

	written, err := c.Flush(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if written != 3 {
		t.Fatalf("written = %d, want 3", written)
	}
	if store.total("aaaaaaaa") != 2 || store.total("bbbbbbbb") != 1 {
		t.Fatalf("totals = %v", store.totals)
	}

	if written, err := c.Flush(context.Background()); err != nil || written != 0 {
		t.Fatalf("second flush: written %d, err %v, want nothing", written, err)
	}
	if store.total("aaaaaaaa") != 2 {
		t.Fatalf("counted twice: %v", store.totals)
	}
}

func TestFlushWithNothingPendingDoesNotWrite(t *testing.T) {
	store := &fakeStore{}
	if _, err := newCounter(store).Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.writes != 0 {
		t.Fatalf("%d writes, want none", store.writes)
	}
}

func TestFlushWritesBatchesInIDOrder(t *testing.T) {
	store := &fakeStore{}
	c := newCounter(store)
	const pastes = 2*batchSize + 1
	for i := range pastes {
		c.Record(fmt.Sprintf("%08d", pastes-i))
	}

	if _, err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.batches) != 3 {
		t.Fatalf("%d batches, want 3", len(store.batches))
	}
	last := ""
	for _, batch := range store.batches {
		if len(batch) > batchSize {
			t.Fatalf("batch of %d, want at most %d", len(batch), batchSize)
		}
		for _, count := range batch {
			if count.ID <= last {
				t.Fatalf("%s came after %s: not in order", count.ID, last)
			}
			last = count.ID
		}
	}
}

func TestAFailedWriteKeepsOnlyWhatWasNotWritten(t *testing.T) {
	store := &fakeStore{failOn: 2}
	c := newCounter(store)
	const pastes = batchSize + 10
	for i := range pastes {
		c.Record(fmt.Sprintf("%08d", i))
	}

	written, err := c.Flush(context.Background())
	if err == nil {
		t.Fatal("want the write's error")
	}
	if written != batchSize {
		t.Fatalf("written = %d, want %d", written, batchSize)
	}
	c.Record(fmt.Sprintf("%08d", pastes-1)) // a read arrives before the retry

	if _, err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := range pastes {
		want := int64(1)
		if i == pastes-1 {
			want = 2
		}
		if got := store.total(fmt.Sprintf("%08d", i)); got != want {
			t.Fatalf("paste %d counted %d times, want %d", i, got, want)
		}
	}
}

func TestPendingIsBoundedButKnownPastesStillCount(t *testing.T) {
	store := &fakeStore{}
	c := newCounter(store)
	c.maxPending = 2
	c.Record("aaaaaaaa")
	c.Record("bbbbbbbb")
	c.Record("cccccccc") // no room
	c.Record("aaaaaaaa") // already has a counter

	if _, err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.total("aaaaaaaa") != 2 || store.total("bbbbbbbb") != 1 || store.total("cccccccc") != 0 {
		t.Fatalf("totals = %v", store.totals)
	}
}

func TestEveryWritesOnTheIntervalAndOnceMoreWhenStopped(t *testing.T) {
	store := &fakeStore{}
	c := newCounter(store)
	stop, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Every(stop, 5*time.Millisecond)
	}()

	c.Record("aaaaaaaa")
	deadline := time.Now().Add(2 * time.Second)
	for store.total("aaaaaaaa") != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the count wasn't written on the interval")
		}
		time.Sleep(time.Millisecond)
	}

	c.Record("bbbbbbbb")
	cancel()
	<-done
	if store.total("bbbbbbbb") != 1 {
		t.Fatal("the count wasn't written at shutdown")
	}
	if store.ctxErr != nil {
		t.Fatalf("the last write got an ended context: %v", store.ctxErr)
	}
}

func TestNoReadIsLostUnderConcurrency(t *testing.T) {
	store := &fakeStore{failOn: 3}
	c := newCounter(store)

	const readers, reads = 20, 500
	var wg sync.WaitGroup
	for range readers {
		wg.Go(func() {
			for range reads {
				c.Record("aaaaaaaa")
			}
		})
	}
	stopFlushing := make(chan struct{})
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		for {
			select {
			case <-stopFlushing:
				return
			default:
				_, _ = c.Flush(context.Background())
			}
		}
	}()
	wg.Wait()
	close(stopFlushing)
	<-flushed

	if _, err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.total("aaaaaaaa"); got != readers*reads {
		t.Fatalf("counted %d reads, want %d", got, readers*reads)
	}
}
