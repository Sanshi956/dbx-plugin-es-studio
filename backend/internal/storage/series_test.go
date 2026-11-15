package storage

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T, opts Options) *Store {
	t.Helper()
	store, err := New(t.TempDir(), opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

func TestAppendReadKeepsOrderAndLimit(t *testing.T) {
	store := newStore(t, Options{MaxSamples: 100, CompactEvery: 1000})
	key := Key("conn-1")
	for i := 1; i <= 5; i++ {
		payload := fmt.Sprintf(`{"cluster":{"nodes":%d}}`, i)
		if err := store.Append(key, int64(1000*i), []byte(payload)); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	all, err := store.Read(key, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("want 5 samples, got %d", len(all))
	}
	for i, record := range all {
		if record.At != int64(1000*(i+1)) {
			t.Fatalf("sample %d has at=%d, want %d", i, record.At, 1000*(i+1))
		}
	}

	last, err := store.Read(key, 0, 2)
	if err != nil {
		t.Fatalf("Read limit: %v", err)
	}
	if len(last) != 2 || last[0].At != 4000 || last[1].At != 5000 {
		t.Fatalf("limit must return the newest two, got %+v", last)
	}
}

func TestReadSinceFilter(t *testing.T) {
	store := newStore(t, Options{CompactEvery: 1000})
	key := Key("conn-1")
	for i := 1; i <= 4; i++ {
		if err := store.Append(key, int64(1000*i), []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	since, err := store.Read(key, 3000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(since) != 2 || since[0].At != 3000 {
		t.Fatalf("since filter wrong: %+v", since)
	}
}

func TestCompactionKeepsNewestSamples(t *testing.T) {
	store := newStore(t, Options{MaxSamples: 3, CompactEvery: 1})
	key := Key("conn-1")
	// Timestamps must sit inside the retention window, otherwise compaction
	// (correctly) treats them as expired before the count cap applies.
	base := time.Now().UnixMilli()
	for i := 1; i <= 6; i++ {
		at := base - int64(6-i)*1000
		if err := store.Append(key, at, []byte(`{"cluster":{"nodes":1}}`)); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	records, err := store.Read(key, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("compaction must cap at 3 samples, got %d", len(records))
	}
	if records[2].At != base {
		t.Fatalf("compaction kept the wrong end: %+v", records)
	}
}

func TestCompactionDropsExpiredSamples(t *testing.T) {
	store := newStore(t, Options{MaxSamples: 100, CompactEvery: 1, Retention: time.Hour})
	// Pin "now" so the retention window is deterministic.
	now := time.UnixMilli(10_000_000)
	store.now = func() time.Time { return now }
	key := Key("conn-1")

	if err := store.Append(key, now.Add(-2*time.Hour).UnixMilli(), []byte(`{"old":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(key, now.Add(-time.Minute).UnixMilli(), []byte(`{"old":false}`)); err != nil {
		t.Fatal(err)
	}
	records, err := store.Read(key, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expired sample must be dropped, got %d: %+v", len(records), records)
	}
	if !strings.Contains(string(records[0].Payload), "false") {
		t.Fatalf("kept the expired sample: %s", records[0].Payload)
	}
}

func TestStatsAndByteCap(t *testing.T) {
	store := newStore(t, Options{MaxSamples: 100, MaxBytes: 400, CompactEvery: 1})
	key := Key("conn-1")
	base := time.Now().UnixMilli()
	for i := 1; i <= 5; i++ {
		payload := fmt.Sprintf(`{"n":%d,"pad":"%s"}`, i, strings.Repeat("x", 60))
		if err := store.Append(key, base-int64(5-i)*1000, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := store.Stats(key)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Samples >= 5 {
		t.Fatalf("byte cap should have dropped samples, kept %d", stats.Samples)
	}
	if stats.Bytes > 400 {
		t.Fatalf("file still larger than the cap allows: %d bytes", stats.Bytes)
	}
	if stats.FirstAt >= stats.LastAt {
		t.Fatalf("stats timestamps wrong: %+v", stats)
	}
	if !strings.HasSuffix(stats.File, ".jsonl") {
		t.Fatalf("file name missing extension: %s", stats.File)
	}
}

func TestKeyIsSafeAndStable(t *testing.T) {
	cases := []string{"local-es", "../../etc/passwd", "连接-1", ""}
	for _, id := range cases {
		key := Key(id)
		if strings.Contains(key, "/") || strings.Contains(key, "\\") || strings.Contains(key, "..") {
			t.Fatalf("key %q is not filesystem safe", key)
		}
		if len(key) > 48 {
			t.Fatalf("key %q is too long", key)
		}
		if Key(id) != key {
			t.Fatalf("Key(%q) is not stable", id)
		}
	}
	if Key("a") == Key("b") {
		t.Fatal("different connections must not collide")
	}
}

func TestReadMissingFileReturnsEmpty(t *testing.T) {
	store := newStore(t, Options{})
	records, err := store.Read(Key("never-collected"), 0, 0)
	if err != nil {
		t.Fatalf("Read on missing file must not error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("want no records, got %d", len(records))
	}
}

func TestAppendRejectsEmptyPayload(t *testing.T) {
	store := newStore(t, Options{})
	if err := store.Append(Key("c"), 1, nil); err == nil {
		t.Fatal("empty payload must be rejected")
	}
}

// A sidecar restart must not lose the series: the collector is recreated with a
// fresh Store pointing at the same data dir.
func TestSeriesSurvivesNewStoreInstance(t *testing.T) {
	dir := t.TempDir()
	key := Key("conn-1")
	at := time.Now().UnixMilli()

	first, err := New(dir, Options{CompactEvery: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Append(key, at, []byte(`{"cluster":{"status":"green"}}`)); err != nil {
		t.Fatal(err)
	}

	second, err := New(dir, Options{CompactEvery: 1})
	if err != nil {
		t.Fatal(err)
	}
	records, err := second.Read(key, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].At != at {
		t.Fatalf("a new Store instance must see the existing series, got %+v", records)
	}
	if string(records[0].Payload) != `{"cluster":{"status":"green"}}` {
		t.Fatalf("payload changed across instances: %s", records[0].Payload)
	}
}
