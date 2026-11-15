package transport

import (
	"context"
	"encoding/json"
	"time"

	"github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
	"github.com/xihonglei/es-studio/backend/internal/collector"
	"github.com/xihonglei/es-studio/backend/internal/core/es"
	"github.com/xihonglei/es-studio/backend/internal/core/metrics"
	"github.com/xihonglei/es-studio/backend/internal/storage"
)

// mon/* is the first RPC family that is not a thin ES proxy: the collector has
// to keep its own state (goroutines + on-disk series), which P2/P3 deliberately
// avoided. See docs/04-PLAN-P4.md for why this break is intentional.

const (
	defaultHistoryLimit = 720 // 12h at one sample per minute
	maxHistoryLimit     = 5000
)

func (h *Handler) requireCollector() *dbxpluginsdk.PluginError {
	if h.store == nil || h.collector == nil {
		return dbxpluginsdk.NewError(-32001, "monitoring storage is not available")
	}
	return nil
}

// fetchFor adapts the shared ES client to the collector's FetchFunc.
func (h *Handler) fetchFor(client *es.Client) collector.FetchFunc {
	return func(ctx context.Context) (int64, []byte, error) {
		sample, err := metrics.Collect(ctx, client)
		if err != nil {
			return 0, nil, err
		}
		payload, err := json.Marshal(sample)
		if err != nil {
			return 0, nil, err
		}
		return sample.At, payload, nil
	}
}

func (h *Handler) monStart(p callParams) (any, *dbxpluginsdk.PluginError) {
	if perr := h.requireCollector(); perr != nil {
		return nil, perr
	}
	if p.ConnectionID == "" {
		return nil, dbxpluginsdk.NewError(-32602, "connection_id is required")
	}
	_, client, perr := h.resolve(p)
	if perr != nil {
		return nil, perr
	}
	interval := time.Duration(p.IntervalSec) * time.Second
	status, err := h.collector.Start(p.ConnectionID, collector.NormalizeInterval(interval), h.fetchFor(client))
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32001, err.Error())
	}
	return status, nil
}

func (h *Handler) monStop(p callParams) (any, *dbxpluginsdk.PluginError) {
	if perr := h.requireCollector(); perr != nil {
		return nil, perr
	}
	if p.ConnectionID == "" {
		return nil, dbxpluginsdk.NewError(-32602, "connection_id is required")
	}
	stopped := h.collector.Stop(p.ConnectionID)
	status, err := h.collector.StatusFor(p.ConnectionID)
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32001, err.Error())
	}
	status.Running = false
	return map[string]any{"connection_id": p.ConnectionID, "stopped": stopped, "status": status}, nil
}

func (h *Handler) monStatus(p callParams) (any, *dbxpluginsdk.PluginError) {
	if perr := h.requireCollector(); perr != nil {
		return nil, perr
	}
	if p.ConnectionID != "" {
		status, err := h.collector.StatusFor(p.ConnectionID)
		if err != nil {
			return nil, dbxpluginsdk.NewError(-32001, err.Error())
		}
		return map[string]any{"status": status}, nil
	}
	return map[string]any{"statuses": h.collector.Status()}, nil
}

func (h *Handler) monSample(p callParams) (any, *dbxpluginsdk.PluginError) {
	if perr := h.requireCollector(); perr != nil {
		return nil, perr
	}
	if p.ConnectionID == "" {
		return nil, dbxpluginsdk.NewError(-32602, "connection_id is required")
	}
	_, client, perr := h.resolve(p)
	if perr != nil {
		return nil, perr
	}
	status, err := h.collector.SampleNow(context.Background(), p.ConnectionID, h.fetchFor(client))
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32001, err.Error())
	}
	return map[string]any{"connection_id": p.ConnectionID, "status": status}, nil
}

func (h *Handler) monHistory(p callParams) (any, *dbxpluginsdk.PluginError) {
	if perr := h.requireCollector(); perr != nil {
		return nil, perr
	}
	if p.ConnectionID == "" {
		return nil, dbxpluginsdk.NewError(-32602, "connection_id is required")
	}
	limit := p.Limit
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}

	key := storage.Key(p.ConnectionID)
	records, err := h.store.Read(key, p.Since, limit)
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32001, err.Error())
	}
	stats, err := h.store.Stats(key)
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32001, err.Error())
	}

	samples := make([]json.RawMessage, 0, len(records))
	for _, record := range records {
		samples = append(samples, record.Payload)
	}
	return map[string]any{
		"connection_id": p.ConnectionID,
		"samples":       samples,
		"count":         len(samples),
		"stored":        stats.Samples,
		"first_at":      stats.FirstAt,
		"last_at":       stats.LastAt,
		"bytes":         stats.Bytes,
		"file":          stats.File,
	}, nil
}
