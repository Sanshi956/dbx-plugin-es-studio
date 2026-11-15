package transport

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
	"github.com/xihonglei/es-studio/backend/internal/core/connection"
	"github.com/xihonglei/es-studio/backend/internal/core/es"
)

// connection/action is the DBX host's uniform RPC for a connection-provider's
// declared actions (manifest.contributions[].actions[]). The host sends the
// action id plus a connection payload whose nesting differs from the workbench
// lifecycle payload, so values are collected from every nested object instead
// of assuming one fixed shape.
//
// Only key names and value kinds are ever written to the diagnostic log;
// secrets (password / api_key / token) never leave the Sidecar.
func (h *Handler) connectionAction(raw json.RawMessage) (any, *dbxpluginsdk.PluginError) {
	var root map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return nil, dbxpluginsdk.NewError(-32602, "invalid connection action parameters")
	}
	values := flattenValues(root)
	logActionShape(root, values)

	action := actionName(root, values)
	cfg := actionConfig(root, values)

	switch action {
	case "", "test", "connection/test", "connection_test":
		return h.testWithConfig(cfg)
	case "connect", "connection/connect", "connection_connect":
		session, err := h.manager.Connect(cfg)
		if err != nil {
			return nil, dbxpluginsdk.NewError(-32002, err.Error())
		}
		return map[string]any{
			"success":       true,
			"ok":            true,
			"connected":     true,
			"action":        action,
			"connection_id": session.ID,
			"endpoint":      session.Endpoint,
			"readonly":      session.ReadOnly,
		}, nil
	case "disconnect", "connection/disconnect", "connection_disconnect":
		removed := h.manager.Disconnect(cfg.ID)
		return map[string]any{
			"success":       true,
			"ok":            true,
			"connected":     false,
			"action":        action,
			"connection_id": cfg.ID,
			"disconnected":  removed,
		}, nil
	default:
		return nil, dbxpluginsdk.NewError(-32601, "unknown connection action: "+action)
	}
}

func actionName(root map[string]any, values map[string]any) string {
	candidates := []any{root["action"], root["actionId"], root["action_id"], values["action"], values["actionId"], values["action_id"]}
	for _, candidate := range candidates {
		switch v := candidate.(type) {
		case string:
			if v != "" {
				return strings.ToLower(strings.TrimSpace(v))
			}
		case map[string]any:
			for _, key := range []string{"id", "action", "name"} {
				if s, ok := v[key].(string); ok && s != "" {
					return strings.ToLower(strings.TrimSpace(s))
				}
			}
		}
	}
	return "test"
}

// flattenValues keeps the first scalar seen for each key while walking every
// nested object/array. DBX may put the form under connection / config / values
// / form_values, and secrets under connection_secrets / secrets.
func flattenValues(root map[string]any) map[string]any {
	out := make(map[string]any)
	var walk func(any, int)
	walk = func(node any, depth int) {
		if depth > 6 {
			return
		}
		switch v := node.(type) {
		case map[string]any:
			for key, value := range v {
				if _, exists := out[key]; !exists && isScalar(value) {
					out[key] = value
				}
				walk(value, depth+1)
			}
		case []any:
			for _, item := range v {
				walk(item, depth+1)
			}
		}
	}
	walk(root, 0)
	return out
}

