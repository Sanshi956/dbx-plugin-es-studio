package permission

import (
	"errors"
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
