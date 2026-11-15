package permission

import (
	"errors"
	"fmt"
	"testing"

	"github.com/xihonglei/es-studio/backend/internal/core/connection"
	"github.com/xihonglei/es-studio/backend/internal/core/es"
)

// T10 (RED): service-layer read-only / path / risk contract.
func TestGuardReadOnly(t *testing.T) {
	conn := connection.Config{ID: "prod-es", ReadOnly: true, Environment: "Production"}
	writes := []es.Request{
		{Method: "PUT", Path: "/orders"},
		{Method: "POST", Path: "/_reindex"},
		{Method: "DELETE", Path: "/orders"},
		{Method: "PUT", Path: "/_cluster/settings"},
		{Method: "POST", Path: "/_cluster/reroute"},
		{Method: "POST", Path: "/orders/_doc"},
	}
	for _, r := range writes {
		if err := Check(conn, r); !errors.Is(err, ErrReadOnlyViolation) {
			t.Fatalf("read-only must reject %s %s, got %v", r.Method, r.Path, err)
		}
	}
	// A read-write connection may perform the same operations.
	rw := conn
	rw.ReadOnly = false
	for _, r := range writes {
		if err := Check(rw, r); err != nil {
			t.Fatalf("rw connection should allow %s %s: %v", r.Method, r.Path, err)
		}
	}
}

func TestGuardSafeReadsAlwaysAllowed(t *testing.T) {
	conn := connection.Config{ID: "prod-es", ReadOnly: true}
	safe := []es.Request{
		{Method: "GET", Path: "/_cluster/health"},
		{Method: "HEAD", Path: "/orders"},
		{Method: "POST", Path: "/orders/_search"},
		{Method: "POST", Path: "/orders/_count"},
		{Method: "POST", Path: "/_analyze"},
		{Method: "GET", Path: "/_cat/indices?format=json"},
	}
	for _, r := range safe {
		if err := Check(conn, r); err != nil {
			t.Fatalf("safe read %s %s rejected: %v", r.Method, r.Path, err)
		}
	}
}

func TestGuardValidatePath(t *testing.T) {
	unsafe := []string{
		"http://evil.example/_search",
		"https://evil.example/_search",
		"//evil.example/_search",
		"../_cluster/settings",
		"/_cat/../_cluster/settings",
		"/_search@evil.example",
		"user:pass@host/_search",
		"/index\r\nInjected: 1",
		"",
	}
	for _, p := range unsafe {
		if err := ValidatePath(p); !errors.Is(err, es.ErrUnsafePath) {
			t.Fatalf("ValidatePath(%q) must return ErrUnsafePath, got %v", p, err)
		}
	}
	for _, p := range []string{"/", "/_cluster/health", "/orders/_search", "/_cat/indices?format=json"} {
		if err := ValidatePath(p); err != nil {
			t.Fatalf("ValidatePath(%q) should pass: %v", p, err)
		}
	}
}

// T13 (P2): every P2 write entry point must be blocked on a read-only
// connection and allowed on a read-write one.
func TestGuardP2WritePaths(t *testing.T) {
	ro := connection.Config{ID: "prod-es", ReadOnly: true}
	rw := ro
	rw.ReadOnly = false
	writes := []es.Request{
		{Method: "DELETE", Path: "/orders"},
		{Method: "DELETE", Path: "/orders/_doc/42"},
		{Method: "POST", Path: "/orders/_close"},
		{Method: "POST", Path: "/orders/_open"},
		{Method: "POST", Path: "/orders/_forcemerge"},
		{Method: "POST", Path: "/_aliases"},
		{Method: "PUT", Path: "/new-index"},
	}
	for _, r := range writes {
		if err := Check(ro, r); !errors.Is(err, ErrReadOnlyViolation) {
			t.Fatalf("read-only must reject P2 write %s %s, got %v", r.Method, r.Path, err)
		}
		if err := Check(rw, r); err != nil {
			t.Fatalf("rw must allow P2 write %s %s: %v", r.Method, r.Path, err)
		}
	}
}

// T13 (P2): read-only connections may still run search and analysis.
func TestGuardP2ReadsAllowedOnReadOnly(t *testing.T) {
	ro := connection.Config{ID: "prod-es", ReadOnly: true}
	safe := []es.Request{
		{Method: "POST", Path: "/orders/_search"},
		{Method: "POST", Path: "/_search"},
		{Method: "POST", Path: "/_analyze"},
		{Method: "POST", Path: "/orders/_analyze"},
		{Method: "POST", Path: "/_count"},
	}
	for _, r := range safe {
		if err := Check(ro, r); err != nil {
			t.Fatalf("read-only must allow P2 read %s %s: %v", r.Method, r.Path, err)
		}
	}
}

