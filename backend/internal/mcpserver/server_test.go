package mcpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/xihonglei/es-studio/backend/internal/mcp"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*Server, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	parsed, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse upstream port: %v", err)
	}
	// Grants keep host and port apart (the Sidecar never accepts a URL from the
	// client), so the fixture splits the httptest URL the same way.
	grant := mcp.Grant{
		Token: "tok", ConnectionID: "local-es", Label: "Local ES (test)",
		Scheme: "http", Host: parsed.Hostname(), Port: port, TimeoutSecs: 5,
	}
	return &Server{Caller: mcp.NewCaller(mcp.Default(), nil), Grant: grant, Version: "0.4.0"}, upstream
}

func run(t *testing.T, server *Server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	server.In = strings.NewReader(strings.Join(lines, "\n") + "\n")
	server.Out = &out
	if err := server.Serve(); err != nil {
		t.Fatalf("serve: %v", err)
	}
	decoder := json.NewDecoder(&out)
	var responses []map[string]any
	for decoder.More() {
		var message map[string]any
		if err := decoder.Decode(&message); err != nil {
			t.Fatalf("decode: %v", err)
		}
		responses = append(responses, message)
	}
	return responses
}

func TestInitializeNegotiatesProtocolAndAdvertisesTools(t *testing.T) {
	server, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	responses := run(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	)
	if len(responses) != 1 {
		t.Fatalf("a notification must not be answered: got %d responses", len(responses))
	}
	result := responses[0]["result"].(map[string]any)
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("protocolVersion = %v", result["protocolVersion"])
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "es-studio-mcp" || info["version"] != "0.4.0" {
		t.Fatalf("serverInfo = %v", info)
	}
	capabilities := result["capabilities"].(map[string]any)
	if _, ok := capabilities["tools"]; !ok {
		t.Fatal("tools capability missing")
	}
}

func TestUnknownProtocolVersionFallsBackToNewest(t *testing.T) {
	server, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	responses := run(t, server, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if responses[0]["result"].(map[string]any)["protocolVersion"] != supportedVersions[0] {
		t.Fatalf("expected fallback to %s", supportedVersions[0])
	}
}

func TestToolsListExposesCatalogue(t *testing.T) {
	server, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	responses := run(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := responses[0]["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 40 {
		t.Fatalf("tools = %d, want 40+", len(tools))
	}
	first := tools[0].(map[string]any)
	for _, key := range []string{"name", "description", "inputSchema", "annotations"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("tool descriptor missing %s: %v", key, first)
		}
	}
	annotations := first["annotations"].(map[string]any)
	if annotations["readOnlyHint"] != true {
		t.Fatal("every tool must advertise readOnlyHint")
	}
}

func TestToolsCallReturnsTextContent(t *testing.T) {
	server, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_cluster/health" || r.Method != "GET" {
			t.Errorf("upstream saw %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"green","number_of_nodes":1}`))
	})
	responses := run(t, server,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"es_get_cluster_health","arguments":{}}}`)
	result := responses[0]["result"].(map[string]any)
	if result["isError"] != false {
		t.Fatalf("isError = %v", result["isError"])
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"status": "green"`) {
		t.Fatalf("text = %q", text)
	}
	structured := result["structuredContent"].(map[string]any)
	if structured["status"].(float64) != 200 || structured["request"] != "GET /_cluster/health" {
		t.Fatalf("structuredContent = %v", structured)
	}
}

func TestToolsCallReportsToolFailureAsResultNotProtocolError(t *testing.T) {
	server, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	responses := run(t, server,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"es_get_index_mapping","arguments":{}}}`)
	result := responses[0]["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("missing required argument must be a tool error: %v", result)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "index") {
		t.Fatalf("error text should name the argument: %q", text)
	}
	if _, isProtocolError := responses[0]["error"]; isProtocolError {
		t.Fatal("tool failures must not use the protocol error channel")
	}
}

func TestUpstreamHTTPErrorIsFlagged(t *testing.T) {
	server, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"type":"index_not_found_exception"},"status":404}`))
	})
	responses := run(t, server,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"es_get_index_mapping","arguments":{"index":"missing"}}}`)
	result := responses[0]["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("404 must be flagged as isError: %v", result["isError"])
	}
	if result["structuredContent"].(map[string]any)["status"].(float64) != 404 {
		t.Fatal("status must be preserved")
	}
}

func TestUnknownMethodAndBadJSON(t *testing.T) {
	server, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	responses := run(t, server,
		`{"jsonrpc":"2.0","id":6,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
		`not json at all`,
	)
	if responses[0]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatalf("unknown method = %v", responses[0]["error"])
	}
	if _, ok := responses[1]["result"]; !ok {
		t.Fatalf("ping must be answered: %v", responses[1])
	}
	if responses[2]["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Fatalf("bad json = %v", responses[2]["error"])
	}
}
