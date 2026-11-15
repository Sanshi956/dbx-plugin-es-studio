// Package storage persists monitoring samples on disk.
//
// One JSONL file per connection, appended synchronously and compacted behind a
// retention / size cap. Deliberately no SQLite dependency: the backend must
// build offline with GOFLAGS=-mod=mod and no CGO (docs/00-SPEC.md §2.2 named
// SQLite, but a pure-Go driver would add ~10 MB of vendored source for a
// workload of a few thousand small rows — the format below is inspectable with
// any text editor, survives truncation, and needs no migration).
package storage

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Record is one stored sample: a timestamp plus the collector's payload.
type Record struct {
	At      int64           `json:"t"`
	Payload json.RawMessage `json:"p"`
}

// Stats describes what is currently on disk for one connection.
type Stats struct {
	Samples int    `json:"samples"`
	Bytes   int64  `json:"bytes"`
	FirstAt int64  `json:"first_at"`
	LastAt  int64  `json:"last_at"`
	File    string `json:"file"`
}

// Options are the retention knobs. Zero fields fall back to the defaults below.
type Options struct {
	MaxSamples int
	MaxBytes   int64
	Retention  time.Duration
	// CompactEvery controls how many appends trigger a compaction. Exposed for
	// tests; production keeps the default.
	CompactEvery int
}

const (
	// A sample is a few hundred bytes, so 20k samples ≈ 8 MB — a week of
	// one-minute samples with headroom.
	DefaultMaxSamples = 20000
	DefaultMaxBytes   = 8 << 20
	DefaultRetention  = 7 * 24 * time.Hour

	// Compaction rewrites the whole file, so it must not run on every append.
	compactEvery = 64
)

// Store is safe for concurrent use.
type Store struct {
	dir          string
	maxSamples   int
	maxBytes     int64
	retention    time.Duration
	compactEvery int

	mu      sync.Mutex
	pending map[string]int
	now     func() time.Time
}

// New prepares <dir>/metrics. The directory is created lazily on first append
// so a read-only plugin data dir never fails at startup.
func New(dir string, opts Options) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("storage: empty data directory")
	}
	if opts.MaxSamples <= 0 {
		opts.MaxSamples = DefaultMaxSamples
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.Retention <= 0 {
		opts.Retention = DefaultRetention
	}
	if opts.CompactEvery <= 0 {
		opts.CompactEvery = compactEvery
	}
	return &Store{
		dir:          dir,
		maxSamples:   opts.MaxSamples,
		maxBytes:     opts.MaxBytes,
		retention:    opts.Retention,
		compactEvery: opts.CompactEvery,
		pending:      map[string]int{},
		now:          time.Now,
	}, nil
}

// Key turns a connection id into a filesystem-safe, collision-resistant name.
// The readable prefix keeps the data dir diagnosable by hand.
func Key(connectionID string) string {
	sum := sha256.Sum256([]byte(connectionID))
	safe := sanitize(connectionID)
	return safe + "-" + hex.EncodeToString(sum[:4])
}

func sanitize(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 32 {
			break
		}
	}
	if b.Len() == 0 {
		return "conn"
	}
	return b.String()
}

func (s *Store) path(key string) string {
	// Key is generated server-side, but never let a caller-supplied string
	// escape the metrics directory.
	return filepath.Join(s.dir, "metrics", filepath.Base(key)+".jsonl")
}

// Append writes one sample. Append is synchronous so a crash never loses the
// last sample; compaction runs at most once every compactEvery appends.
func (s *Store) Append(key string, at int64, payload []byte) error {
	if at <= 0 {
		at = s.now().UnixMilli()
	}
	if len(payload) == 0 {
		return errors.New("storage: empty sample payload")
	}
	line, err := json.Marshal(Record{At: at, Payload: json.RawMessage(payload)})
	if err != nil {
		return fmt.Errorf("storage: encode sample: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.path(key)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("storage: create data dir: %w", err)
	}
	file, err := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("storage: open %s: %w", filepath.Base(target), err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("storage: append sample: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("storage: close sample file: %w", err)
	}

	s.pending[key]++
	if s.pending[key] < s.compactEvery {
		return nil
	}
	s.pending[key] = 0
	_, err = s.compactLocked(key)
	return err
}

// Read returns the newest samples newer than sinceMs (0 = no lower bound),
// capped at limit (0 = no cap). Corrupt lines are skipped, not fatal.
func (s *Store) Read(key string, sinceMs int64, limit int) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.readLocked(key)
	if err != nil {
		return nil, err
	}
	out := records[:0]
	for _, record := range records {
		if sinceMs > 0 && record.At < sinceMs {
			continue
		}
		out = append(out, record)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// Stats reports the on-disk footprint; it never triggers a compaction.
func (s *Store) Stats(key string) (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.readLocked(key)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{Samples: len(records), File: filepath.Base(s.path(key))}
	if info, err := os.Stat(s.path(key)); err == nil {
		stats.Bytes = info.Size()
	}
	if len(records) > 0 {
		stats.FirstAt = records[0].At
		stats.LastAt = records[len(records)-1].At
	}
	return stats, nil
}

func (s *Store) readLocked(key string) ([]Record, error) {
	file, err := os.Open(s.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", filepath.Base(s.path(key)), err)
	}
	defer func() { _ = file.Close() }()

	records := make([]Record, 0, 256)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue // a torn line from a crash must not hide the rest
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("storage: read samples: %w", err)
	}
	return records, nil
}

// compactLocked drops expired samples, then trims from the head until both the
// count and the byte budget fit. Caller holds s.mu.
func (s *Store) compactLocked(key string) (int, error) {
	records, err := s.readLocked(key)
	if err != nil {
		return 0, err
	}
	cutoff := s.now().Add(-s.retention).UnixMilli()
	kept := records[:0]
	for _, record := range records {
		if record.At >= cutoff {
			kept = append(kept, record)
		}
	}
	if len(kept) > s.maxSamples {
		kept = kept[len(kept)-s.maxSamples:]
	}

	// Byte budget first, then write once: accumulate from the newest sample
	// backwards so a compaction never leaves a partially trimmed file.
	var total int64
	start := len(kept)
	for i := len(kept) - 1; i >= 0; i-- {
		line, err := json.Marshal(kept[i])
		if err != nil {
			continue
		}
		if total+int64(len(line))+1 > s.maxBytes {
			break
		}
		total += int64(len(line)) + 1
		start = i
	}
	kept = kept[start:]

	target := s.path(key)
	tmp := target + ".compact"
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, fmt.Errorf("storage: create data dir: %w", err)
	}
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, fmt.Errorf("storage: open compact file: %w", err)
	}
	writer := bufio.NewWriter(file)
	for _, record := range kept {
		line, err := json.Marshal(record)
		if err != nil {
			continue
		}
		if _, err := writer.Write(append(line, '\n')); err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return 0, fmt.Errorf("storage: write compact file: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("storage: flush compact file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("storage: close compact file: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("storage: replace sample file: %w", err)
	}
	return len(kept), nil
}
