package mcp

import (
	"encoding/json"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/storage"
)

// logKey is the storage series that holds MCP audit rows. One shared file for
// all connections: auditing "who called what" is a single question, and the
// volume is a handful of rows per session.
const logKey = "mcp-calls"

// LogEntry is one audit row. It deliberately records argument *names* only:
// search terms and document ids are user data and must not be persisted as part
// of telemetry (requirement 10.1 — nothing sensitive in logs).
type LogEntry struct {
	At         int64    `json:"at"`
	Tool       string   `json:"tool"`
	Connection string   `json:"connection,omitempty"`
	OK         bool     `json:"ok"`
	Status     int      `json:"status,omitempty"`
	DurationMs int      `json:"ms,omitempty"`
	Bytes      int      `json:"bytes,omitempty"`
	Truncated  bool     `json:"truncated,omitempty"`
	Error      string   `json:"error,omitempty"`
	Args       []string `json:"args,omitempty"`
}

// Logger appends audit rows to the plugin data dir (JSONL, same quotas as the
// monitoring series) and reads them back for the MCP page.
type Logger struct {
	store *storage.Store
}

// NewLogger returns nil when storage is unavailable, which Caller treats as
// "no audit log" instead of failing tool calls.
func NewLogger(store *storage.Store) *Logger {
	if store == nil {
		return nil
	}
	return &Logger{store: store}
}

// Append writes one row; failures are ignored on purpose — losing an audit row
// must never fail a read-only tool call.
func (l *Logger) Append(entry LogEntry) {
	if l == nil || l.store == nil {
		return
	}
	if entry.At == 0 {
		entry.At = time.Now().UnixMilli()
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = l.store.Append(logKey, entry.At, payload)
}

// Recent returns the newest `limit` rows in chronological order.
func (l *Logger) Recent(limit int) ([]LogEntry, error) {
	if l == nil || l.store == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	records, err := l.store.Read(logKey, 0, limit)
	if err != nil {
		return nil, err
	}
	out := make([]LogEntry, 0, len(records))
	for _, record := range records {
		var entry LogEntry
		if json.Unmarshal(record.Payload, &entry) == nil {
			out = append(out, entry)
		}
	}
	return out, nil
}

// Stats reports the log file behind the audit rows (count/bytes/name).
func (l *Logger) Stats() (storage.Stats, error) {
	if l == nil || l.store == nil {
		return storage.Stats{}, nil
	}
	return l.store.Stats(logKey)
}
