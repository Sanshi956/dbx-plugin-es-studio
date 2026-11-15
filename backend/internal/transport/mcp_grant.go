package transport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
	"github.com/xihonglei/es-studio/backend/internal/mcp"
)

// P5 · authorisation for the independent MCP server (SPEC §5.2).
//
// The Sidecar is the only process that ever holds credentials. When the user
// authorises a connection, we seal {endpoint, auth} for the *current OS user*
// and hand back an opaque token. The MCP server reads nothing from DBX: it only
// opens its own sealed grant (see internal/mcp/grant.go).

// mcpGrants lists existing grants without secrets.
func (h *Handler) mcpGrants() any {
	grants, err := h.grants.List()
	if err != nil {
		return map[string]any{"grants": []any{}, "count": 0, "error": err.Error()}
	}
	now := time.Now()
	out := make([]mcp.Public, 0, len(grants))
	for _, grant := range grants {
		out = append(out, grant.Public(now))
	}
	return map[string]any{
		"grants": out,
		"count":  len(out),
		"file":   filepath.Base(h.grants.Path()),
		"dir":    filepath.Dir(h.grants.Path()),
	}
}

// mcpGrant authorises one *currently connected* connection and returns a fresh
// token once. The token is never stored in clear text by the plugin, so it can
// not be shown again — re-authorising rotates it.
func (h *Handler) mcpGrant(p callParams) (any, *dbxpluginsdk.PluginError) {
	if p.ConnectionID == "" {
		return nil, dbxpluginsdk.NewError(-32602, "connection_id is required")
	}
	// resolve() accepts the same shapes as every other method: a real session
	// from the DBX lifecycle payload, or the `direct` override the preview
	// bridge and smoke tests use. Nothing else can name an endpoint.
	cfg, _, perr := h.resolve(p)
	if perr != nil {
		return nil, perr
	}
	token, err := mcp.NewToken()
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32000, "cannot generate token: "+err.Error())
	}
	now := time.Now()
	ttl := mcp.TokenTTL
	if p.TTLDays > 0 && p.TTLDays <= 365 {
		ttl = time.Duration(p.TTLDays) * 24 * time.Hour
	}
	grant := mcp.Grant{
		Token:        token,
		ConnectionID: cfg.ID,
		Label:        cfg.Name,
		Scheme:       cfg.Scheme,
		Host:         cfg.Host,
		Port:         cfg.Port,
		Auth:         mcp.AuthFromES(cfg.Auth),
		TLSVerify:    cfg.TLSVerify,
		TimeoutSecs:  int(cfg.Timeout.Seconds()),
		ReadOnly:     true,
		CreatedAt:    now.UnixMilli(),
		ExpiresAt:    now.Add(ttl).UnixMilli(),
	}
	if err := h.grants.Put(grant); err != nil {
		return nil, dbxpluginsdk.NewError(-32000, "cannot write grant: "+err.Error())
	}
	command, args := mcpClientCommand(h)
	response := map[string]any{
		"token":      token,
		"public":     grant.Public(now),
		"expires_at": grant.ExpiresAt,
		"command":    command,
		"args":       args,
		"env":        map[string]string{"ES_STUDIO_MCP_TOKEN": token},
		"read_only":  true,
	}
	response["snippet"] = snippet(command, args, token)
	return response, nil
}

// mcpRevoke removes the grant for one connection; the next tool call from that
// client fails immediately.
func (h *Handler) mcpRevoke(p callParams) (any, *dbxpluginsdk.PluginError) {
	if p.ConnectionID == "" {
		return nil, dbxpluginsdk.NewError(-32602, "connection_id is required")
	}
	removed, err := h.grants.Revoke(p.ConnectionID)
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32000, err.Error())
	}
	return map[string]any{"connection_id": p.ConnectionID, "revoked": removed}, nil
}

// mcpClientCommand is what the user pastes into their MCP client. The packaged
// plugin only ships one executable, and it doubles as the MCP server via --mcp,
// so there is no second binary to keep in sync with the plugin version.
func mcpClientCommand(h *Handler) (string, []string) {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "es-studio-sidecar"
	}
	return exe, []string{"--mcp", "--data-dir", filepath.Dir(h.grants.Path())}
}

func snippet(command string, args []string, token string) string {
	payload := map[string]any{
		"mcpServers": map[string]any{
			"es-studio": map[string]any{
				"command": command,
				"args":    args,
				"env":     map[string]string{"ES_STUDIO_MCP_TOKEN": token},
			},
		},
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ""
	}
	return string(encoded)
}