func isScalar(v any) bool {
	switch v.(type) {
	case nil, map[string]any, []any:
		return false
	default:
		return true
	}
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := values[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func firstInt(values map[string]any, keys ...string) int {
	for _, key := range keys {
		switch v := values[key].(type) {
		case float64:
			if int(v) > 0 {
				return int(v)
			}
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

func firstBool(values map[string]any, fallback bool, keys ...string) bool {
	for _, key := range keys {
		switch v := values[key].(type) {
		case bool:
			return v
		case string:
			if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
				return b
			}
		}
	}
	return fallback
}

func actionConfig(root map[string]any, values map[string]any) connection.Config {
	// Read the connection id from the connection layer first: a nested
	// action object also carries an `id` (e.g. "test") which must not become
	// the session key.
	id := firstString(values, "connection_id", "connectionId")
	if id == "" {
		for _, key := range []string{"connection", "config", "values", "form_values", "form"} {
			layer, ok := root[key].(map[string]any)
			if !ok {
				continue
			}
			for _, field := range []string{"id", "connection_id", "connectionId"} {
				if s, ok := layer[field].(string); ok && s != "" {
					id = s
					break
				}
			}
			if id != "" {
				break
			}
		}
	}
	if id == "" {
		id = firstString(values, "display_name", "name")
	}
	if id == "" {
		id = "connection-action"
	}
	scheme := strings.ToLower(firstString(values, "protocol", "scheme"))
	if scheme != "https" {
		scheme = "http"
	}
	host := firstString(values, "host", "hostname")
	if host == "" {
		host = "127.0.0.1"
	}
	port := firstInt(values, "port")
	if port == 0 {
		port = 9200
	}
	timeout := firstInt(values, "query_timeout_secs", "timeout_secs", "timeout")
	tlsVerify := firstBool(values, true, "tls_verify", "verify_tls", "tlsVerify")
	readOnly := firstBool(values, false, "read_only", "readonly", "readOnly")
	authType := strings.ToLower(firstString(values, "auth_type", "authType", "authentication"))
	direct := connection.Direct{
		URL:         scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port)),
		Username:    firstString(values, "username", "user"),
		Password:    firstString(values, "password"),
		APIKey:      firstString(values, "api_key", "apiKey"),
		Token:       firstString(values, "bearer_token", "token"),
		ReadOnly:    readOnly,
		TLSVerify:   &tlsVerify,
		TimeoutSecs: timeout,
	}
	cfg := direct.Config(id)
	switch authType {
	case "apikey", "api_key", "api-key":
		if direct.APIKey == "" {
			cfg.Auth = es.Auth{Kind: es.AuthNone, Username: direct.Username}
		}
	case "bearer", "token", "bearer_token":
		if direct.Token == "" {
			cfg.Auth = es.Auth{Kind: es.AuthNone, Username: direct.Username}
		}
	}
	if firstString(values, "display_name", "name") != "" {
		cfg.Name = firstString(values, "display_name", "name")
	}
	return cfg
}

func (h *Handler) testWithConfig(cfg connection.Config) (any, *dbxpluginsdk.PluginError) {
	client, err := es.NewClient(es.Config{
		BaseURL: cfg.BaseURL(), Auth: cfg.Auth, Timeout: cfg.Timeout, TLSVerify: cfg.TLSVerify,
	})
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32002, "invalid connection target")
	}
	return h.testWith(cfg, client)
}

// shapeOf returns key names -> value kinds only. Never a real value.
func shapeOf(node any, depth int) any {
	if depth > 4 {
		return "..."
	}
	switch v := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, value := range v {
			out[key] = shapeOf(value, depth+1)
		}
		return out
	case []any:
		if len(v) == 0 {
			return "[]"
		}
		return []any{shapeOf(v[0], depth+1)}
	case nil:
		return "null"
	default:
		return fmt.Sprintf("<%T>", v)
	}
}

func logActionShape(root map[string]any, values map[string]any) {
	dir := os.Getenv(dbxpluginsdk.DataDirEnvVar)
	if dir == "" {
		dir = os.TempDir()
	}
	entry := map[string]any{
		"ts":     time.Now().UTC().Format(time.RFC3339Nano),
		"method": "connection/action",
		"shape":  shapeOf(root, 0),
		"resolved": map[string]any{
			"has_host":   values["host"] != nil || values["hostname"] != nil,
			"has_port":   values["port"] != nil,
			"has_action": root["action"] != nil || root["actionId"] != nil || root["action_id"] != nil,
		},
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := filepath.Join(dir, "es-studio-rpc-shapes.jsonl")
	if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_, _ = f.Write(append(line, '\n'))
		_ = f.Close()
	}
}