// T13 (P3): every P3 write path must be rejected on read-only and allowed on rw.
func TestGuardP3WritePaths(t *testing.T) {
	ro := connection.Config{ID: "prod-es", ReadOnly: true}
	rw := ro
	rw.ReadOnly = false
	writes := []es.Request{
		{Method: "PUT", Path: "/orders/_settings"},                      // allocation / replicas
		{Method: "PUT", Path: "/_cluster/settings"},                     // cluster settings
		{Method: "PUT", Path: "/_index_template/p3-demo"},               // index template
		{Method: "DELETE", Path: "/_index_template/p3-demo"},            // index template
		{Method: "PUT", Path: "/_component_template/p3-demo"},           // component template
		{Method: "DELETE", Path: "/_component_template/p3-demo"},        //
		{Method: "PUT", Path: "/_data_stream/logs-p3-demo"},             // data stream
		{Method: "POST", Path: "/logs-p3-demo/_rollover"},               // rollover
		{Method: "DELETE", Path: "/_data_stream/logs-p3-demo"},          //
		{Method: "POST", Path: "/_ilm/stop"},                            // ILM global state
		{Method: "PUT", Path: "/_ilm/policy/p3-demo"},                   // ILM policy
		{Method: "DELETE", Path: "/_ilm/policy/p3-demo"},                //
		{Method: "PUT", Path: "/_snapshot/p3-fs-repo"},                  // snapshot repository
		{Method: "PUT", Path: "/_snapshot/p3-fs-repo/snap-1"},           // create snapshot
		{Method: "POST", Path: "/_snapshot/p3-fs-repo/snap-1/_restore"}, // restore
		{Method: "DELETE", Path: "/_snapshot/p3-fs-repo/snap-1"},        //
		{Method: "POST", Path: "/_tasks/node-1:42/_cancel"},             // cancel task
	}
	for _, r := range writes {
		if err := Check(ro, r); !errors.Is(err, ErrReadOnlyViolation) {
			t.Fatalf("read-only must reject P3 write %s %s, got %v", r.Method, r.Path, err)
		}
		if err := Check(rw, r); err != nil {
			t.Fatalf("rw must allow P3 write %s %s: %v", r.Method, r.Path, err)
		}
	}
}

// T13 (P3): P3 screens are mostly read-only and must work on a read-only connection.
func TestGuardP3ReadsAllowedOnReadOnly(t *testing.T) {
	ro := connection.Config{ID: "prod-es", ReadOnly: true}
	safe := []es.Request{
		{Method: "GET", Path: "/_cluster/allocation/explain"},
		{Method: "GET", Path: "/_cluster/settings?flat_settings=true"},
		{Method: "GET", Path: "/_index_template"},
		{Method: "GET", Path: "/_component_template"},
		{Method: "GET", Path: "/_data_stream"},
		{Method: "GET", Path: "/_ilm/status"},
		{Method: "GET", Path: "/_ilm/policy"},
		{Method: "GET", Path: "/_all/_ilm/explain"}, // ES 8.x requires an explicit target
		{Method: "GET", Path: "/_snapshot"},
		{Method: "GET", Path: "/_tasks?group_by=none&detailed=true"},
		{Method: "POST", Path: "/products/_search"}, // profiler is a search
	}
	for _, r := range safe {
		if err := Check(ro, r); err != nil {
			t.Fatalf("read-only must allow P3 read %s %s: %v", r.Method, r.Path, err)
		}
	}
}

// T19 (P3): DescribeRisk must name the P3 operations instead of a generic label.
func TestGuardDescribeRiskP3(t *testing.T) {
	cases := map[string]string{
		"PUT /_cluster/settings":             "Update Cluster Settings",
		"PUT /orders/_settings":              "Update Index Settings",
		"POST /_snapshot/repo/snap/_restore": "Snapshot Operation",
		"DELETE /_snapshot/repo/snap":        "Snapshot Operation",
		"PUT /_index_template/tpl":           "Modify Index Template",
		"DELETE /_component_template/comp":   "Modify Index Template",
		"DELETE /_data_stream/logs":          "Modify Data Stream",
		"POST /logs/_rollover":               "Modify Data Stream",
		"POST /_ilm/stop":                    "Lifecycle Operation",
		"DELETE /_ilm/policy/p":              "Lifecycle Operation",
		"POST /_tasks/node-1:42/_cancel":     "Cancel Task",
	}
	for input, want := range cases {
		var method, path string
		if _, err := fmt.Sscanf(input, "%s %s", &method, &path); err != nil {
			t.Fatalf("bad case %q: %v", input, err)
		}
		risk, ok := DescribeRisk(es.Request{Method: method, Path: path})
		if !ok {
			t.Fatalf("DescribeRisk(%s) should be flagged", input)
		}
		if risk.Name != want {
			t.Fatalf("DescribeRisk(%s).Name = %q, want %q", input, risk.Name, want)
		}
	}
}

func TestGuardDescribeRisk(t *testing.T) {
	cases := []es.Request{
		{Method: "DELETE", Path: "/prod-orders"},
		{Method: "POST", Path: "/_reindex"},
		{Method: "POST", Path: "/_cluster/reroute"},
		{Method: "PUT", Path: "/_cluster/settings"},
	}
	for _, r := range cases {
		risk, ok := DescribeRisk(r)
		if !ok {
			t.Fatalf("DescribeRisk(%s %s) should be flagged", r.Method, r.Path)
		}
		if risk.Name == "" || risk.Target == "" || risk.Scope == "" || risk.Reason == "" {
			t.Fatalf("incomplete Risk for %s %s: %+v", r.Method, r.Path, risk)
		}
	}
	if _, ok := DescribeRisk(es.Request{Method: "GET", Path: "/_cluster/health"}); ok {
		t.Fatal("a GET must not be flagged as a dangerous operation")
	}
}
