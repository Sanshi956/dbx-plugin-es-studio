package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
	"github.com/xihonglei/es-studio/backend/internal/collector"
	"github.com/xihonglei/es-studio/backend/internal/core/connection"
	"github.com/xihonglei/es-studio/backend/internal/core/es"
	"github.com/xihonglei/es-studio/backend/internal/core/permission"
	"github.com/xihonglei/es-studio/backend/internal/storage"
)

// Deps wires the shared core engine into the transport. The same core packages
// are reused by the independent MCP server in P5 (docs/00-SPEC.md D3).
type Deps struct {
	Manager   *connection.Manager
	Collector *collector.Collector
	Store     *storage.Store
}

// Handler is the DBX stdio-jsonl Sidecar adapter (and, later, MCP adapter).
type Handler struct {
	manager   *connection.Manager
	collector *collector.Collector
	store     *storage.Store
}

// NewHandler builds the Sidecar handler; it satisfies dbxpluginsdk.Handler.
func NewHandler(d Deps) *Handler {
	manager := d.Manager
	if manager == nil {
		manager = connection.NewManager()
	}
	return &Handler{manager: manager, collector: d.Collector, store: d.Store}
}

type callParams struct {
	ConnectionID string              `json:"connection_id"`
	Method       string              `json:"method"`
	Path         string              `json:"path"`
	Query        map[string][]string `json:"query"`
	Headers      map[string][]string `json:"headers"`
	Body         json.RawMessage     `json:"body"`
	Connection   json.RawMessage     `json:"connection"` // present on DBX lifecycle payloads
	Direct       *connection.Direct  `json:"direct"`
	Requests     []callParams        `json:"requests"`
	IntervalSec  int                 `json:"interval_sec"` // mon/start
	Since        int64               `json:"since"`        // mon/history (unix ms)
	Limit        int                 `json:"limit"`        // mon/history
}

// Handle dispatches one JSON-RPC request. Responses never contain credentials:
// Config/Session keep secrets in json:"-" fields and only safe projections are
// returned here.
func (h *Handler) Handle(
	ctx dbxpluginsdk.RequestContext,
	method string,
	params json.RawMessage,
	emitter *dbxpluginsdk.Emitter,
) (any, *dbxpluginsdk.PluginError) {
	var p callParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, dbxpluginsdk.NewError(-32602, "invalid request parameters")
		}
	}
	switch method {
	case "es/ping":
		return map[string]any{"pong": true, "plugin": "local.es.studio"}, nil
	case "es/request":
		return h.esRequest(p)
	case "es/batch":
		return h.esBatch(p)
	case "es/test", "connection/test":
		return h.connectionTest(params, p)
	case "connection/connect":
		return h.connectionConnect(params, p)
	case "connection/disconnect":
		return h.connectionDisconnect(params, p)
	case "connection/action":
		return h.connectionAction(params)
	case "mon/start":
		return h.monStart(p)
	case "mon/stop":
		return h.monStop(p)
	case "mon/status":
		return h.monStatus(p)
	case "mon/sample":
		return h.monSample(p)
	case "mon/history":
		return h.monHistory(p)
	default:
		return nil, dbxpluginsdk.MethodNotFound(method)
	}
}

// resolve picks the client for a request. `direct` is a test/preview override;
// production paths always use a session created from a DBX lifecycle payload.
func (h *Handler) resolve(p callParams) (connection.Config, *es.Client, *dbxpluginsdk.PluginError) {
	if p.Direct != nil && p.Direct.URL != "" {
		cfg := p.Direct.Config(p.ConnectionID)
		client, err := es.NewClient(es.Config{
			BaseURL: cfg.BaseURL(), Auth: cfg.Auth, Timeout: cfg.Timeout, TLSVerify: cfg.TLSVerify,
		})
		if err != nil {
			return connection.Config{}, nil, dbxpluginsdk.NewError(-32002, "invalid connection target")
		}
		return cfg, client, nil
	}
	session, ok := h.manager.Get(p.ConnectionID)
	if !ok {
		return connection.Config{}, nil,
			dbxpluginsdk.NewError(-32002, "connection is not connected: "+p.ConnectionID)
	}
	return session.Config(), session.Client(), nil
}

// configFor accepts both shapes:
//   - DBX lifecycle payload ({provider, connection, runtime}) for connect/test
//   - explicit connection_id / direct override for Workbench UI and smoke tests
func (h *Handler) configFor(raw json.RawMessage, p callParams) (connection.Config, *es.Client, *dbxpluginsdk.PluginError) {
	if len(p.Connection) > 0 && string(p.Connection) != "null" {
		cfg, err := connection.ParseLifecycle(raw)
		if err != nil {
			return connection.Config{}, nil, dbxpluginsdk.NewError(-32002, "invalid lifecycle payload: "+err.Error())
		}
		client, err := es.NewClient(es.Config{
			BaseURL: cfg.BaseURL(), Auth: cfg.Auth, Timeout: cfg.Timeout, TLSVerify: cfg.TLSVerify,
		})
		if err != nil {
			return connection.Config{}, nil, dbxpluginsdk.NewError(-32002, "invalid connection target")
		}
		return cfg, client, nil
	}
	return h.resolve(p)
}

func requestBody(raw json.RawMessage) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			return []byte(text)
		}
	}
	return []byte(trimmed)
}

func safeHeaders(header http.Header) http.Header {
	out := http.Header{}
	for key, values := range header {
		lower := strings.ToLower(key)
		if lower == "authorization" || lower == "set-cookie" || lower == "www-authenticate" {
			continue
		}
		out[key] = values
	}
	return out
}

