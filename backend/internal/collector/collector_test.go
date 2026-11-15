package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/storage"
)

func newCollector(t *testing.T) (*Collector, *storage.Store) {
	t.Helper()
	store, err := storage.New(t.TempDir(), storage.Options{CompactEvery: 1})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return New(store), store
}

func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", message)
}

func TestNormalizeInterval(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want time.Duration
	}{
		{0, DefaultInterval},
		{time.Second, MinInterval},
		{30 * time.Second, 30 * time.Second},
		{2 * time.Hour, MaxInterval},
	}
	for _, item := range cases {
		if got := NormalizeInterval(item.in); got != item.want {
			t.Fatalf("NormalizeInterval(%v) = %v, want %v", item.in, got, item.want)
		}
	}
}

func TestStartCollectsImmediatelyAndPersists(t *testing.T) {
	coll, store := newCollector(t)
	calls := make(chan int, 8)
	fetch := func(ctx context.Context) (int64, []byte, error) {
		calls <- 1
		return time.Now().UnixMilli(), []byte(`{"cluster":{"status":"green"}}`), nil
	}

	status, err := coll.Start("conn-1", 0, fetch)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !status.Running || status.IntervalSec != int(DefaultInterval/time.Second) {
		t.Fatalf("unexpected initial status: %+v", status)
	}
	defer coll.Stop("conn-1")

	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("collection must start immediately, not wait for the first tick")
	}

	waitFor(t, func() bool {
		stats, err := store.Stats(storage.Key("conn-1"))
		return err == nil && stats.Samples >= 1
	}, "sample should be persisted")

	got, err := coll.StatusFor("conn-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Collected < 1 || got.LastAt == 0 || got.Stored < 1 {
		t.Fatalf("status not updated after first collection: %+v", got)
	}
	if got.LastError != "" {
		t.Fatalf("unexpected error on status: %s", got.LastError)
	}
	if got.File == "" {
		t.Fatal("status should expose the series file name")
	}
}

func TestStopStopsCollecting(t *testing.T) {
	coll, _ := newCollector(t)
	count := make(chan int, 32)
	fetch := func(ctx context.Context) (int64, []byte, error) {
		count <- 1
		return time.Now().UnixMilli(), []byte(`{}`), nil
	}
	if _, err := coll.Start("conn-1", MinInterval, fetch); err != nil {
		t.Fatal(err)
	}
	select {
	case <-count:
	case <-time.After(2 * time.Second):
		t.Fatal("no immediate collection")
	}
	if !coll.Stop("conn-1") {
		t.Fatal("Stop should report a running collector")
	}
	// Drain whatever raced in, then assert nothing new arrives.
	time.Sleep(50 * time.Millisecond)
	for len(count) > 0 {
		<-count
	}
	time.Sleep(120 * time.Millisecond)
	if len(count) != 0 {
		t.Fatalf("collector kept running after Stop (%d extra collections)", len(count))
	}
	status, err := coll.StatusFor("conn-1")
	if err != nil {
		t.Fatal(err)
	}
	if status.Running {
		t.Fatalf("status still running after Stop: %+v", status)
	}
}

func TestFetchErrorIsRecordedAndDoesNotStopTheLoop(t *testing.T) {
	coll, _ := newCollector(t)
	attempts := 0
	fetch := func(ctx context.Context) (int64, []byte, error) {
		attempts++
		return 0, nil, errors.New("cluster is asleep")
	}
	if _, err := coll.Start("conn-1", MinInterval, fetch); err != nil {
		t.Fatal(err)
	}
	defer coll.Stop("conn-1")

	waitFor(t, func() bool {
		status, _ := coll.StatusFor("conn-1")
		return status.LastError != "" && status.Collected >= 1
	}, "fetch error should be surfaced in status")

	status, _ := coll.StatusFor("conn-1")
	if status.Running != true {
		t.Fatalf("collector must stay running after a transient error: %+v", status)
	}
}

func TestSampleNowWritesAndUpdatesRunningCollector(t *testing.T) {
	coll, store := newCollector(t)
	fetch := func(ctx context.Context) (int64, []byte, error) {
		return time.Now().UnixMilli(), []byte(`{"manual":true}`), nil
	}
	if _, err := coll.Start("conn-1", time.Hour, fetch); err != nil {
		t.Fatal(err)
	}
	defer coll.Stop("conn-1")
	waitFor(t, func() bool {
		stats, _ := store.Stats(storage.Key("conn-1"))
		return stats.Samples >= 1
	}, "initial sample")

	before, _ := coll.StatusFor("conn-1")
	if _, err := coll.SampleNow(context.Background(), "conn-1", fetch); err != nil {
		t.Fatalf("SampleNow: %v", err)
	}
	after, _ := coll.StatusFor("conn-1")
	if after.Collected <= before.Collected {
		t.Fatalf("SampleNow should count as a collection: %d -> %d", before.Collected, after.Collected)
	}
	stats, _ := store.Stats(storage.Key("conn-1"))
	if stats.Samples < 2 {
		t.Fatalf("SampleNow should persist a sample, stored=%d", stats.Samples)
	}
}

func TestSampleNowSurfacesFetchErrors(t *testing.T) {
	coll, _ := newCollector(t)
	_, err := coll.SampleNow(context.Background(), "conn-1",
		func(ctx context.Context) (int64, []byte, error) { return 0, nil, errors.New("boom") })
	if err == nil {
		t.Fatal("SampleNow must return the fetch error")
	}
}

func TestStartRequiresConnectionID(t *testing.T) {
	coll, _ := newCollector(t)
	if _, err := coll.Start("", 0, func(ctx context.Context) (int64, []byte, error) { return 0, []byte(`{}`), nil }); err == nil {
		t.Fatal("empty connection id must be rejected")
	}
}
