package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
)

func esStub(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			_, _ = io.WriteString(w, `{"name":"node-1","cluster_name":"es-studio-unit","cluster_uuid":"uuid-1","version":{"number":"8.19.23","distribution":"elasticsearch"},"tagline":"You Know, for Search"}`)
		case "/_cluster/health":
			_, _ = io.WriteString(w, `{"cluster_name":"es-studio-unit","status":"green","number_of_nodes":1}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func call(t *testing.T, h *Handler, method, params string) (map[string]any, *dbxpluginsdk.PluginError) {
	t.Helper()
	res, perr := h.Handle(dbxpluginsdk.RequestContext{}, method, json.RawMessage(params), nil)
	if perr != nil {
		return nil, perr
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("result is not a JSON object: %s", b)
	}
	return m, nil
}

// T12 (RED): Sidecar protocol contract, docs/01-PLAN.md.
func TestSidecarInitialize(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"plugin/initialize","params":{"host":{"protocolVersions":[1]}}}` + "\n")
	out := &bytes.Buffer{}
	srv := dbxpluginsdk.NewServer(
		dbxpluginsdk.Metadata{ID: "local.es.studio", Version: "0.1.0", Capabilities: []string{"es/request"}},
		NewHandler(Deps{}),
	).WithIO(in, out, io.Discard)
	if err := srv.Serve(); err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result struct {
			ProtocolVersion int `json:"protocolVersion"`
			Plugin          struct {
				ID      string `json:"id"`
				Version string `json:"version"`
			} `json:"plugin"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("bad initialize response %q: %v", out.String(), err)
	}
	if resp.Result.ProtocolVersion != dbxpluginsdk.ProtocolVersion ||
		resp.Result.Plugin.ID != "local.es.studio" || resp.Result.Plugin.Version != "0.1.0" {
		t.Fatalf("initialize mismatch: %+v", resp.Result)
	}
}

func TestSidecarPingAndMethodNotFound(t *testing.T) {
	h := NewHandler(Deps{})
	r, perr := call(t, h, "es/ping", `{}`)
	if perr != nil || r["pong"] != true {
		t.Fatalf("es/ping failed: %+v %+v", r, perr)
	}
	_, perr = h.Handle(dbxpluginsdk.RequestContext{}, "no/such/method", json.RawMessage(`{}`), nil)
	if perr == nil || perr.Code != -32601 {
		t.Fatalf("unknown method must return -32601, got %+v", perr)
	}
}

func TestSidecarConnectionTest(t *testing.T) {
	srv := esStub(t)
	h := NewHandler(Deps{})
	r, perr := call(t, h, "connection/test", `{"direct":{"url":"`+srv.URL+`"}}`)
	if perr != nil {
		t.Fatalf("connection/test error: %+v", perr)
	}
	if r["connected"] != true || r["version"] != "8.19.23" ||
		r["cluster_name"] != "es-studio-unit" || r["engine"] != "elasticsearch" {
		t.Fatalf("connection/test must read REAL cluster metadata: %+v", r)
	}
}

func TestSidecarLifecycleAndRequest(t *testing.T) {
	srv := esStub(t)
	h := NewHandler(Deps{})
	direct := `{"direct":{"url":"` + srv.URL + `"}}`

	for i := 0; i < 2; i++ { // idempotent connect
		r, perr := call(t, h, "connection/connect", `{"connection_id":"dev-es",`+direct[1:])
		if perr != nil || r["connected"] != true {
			t.Fatalf("connect #%d failed: %+v %+v", i, r, perr)
		}
	}
	r, perr := call(t, h, "es/request", `{"connection_id":"dev-es","method":"GET","path":"/_cluster/health"}`)
	if perr != nil {
		t.Fatalf("es/request error: %+v", perr)
	}
	if r["status"] != float64(200) {
		t.Fatalf("es/request status = %v, want 200: %+v", r["status"], r)
	}
	body, ok := r["body"].(map[string]any)
	if !ok || body["status"] != "green" {
		t.Fatalf("es/request body lost: %+v", r["body"])
	}
	for i := 0; i < 2; i++ { // idempotent disconnect
		if _, perr := call(t, h, "connection/disconnect", `{"connection_id":"dev-es"}`); perr != nil {
			t.Fatalf("disconnect #%d must not error: %+v", i, perr)
		}
	}
}

func TestSidecarDevHostLifecycle(t *testing.T) {
	srv := esStub(t)
	addr := strings.TrimPrefix(srv.URL, "http://")
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		t.Fatalf("unexpected stub url %q", srv.URL)
	}
	// Exact shape produced by the official dbx-plugin dev host lifecyclePayload().
	payload := `{"provider":{"id":"local.es.studio.connection"},"connection":{` +
		`"id":"dev-1","name":"Local ES","host":"` + parts[0] + `","port":` + parts[1] + `,` +
		`"external_config":{"protocol":"http","auth_type":"none","timeout_secs":5,"tls_verify":true},` +
		`"connection_secrets":{},"read_only":false},"runtime":null}`
	h := NewHandler(Deps{})
	r, perr := call(t, h, "connection/connect", payload)
	if perr != nil || r["connected"] != true || r["connection_id"] != "dev-1" {
		t.Fatalf("dev-host connect failed: %+v %+v", r, perr)
	}
	r, perr = call(t, h, "es/request", `{"connection_id":"dev-1","method":"GET","path":"/_cluster/health"}`)
	if perr != nil || r["status"] != float64(200) {
		t.Fatalf("dev-host session request failed: %+v %+v", r, perr)
	}
	r, perr = call(t, h, "connection/disconnect", payload)
	if perr != nil || r["disconnected"] != true {
		t.Fatalf("dev-host disconnect failed: %+v %+v", r, perr)
	}
}

func TestSidecarNoSecretLeak(t *testing.T) {
	srv := esStub(t)
	h := NewHandler(Deps{})
	params := `{"connection_id":"prod-es","direct":{"url":"` + srv.URL + `","username":"admin","password":"pass-s3cr3t","api_key":"key-s3cr3t","token":"tok-s3cr3t"}}`
	for _, method := range []string{"connection/test", "connection/connect", "es/request"} {
		p := params
		if method == "es/request" {
			p = `{"connection_id":"prod-es","method":"GET","path":"/_cluster/health","direct":{"url":"` + srv.URL + `","password":"pass-s3cr3t","token":"tok-s3cr3t"}}`
		}
		res, perr := h.Handle(dbxpluginsdk.RequestContext{}, method, json.RawMessage(p), nil)
		if perr != nil {
			t.Fatalf("%s error: %+v", method, perr)
		}
		b, _ := json.Marshal(res)
		for _, secret := range []string{"pass-s3cr3t", "key-s3cr3t", "tok-s3cr3t"} {
			if strings.Contains(string(b), secret) {
				t.Fatalf("%s leaked %q in response: %s", method, secret, b)
			}
		}
	}
}