func decodeBody(body []byte) any {
	var value any
	if len(body) > 0 && json.Unmarshal(body, &value) == nil {
		return value
	}
	return string(body)
}

func (h *Handler) perform(p callParams) (any, *dbxpluginsdk.PluginError) {
	cfg, client, perr := h.resolve(p)
	if perr != nil {
		return nil, perr
	}
	method := strings.ToUpper(strings.TrimSpace(p.Method))
	if method == "" {
		method = http.MethodGet
	}
	req := es.Request{
		Method:  method,
		Path:    p.Path,
		Query:   url.Values(p.Query),
		Headers: http.Header(p.Headers),
		Body:    requestBody(p.Body),
	}
	if err := permission.Check(cfg, req); err != nil {
		code := -32000
		if errors.Is(err, permission.ErrReadOnlyViolation) {
			code = -32003
		} else if errors.Is(err, es.ErrUnsafePath) {
			code = -32004
		}
		return nil, dbxpluginsdk.NewError(code, err.Error())
	}
	resp, err := client.Do(context.Background(), req)
	if err != nil {
		switch {
		case errors.Is(err, es.ErrTimeout):
			return nil, dbxpluginsdk.NewError(-32010, err.Error())
		case errors.Is(err, es.ErrResponseTooLarge):
			return nil, dbxpluginsdk.NewError(-32011, err.Error())
		default:
			// err.Error() only contains method + path, never headers/credentials.
			return nil, dbxpluginsdk.NewError(-32000, err.Error())
		}
	}
	return map[string]any{
		"status":      resp.Status,
		"headers":     safeHeaders(resp.Headers),
		"body":        decodeBody(resp.Body),
		"duration_ms": float64(resp.Duration.Microseconds()) / 1000,
		"readonly":    cfg.ReadOnly,
	}, nil
}

func (h *Handler) esRequest(p callParams) (any, *dbxpluginsdk.PluginError) {
	return h.perform(p)
}

func (h *Handler) esBatch(p callParams) (any, *dbxpluginsdk.PluginError) {
	results := make([]any, 0, len(p.Requests))
	for _, item := range p.Requests {
		if item.ConnectionID == "" {
			item.ConnectionID = p.ConnectionID
		}
		if item.Direct == nil {
			item.Direct = p.Direct
		}
		result, perr := h.perform(item)
		if perr != nil {
			results = append(results, map[string]any{"error": perr})
			continue
		}
		results = append(results, result)
	}
	return map[string]any{"results": results}, nil
}

func (h *Handler) connectionTest(raw json.RawMessage, p callParams) (any, *dbxpluginsdk.PluginError) {
	cfg, client, perr := h.configFor(raw, p)
	if perr != nil {
		return nil, perr
	}
	return h.testWith(cfg, client)
}

// testWith is shared by connection/test and connection/action's test action.
// The extra success/ok/message fields keep the result compatible with DBX's
// connection dialog regardless of which field it inspects.
func (h *Handler) testWith(cfg connection.Config, client *es.Client) (any, *dbxpluginsdk.PluginError) {
	resp, err := client.Do(context.Background(), es.Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32001, "connection failed: "+err.Error())
	}
	var root map[string]any
	if json.Unmarshal(resp.Body, &root) != nil {
		return nil, dbxpluginsdk.NewError(-32001, "server did not return cluster metadata")
	}
	version := "unknown"
	if v, ok := root["version"].(map[string]any); ok {
		if n, ok := v["number"].(string); ok {
			version = n
		}
	}
	engine := string(es.DetectEngine(root))
	connected := resp.Status >= 200 && resp.Status < 300
	result := map[string]any{
		"success":      connected,
		"ok":           connected,
		"connected":    connected,
		"status":       resp.Status,
		"version":      version,
		"engine":       engine,
		"cluster_name": root["cluster_name"],
		"cluster_uuid": root["cluster_uuid"],
		"node_name":    root["name"],
		"endpoint":     cfg.BaseURL(),
		"readonly":     cfg.ReadOnly,
		"message":      fmt.Sprintf("ES %s at %s", version, cfg.BaseURL()),
	}
	if parsed, err := es.ParseVersion(version); err == nil {
		if caps, err := es.Capabilities(parsed); err == nil {
			caps.Engine = es.Engine(engine)
			result["capabilities"] = caps
		}
	}
	return result, nil
}

func (h *Handler) connectionConnect(raw json.RawMessage, p callParams) (any, *dbxpluginsdk.PluginError) {
	cfg, _, perr := h.configFor(raw, p)
	if perr != nil {
		return nil, perr
	}
	session, err := h.manager.Connect(cfg)
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32002, err.Error())
	}
	return map[string]any{
		"connection_id": session.ID,
		"connected":     true,
		"endpoint":      session.Endpoint,
		"readonly":      session.ReadOnly,
	}, nil
}

func (h *Handler) connectionDisconnect(raw json.RawMessage, p callParams) (any, *dbxpluginsdk.PluginError) {
	id := p.ConnectionID
	if len(p.Connection) > 0 && string(p.Connection) != "null" {
		if cfg, err := connection.ParseLifecycle(raw); err == nil && cfg.ID != "" {
			id = cfg.ID
		}
	}
	removed := h.manager.Disconnect(id)
	return map[string]any{
		"connection_id": id,
		"disconnected":  removed,
		"connected":     false,
	}, nil
}

// compile-time interface assertion
var _ dbxpluginsdk.Handler = (*Handler)(nil)
