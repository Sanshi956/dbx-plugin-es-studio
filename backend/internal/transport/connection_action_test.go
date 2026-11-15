package transport

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
)

func actionEndpoint(t *testing.T, rawurl string) (string, int) {
	t.Helper()
	u, err := url.Parse(rawurl)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname(), port
}

// DBX sends the provider action plus a nested connection/form payload.
func dbxActionPayload(action string, host string, port int, connectionID string) string {
	doc := map[string]any{
		"action": map[string]any{"id": action, "label": "Test Connection"},
		"connection": map[string]any{
			"id": connectionID,
			"external_config": map[string]any{
				"host":         host,
				"port":         port,
				"protocol":     "http",
				"auth_type":    "none",
				"timeout_secs": 10,
				"password":     "super-secret-must-not-leak",
				"api_key":      "secret-key-must-not-leak",
				"bearer_token": "secret-token-must-not-leak",
			},
			"connection_secrets": map[string]any{},
		},
		"provider": "local.es.studio.connection",
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

func TestConnectionActionTestReadsNestedForm(t *testing.T) {
	srv := esStub(t)
	host, port := actionEndpoint(t, srv.URL)
	h := NewHandler(Deps{})

	result, perr := call(t, h, "connection/action", dbxActionPayload("test", host, port, "conn-action-1"))
	if perr != nil {
		t.Fatalf("connection/action test: %v", perr)
	}
	if result["success"] != true || result["connected"] != true {
		t.Fatalf("expected a successful test, got %v", result)
	}
	if result["version"] != "8.19.23" {
		t.Fatalf("version came from the real stub: %v", result["version"])
	}
	if endpoint, _ := result["endpoint"].(string); !strings.Contains(endpoint, ":"+strconv.Itoa(port)) {
		t.Fatalf("nested port was not resolved: %v", result["endpoint"])
	}
}

func TestConnectionActionConnectThenRequest(t *testing.T) {
	srv := esStub(t)
	host, port := actionEndpoint(t, srv.URL)
	h := NewHandler(Deps{})

	connected, perr := call(t, h, "connection/action", dbxActionPayload("connect", host, port, "conn-action-2"))
	if perr != nil {
		t.Fatalf("connect: %v", perr)
	}
	if connected["connection_id"] != "conn-action-2" {
		t.Fatalf("action id leaked into the session key: %v", connected["connection_id"])
	}
	if connected["connected"] != true {
		t.Fatalf("connect result: %v", connected)
	}

	res, perr := call(t, h, "es/request", `{"connection_id":"conn-action-2","method":"GET","path":"/_cluster/health"}`)
	if perr != nil || res["status"] != float64(200) {
		t.Fatalf("request through action-created session failed: %v %v", res, perr)
	}

	removed, perr := call(t, h, "connection/action", dbxActionPayload("disconnect", host, port, "conn-action-2"))
	if perr != nil || removed["connected"] != false {
		t.Fatalf("disconnect failed: %v %v", removed, perr)
	}
	if _, perr := call(t, h, "es/request", `{"connection_id":"conn-action-2","method":"GET","path":"/"}`); perr == nil {
		t.Fatal("session should be gone after disconnect")
	}
}

func TestConnectionActionUnknownAction(t *testing.T) {
	srv := esStub(t)
	host, port := actionEndpoint(t, srv.URL)
	h := NewHandler(Deps{})
	_, perr := call(t, h, "connection/action", dbxActionPayload("drop-everything", host, port, "conn-action-3"))
	if perr == nil || perr.Code != -32601 {
		t.Fatalf("expected method-not-found for unknown action, got %v", perr)
	}
}

func TestConnectionActionShapeLogNeverContainsSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dbxpluginsdk.DataDirEnvVar, dir)
	srv := esStub(t)
	host, port := actionEndpoint(t, srv.URL)
	h := NewHandler(Deps{})
	if _, perr := call(t, h, "connection/action", dbxActionPayload("test", host, port, "conn-action-4")); perr != nil {
		t.Fatalf("test action: %v", perr)
	}
	content, err := os.ReadFile(filepath.Join(dir, "es-studio-rpc-shapes.jsonl"))
	if err != nil {
		t.Fatalf("shape log missing: %v", err)
	}
	for _, secret := range []string{"super-secret-must-not-leak", "secret-key-must-not-leak", "secret-token-must-not-leak"} {
		if strings.Contains(string(content), secret) {
			t.Fatalf("shape log leaked a credential: %s", secret)
		}
	}
	if !strings.Contains(string(content), "external_config") || !strings.Contains(string(content), `"has_host":true`) {
		t.Fatalf("shape log lacks the protocol structure: %s", content)
	}
}
