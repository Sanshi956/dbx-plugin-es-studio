package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xihonglei/es-studio/backend/internal/core/es"
)

// stub returns a server with the exact 8.x response shapes we parse, including
// the `_cat` quirks ("-" for unavailable values, everything as a string).
func stub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_cluster/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"cluster_name":"unit","status":"yellow","number_of_nodes":2,
			"number_of_data_nodes":2,"active_primary_shards":16,"active_shards":20,
			"relocating_shards":1,"initializing_shards":2,"unassigned_shards":3}`)
	})
	mux.HandleFunc("/_cluster/stats", func(w http.ResponseWriter, r *http.Request) {
		// Copied from a live 8.19 response: jvm.mem (not jvm.memory), flat fs.
		_, _ = io.WriteString(w, `{"indices":{"count":6,"docs":{"count":42},
			"store":{"size_in_bytes":123456}},
			"nodes":{"count":{"total":2},
			"jvm":{"max_uptime_in_millis":1000,
				"mem":{"heap_used_in_bytes":400000000,"heap_max_in_bytes":1000000000}},
			"process":{"cpu":{"percent":7.25}},
			"fs":{"total_in_bytes":200000000000,"free_in_bytes":100000000000,"available_in_bytes":100000000000}}}`)
	})
	mux.HandleFunc("/_cat/nodes", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("h"); got == "" {
			t.Errorf("_cat/nodes called without explicit h= (got %q)", got)
		}
		_, _ = io.WriteString(w, `[
			{"name":"node-1","heap.percent":"42","cpu":"3","load_1m":"0.85","disk.used_percent":"88"},
			{"name":"node-2","heap.percent":"55","cpu":"-","load_1m":"1.20","disk.used_percent":"-"}]`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func client(t *testing.T, url string) *es.Client {
	t.Helper()
	created, err := es.NewClient(es.Config{BaseURL: url})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return created
}

func TestCollectParsesClusterAndNodes(t *testing.T) {
	srv := stub(t)
	sample, err := Collect(context.Background(), client(t, srv.URL))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if sample.At <= 0 {
		t.Fatal("sample has no timestamp")
	}
	cluster := sample.Cluster
	if cluster.Status != "yellow" || cluster.Nodes != 2 || cluster.Primary != 16 || cluster.Active != 20 {
		t.Fatalf("health fields wrong: %+v", cluster)
	}
	if cluster.Relocating != 1 || cluster.Initializing != 2 || cluster.Unassigned != 3 {
		t.Fatalf("shard state fields wrong: %+v", cluster)
	}
	if cluster.Indices != 6 || cluster.Docs != 42 || cluster.StoreBytes != 123456 {
		t.Fatalf("stats fields wrong: %+v", cluster)
	}
	if cluster.HeapPercent != 40 {
		t.Fatalf("heap percent = %v, want 40", cluster.HeapPercent)
	}
	if cluster.CpuPercent != 7.25 {
		t.Fatalf("cpu percent = %v, want 7.25", cluster.CpuPercent)
	}
	if cluster.DiskFree != 100000000000 || cluster.DiskTotal != 200000000000 {
		t.Fatalf("disk bytes wrong: %+v", cluster)
	}

	node1, ok := sample.Nodes["node-1"]
	if !ok || len(sample.Nodes) != 2 {
		t.Fatalf("nodes map wrong: %+v", sample.Nodes)
	}
	if node1.HeapPercent != 42 || node1.CpuPercent != 3 || node1.Load1 != 0.85 || node1.DiskPercent != 88 {
		t.Fatalf("node-1 fields wrong: %+v", node1)
	}
	// "-" must become 0 rather than poison the series with NaN.
	if node2 := sample.Nodes["node-2"]; node2.CpuPercent != 0 || node2.DiskPercent != 0 {
		t.Fatalf("unavailable cat values must be 0: %+v", node2)
	}
}

func TestCollectFailsWithoutHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"nope"}`)
	}))
	t.Cleanup(srv.Close)
	if _, err := Collect(context.Background(), client(t, srv.URL)); err == nil {
		t.Fatal("a failed /_cluster/health must fail the whole collection")
	}
}

func TestCollectToleratesMissingStats(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_cluster/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"green","number_of_nodes":1,"active_primary_shards":1,"active_shards":1}`)
	})
	srv := httptest.NewServer(mux) // everything else 404s
	t.Cleanup(srv.Close)

	sample, err := Collect(context.Background(), client(t, srv.URL))
	if err != nil {
		t.Fatalf("missing stats must not fail collection: %v", err)
	}
	if sample.Cluster.Status != "green" || sample.Cluster.Docs != 0 {
		t.Fatalf("unexpected sample: %+v", sample.Cluster)
	}
	if len(sample.Nodes) != 0 {
		t.Fatalf("no nodes expected, got %+v", sample.Nodes)
	}
}
