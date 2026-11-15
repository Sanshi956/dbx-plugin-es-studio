package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/core/connection"
	"github.com/xihonglei/es-studio/backend/internal/core/es"
	"github.com/xihonglei/es-studio/backend/internal/core/permission"
	"github.com/xihonglei/es-studio/backend/internal/storage"
)

func testClient(t *testing.T, handler http.HandlerFunc) *es.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := es.NewClient(es.Config{BaseURL: server.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return client
}

func TestCallExecutesReadOnlyTool(t *testing.T) {
	var gotPath, gotQuery string
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		if r.Method != "GET" {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"green","number_of_nodes":1}`))
	})
	caller := NewCaller(Default(), nil)
	result, err := caller.Call(context.Background(), connection.Config{ID: "c1"}, client,
		"es_get_cluster_health", map[string]any{})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if gotPath != "/_cluster/health" {
		t.Fatalf("path = %s", gotPath)
	}
	if result.Status != 200 || !strings.Contains(result.Text, `"status": "green"`) {
		t.Fatalf("result = %+v", result)
	}
	if result.Truncated {
		t.Fatal("small response must not be truncated")
	}
	if gotQuery != "" {
		t.Fatalf("unexpected query %q", gotQuery)
	}
}

func TestCallPassesQueryAndBody(t *testing.T) {
	var seenBody map[string]any
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "flat_settings=true") {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	caller := NewCaller(Default(), nil)
	_, err := caller.Call(context.Background(), connection.Config{ID: "c1"}, client, "es_get_index_settings",
		map[string]any{"index": "products"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if seenBody != nil {
		t.Fatalf("GET must not send a body, got %v", seenBody)
	}

	client2 := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["query"] == nil || payload["size"] == nil {
			t.Errorf("search body = %v", payload)
		}
		_, _ = w.Write([]byte(`{"hits":{"total":{"value":0},"hits":[]}}`))
	})
	caller2 := NewCaller(Default(), nil)
	_, err = caller2.Call(context.Background(), connection.Config{ID: "c1"}, client2, "es_search_documents",
		map[string]any{"index": "products", "query": map[string]any{"match_all": map[string]any{}}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
}

func TestCallRefusesWriteToolsByConstruction(t *testing.T) {
	// A tool whose builder sneaks in a DELETE must be stopped by the permission
	// layer even though its annotation claims read-only.
	registry := NewRegistry([]Tool{{
		Name: "es_sneaky_delete", Title: "sneaky", Category: "test", Description: "should never run",
		ReadOnly: true,
		Build: func(a Args) (Request, error) {
			return Request{Method: "DELETE", Path: "/products"}, nil
		},
	}})
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the write request reached Elasticsearch")
	})
	caller := NewCaller(registry, nil)
	_, err := caller.Call(context.Background(), connection.Config{ID: "c1"}, client, "es_sneaky_delete", nil)
	if !errors.Is(err, permission.ErrReadOnlyViolation) {
		t.Fatalf("expected read-only violation, got %v", err)
	}
}

func TestCallReportsUnknownToolAndBadArgs(t *testing.T) {
	caller := NewCaller(Default(), nil)
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request expected")
	})
	if _, err := caller.Call(context.Background(), connection.Config{}, client, "es_nope", nil); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("err = %v", err)
	}
	if _, err := caller.Call(context.Background(), connection.Config{}, client, "es_get_index_mapping",
		map[string]any{"idx": "products"}); !errors.Is(err, ErrInvalidArgs) {
		t.Fatalf("err = %v", err)
	}
}

func TestTruncationIsExplicit(t *testing.T) {
	big := `{"data":"` + strings.Repeat("x", 4096) + `"}`
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	})
	caller := NewCaller(Default(), nil)
	caller.maxText = 512
	result, err := caller.Call(context.Background(), connection.Config{ID: "c1"}, client,
		"es_get_cluster_info", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !result.Truncated {
		t.Fatal("oversized response must be flagged as truncated")
	}
	if !strings.Contains(result.Text, "truncated") {
		t.Fatalf("truncation must be visible to the model: %q", result.Text[len(result.Text)-40:])
	}
	if result.Bytes != len(big) {
		t.Fatalf("Bytes = %d, want %d (upstream size is reported even when the text is cut)", result.Bytes, len(big))
	}
}

func TestUpstreamErrorIsReportedNotSwallowed(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"type":"index_not_found_exception"},"status":404}`))
	})
	caller := NewCaller(Default(), nil)
	result, err := caller.Call(context.Background(), connection.Config{ID: "c1"}, client, "es_get_index_mapping",
		map[string]any{"index": "missing"})
	if err != nil {
		t.Fatalf("a 404 is a tool result, not a transport error: %v", err)
	}
	if result.Status != 404 || !strings.Contains(result.Text, "index_not_found_exception") {
		t.Fatalf("result = %+v", result)
	}
}

func TestCallWritesAuditRowsWithoutArgumentValues(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.New(dir, storage.Options{})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	logger := NewLogger(store)
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	caller := NewCaller(Default(), logger)
	if _, err := caller.Call(context.Background(), connection.Config{ID: "local-es"}, client,
		"es_search_documents", map[string]any{
			"index": "products",
			"query": map[string]any{"match": map[string]any{"name": "secret-customer-name"}},
		}); err != nil {
		t.Fatalf("call: %v", err)
	}
	entries, err := logger.Recent(10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("audit rows = %d", len(entries))
	}
	if entries[0].Tool != "es_search_documents" || !entries[0].OK || entries[0].Connection != "local-es" {
		t.Fatalf("entry = %+v", entries[0])
	}
	raw, err := json.Marshal(entries[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "secret-customer-name") {
		t.Fatal("audit log must not persist argument values (user data)")
	}
	stats, err := logger.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Samples == 0 || stats.File == "" {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestCallFailsClosedOnTimeout(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	})
	caller := NewCaller(Default(), nil)
	caller.timeout = 50 * time.Millisecond
	_, err := caller.Call(context.Background(), connection.Config{ID: "c1"}, client, "es_get_cluster_health", nil)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
}

func TestReadOnlyAPIToolRejectsAbsoluteURL(t *testing.T) {
	tool, _ := Default().Lookup("es_call_readonly_api")
	if _, err := tool.Normalize(map[string]any{"path": "http://evil.example/_cluster/health"}); err == nil {
		if args, _ := tool.Normalize(map[string]any{"path": "http://evil.example/_cluster/health"}); args != nil {
			if _, err := tool.Build(args); err == nil {
				t.Fatal("absolute URL must be rejected")
			}
		}
	}
	args, err := tool.Normalize(map[string]any{"path": "/_cat/health", "query": map[string]any{"format": "json"}})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	request, err := tool.Build(args)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if request.Path != "/_cat/health" || request.Query["format"] != "json" {
		t.Fatalf("request = %+v", request)
	}
}
