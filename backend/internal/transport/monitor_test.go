package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
	"github.com/xihonglei/es-studio/backend/internal/collector"
	"github.com/xihonglei/es-studio/backend/internal/storage"
)

func monStub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_cluster/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"green","number_of_nodes":1,"active_primary_shards":3,
			"active_shards":3,"relocating_shards":0,"initializing_shards":0,"unassigned_shards":0}`)
	})
	mux.HandleFunc("/_cluster/stats", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"indices":{"count":2,"docs":{"count":9},"store":{"size_in_bytes":2048}},
			"nodes":{"count":{"total":1},
			"jvm":{"mem":{"heap_used_in_bytes":250,"heap_max_in_bytes":1000}},
			"process":{"cpu":{"percent":1.5}},
			"fs":{"total_in_bytes":100,"available_in_bytes":40}}}`)
	})
	mux.HandleFunc("/_cat/nodes", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"name":"node-a","heap.percent":"25","cpu":"4","load_1m":"0.50","disk.used_percent":"60"}]`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func monHandler(t *testing.T) (*Handler, *storage.Store) {
	t.Helper()
	store, err := storage.New(t.TempDir(), storage.Options{CompactEvery: 1})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return NewHandler(Deps{Store: store, Collector: collector.New(store)}), store
}

func directParams(url, extra string) string {
	return fmt.Sprintf(`{"connection_id":"local-es","direct":{"url":%q}%s}`, url, extra)
}

func waitForSamples(t *testing.T, h *Handler, want int) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res, perr := call(t, h, "mon/history", `{"connection_id":"local-es"}`)
		if perr == nil {
			if count, _ := res["count"].(float64); int(count) >= want {
				return res
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected at least %d samples within 3s", want)
	return nil
}

func TestMonitorStatusWhenNothingRunning(t *testing.T) {
	h, _ := monHandler(t)
	res, perr := call(t, h, "mon/status", `{}`)
	if perr != nil {
		t.Fatalf("mon/status: %v", perr)
	}
	statuses, ok := res["statuses"].([]any)
	if !ok || len(statuses) != 0 {
		t.Fatalf("want empty status list, got %#v", res["statuses"])
	}
}

func TestMonitorRequiresCollector(t *testing.T) {
	h := NewHandler(Deps{})
	_, perr := h.Handle(dbxpluginsdk.RequestContext{}, "mon/status", json.RawMessage(`{}`), nil)
	if perr == nil || perr.Code != -32001 {
		t.Fatalf("sidecar without storage must return -32001, got %#v", perr)
	}
}

func TestMonitorStartRejectsMissingConnection(t *testing.T) {
	h, _ := monHandler(t)
	_, perr := h.Handle(dbxpluginsdk.RequestContext{}, "mon/start", json.RawMessage(`{}`), nil)
	if perr == nil || perr.Code != -32602 {
		t.Fatalf("missing connection_id must be -32602, got %#v", perr)
	}
}

func TestMonitorStartCollectsAndHistoryReadsBack(t *testing.T) {
	srv := monStub(t)
	h, _ := monHandler(t)

	res, perr := call(t, h, "mon/start", directParams(srv.URL, `,"interval_sec":60`))
	if perr != nil {
		t.Fatalf("mon/start: %v", perr)
	}
	if running, _ := res["running"].(bool); !running {
		t.Fatalf("collector should be running: %#v", res)
	}
	if interval, _ := res["interval_sec"].(float64); int(interval) != 60 {
		t.Fatalf("interval_sec = %v, want 60", res["interval_sec"])
	}
	t.Cleanup(func() { _, _ = call(t, h, "mon/stop", `{"connection_id":"local-es"}`) })

	history := waitForSamples(t, h, 1)
	samples, ok := history["samples"].([]any)
	if !ok || len(samples) == 0 {
		t.Fatalf("history returned no samples: %#v", history)
	}
	first, ok := samples[0].(map[string]any)
	if !ok {
		t.Fatalf("sample is not an object: %#v", samples[0])
	}
	cluster, ok := first["cluster"].(map[string]any)
	if !ok {
		t.Fatalf("sample has no cluster section: %#v", first)
	}
	if cluster["status"] != "green" || cluster["docs"] != float64(9) {
		t.Fatalf("persisted sample lost its values: %#v", cluster)
	}
	if at, _ := first["t"].(float64); at <= 0 {
		t.Fatalf("sample has no timestamp: %#v", first)
	}
}

func TestMonitorSampleWritesImmediately(t *testing.T) {
	srv := monStub(t)
	h, _ := monHandler(t)

	res, perr := call(t, h, "mon/sample", directParams(srv.URL, ""))
	if perr != nil {
		t.Fatalf("mon/sample: %v", perr)
	}
	status, ok := res["status"].(map[string]any)
	if !ok {
		t.Fatalf("mon/sample returned no status: %#v", res)
	}
	if stored, _ := status["stored"].(float64); stored < 1 {
		t.Fatalf("sample was not persisted: %#v", status)
	}

	history, perr := call(t, h, "mon/history", `{"connection_id":"local-es","limit":5}`)
	if perr != nil {
		t.Fatalf("mon/history: %v", perr)
	}
	if count, _ := history["count"].(float64); count < 1 {
		t.Fatalf("history should contain the sampled point: %#v", history)
	}
	if file, _ := history["file"].(string); file == "" {
		t.Fatalf("history should report the series file: %#v", history)
	}
}

func TestMonitorStop(t *testing.T) {
	srv := monStub(t)
	h, _ := monHandler(t)
	if _, perr := call(t, h, "mon/start", directParams(srv.URL, `,"interval_sec":5`)); perr != nil {
		t.Fatalf("mon/start: %v", perr)
	}
	res, perr := call(t, h, "mon/stop", `{"connection_id":"local-es"}`)
	if perr != nil {
		t.Fatalf("mon/stop: %v", perr)
	}
	if stopped, _ := res["stopped"].(bool); !stopped {
		t.Fatalf("stop should report a stopped collector: %#v", res)
	}
	status, _ := res["status"].(map[string]any)
	if running, _ := status["running"].(bool); running {
		t.Fatalf("status must show running=false after stop: %#v", status)
	}

	statusRes, perr := call(t, h, "mon/status", `{"connection_id":"local-es"}`)
	if perr != nil {
		t.Fatalf("mon/status: %v", perr)
	}
	single, _ := statusRes["status"].(map[string]any)
	if running, _ := single["running"].(bool); running {
		t.Fatalf("status endpoint still reports running: %#v", single)
	}
}

func TestMonitorHistoryOnEmptySeries(t *testing.T) {
	h, _ := monHandler(t)
	res, perr := call(t, h, "mon/history", `{"connection_id":"never-collected"}`)
	if perr != nil {
		t.Fatalf("mon/history: %v", perr)
	}
	if count, _ := res["count"].(float64); count != 0 {
		t.Fatalf("expected zero samples, got %#v", res["count"])
	}
	if text, ok := res["samples"].([]any); !ok || len(text) != 0 {
		t.Fatalf("samples should be an empty array: %#v", res["samples"])
	}
}
