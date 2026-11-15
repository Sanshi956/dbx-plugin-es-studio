// Package collector runs the periodic monitoring loop.
//
// One goroutine per connection, started on demand from the UI (a sidecar cannot
// resume collection by itself — connection credentials only exist after DBX
// opens a session). Collection continues while the plugin process is alive,
// even if the user navigates away from the Metrics page.
package collector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/storage"
)

// FetchFunc produces one sample payload. at is the collection timestamp.
type FetchFunc func(ctx context.Context) (at int64, payload []byte, err error)

// Status is the JSON projection the UI polls.
type Status struct {
	ConnectionID string `json:"connection_id"`
	Running      bool   `json:"running"`
	IntervalSec  int    `json:"interval_sec"`
	Collected    int    `json:"collected"`
	LastAt       int64  `json:"last_at"`
	LastError    string `json:"last_error,omitempty"`
	Stored       int    `json:"stored"`
	Bytes        int64  `json:"bytes"`
	File         string `json:"file,omitempty"`
}

// Interval bounds: faster than 5s hammers the cluster, slower than an hour is
// indistinguishable from not collecting.
const (
	MinInterval     = 5 * time.Second
	MaxInterval     = time.Hour
	DefaultInterval = 60 * time.Second
)

type runner struct {
	fetch     FetchFunc
	interval  time.Duration
	cancel    context.CancelFunc
	collected int
	lastAt    int64
	lastErr   string
}

// Collector owns the goroutines. Safe for concurrent use.
type Collector struct {
	store *storage.Store
	mu    sync.Mutex
	runs  map[string]*runner
}

// New returns a collector writing into store. A nil store disables persistence
// but keeps the loop usable (used by tests that only care about timing).
func New(store *storage.Store) *Collector {
	return &Collector{store: store, runs: map[string]*runner{}}
}

// NormalizeInterval clamps to the supported range; zero means "default".
func NormalizeInterval(value time.Duration) time.Duration {
	if value <= 0 {
		return DefaultInterval
	}
	if value < MinInterval {
		return MinInterval
	}
	if value > MaxInterval {
		return MaxInterval
	}
	return value
}

// Start (re)starts collection for one connection and collects immediately so
// the page has a data point before the first tick.
func (c *Collector) Start(connID string, interval time.Duration, fetch FetchFunc) (Status, error) {
	if connID == "" {
		return Status{}, errors.New("monitor: connection id is required")
	}
	if fetch == nil {
		return Status{}, errors.New("monitor: fetch function is required")
	}
	interval = NormalizeInterval(interval)

	ctx, cancel := context.WithCancel(context.Background())
	next := &runner{fetch: fetch, interval: interval, cancel: cancel}

	c.mu.Lock()
	previous := c.runs[connID]
	c.runs[connID] = next
	c.mu.Unlock()
	if previous != nil {
		previous.cancel()
	}

	go c.loop(ctx, connID, next)
	return c.StatusFor(connID)
}

// Stop cancels collection; samples already on disk are kept.
func (c *Collector) Stop(connID string) bool {
	c.mu.Lock()
	run, ok := c.runs[connID]
	delete(c.runs, connID)
	c.mu.Unlock()
	if ok {
		run.cancel()
	}
	return ok
}

// StopAll is used when the sidecar shuts down.
func (c *Collector) StopAll() {
	c.mu.Lock()
	runs := c.runs
	c.runs = map[string]*runner{}
	c.mu.Unlock()
	for _, run := range runs {
		run.cancel()
	}
}

// SampleNow collects one point outside the schedule (the "collect now" button).
func (c *Collector) SampleNow(ctx context.Context, connID string, fetch FetchFunc) (Status, error) {
	if fetch == nil {
		return Status{}, errors.New("monitor: fetch function is required")
	}
	at, payload, err := fetch(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("monitor: collection failed: %w", err)
	}
	if c.store != nil {
		if err := c.store.Append(storage.Key(connID), at, payload); err != nil {
			return Status{}, err
		}
	}
	c.mu.Lock()
	if run, ok := c.runs[connID]; ok {
		run.collected++
		run.lastAt = at
		run.lastErr = ""
	}
	c.mu.Unlock()
	return c.StatusFor(connID)
}

// StatusFor returns one collector's state (running=false when stopped).
func (c *Collector) StatusFor(connID string) (Status, error) {
	c.mu.Lock()
	run, ok := c.runs[connID]
	status := Status{ConnectionID: connID, IntervalSec: int(DefaultInterval / time.Second)}
	if ok {
		status.Running = true
		status.IntervalSec = int(run.interval / time.Second)
		status.Collected = run.collected
		status.LastAt = run.lastAt
		status.LastError = run.lastErr
	}
	c.mu.Unlock()

	if c.store != nil {
		stats, err := c.store.Stats(storage.Key(connID))
		if err != nil {
			return status, err
		}
		status.Stored = stats.Samples
		status.Bytes = stats.Bytes
		status.File = stats.File
		if status.LastAt == 0 {
			status.LastAt = stats.LastAt
		}
	}
	return status, nil
}

// Status lists every known connection, sorted for stable UI ordering.
func (c *Collector) Status() []Status {
	c.mu.Lock()
	ids := make([]string, 0, len(c.runs))
	for id := range c.runs {
		ids = append(ids, id)
	}
	c.mu.Unlock()
	sort.Strings(ids)

	out := make([]Status, 0, len(ids))
	for _, id := range ids {
		status, err := c.StatusFor(id)
		if err != nil {
			status.LastError = err.Error()
		}
		out = append(out, status)
	}
	return out
}

func (c *Collector) loop(ctx context.Context, connID string, run *runner) {
	c.collectOnce(ctx, connID, run)
	ticker := time.NewTicker(run.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectOnce(ctx, connID, run)
		}
	}
}

func (c *Collector) collectOnce(ctx context.Context, connID string, run *runner) {
	at, payload, fetchErr := run.fetch(ctx)
	var storeErr error
	if fetchErr == nil && c.store != nil {
		storeErr = c.store.Append(storage.Key(connID), at, payload)
	}

	c.mu.Lock()
	run.collected++
	switch {
	case fetchErr != nil:
		// Keep collecting: a transient cluster error must not end the series.
		run.lastErr = fetchErr.Error()
	case storeErr != nil:
		run.lastErr = storeErr.Error()
		run.lastAt = at
	default:
		run.lastErr = ""
		run.lastAt = at
	}
	c.mu.Unlock()
}
