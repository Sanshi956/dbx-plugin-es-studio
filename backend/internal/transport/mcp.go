package transport

import (
	"context"
	"errors"

	"github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
	"github.com/xihonglei/es-studio/backend/internal/core/es"
	"github.com/xihonglei/es-studio/backend/internal/core/permission"
	"github.com/xihonglei/es-studio/backend/internal/mcp"
)

// P5 · MCP adapter for DBX's built-in AI (docs/00-SPEC.md §2.1).
//
// These three methods are the in-process half of the MCP story; the external
// half (cmd/es-studio-mcp) speaks the MCP protocol and calls the exact same
// mcp.Caller, so there is one tool implementation, not two (requirement 6.2).

// mcpTools lists the catalogue. It never touches Elasticsearch: listing tools
// must work even before a connection exists.
func (h *Handler) mcpTools() any {
	registry := h.mcpCaller.Registry()
	tools := registry.Tools()
	descriptors := make([]map[string]any, 0, len(tools))
	catalogue := make([]map[string]any, 0, len(tools))
	categories := map[string]int{}
	for _, tool := range tools {
		descriptors = append(descriptors, tool.Descriptor())
		catalogue = append(catalogue, tool.Summary())
		categories[tool.Category]++
	}
	return map[string]any{
		"count":      len(descriptors),
		"tools":      descriptors,
		"catalogue":  catalogue,
		"categories": categories,
		"read_only":  true,
	}
}

// mcpCall executes one tool for the given connection.
func (h *Handler) mcpCall(p callParams) (any, *dbxpluginsdk.PluginError) {
	if p.Tool == "" {
		return nil, dbxpluginsdk.NewError(-32602, "tool is required")
	}
	if p.ConnectionID == "" {
		return nil, dbxpluginsdk.NewError(-32602, "connection_id is required")
	}
	cfg, client, perr := h.resolve(p)
	if perr != nil {
		return nil, perr
	}
	result, err := h.mcpCaller.Call(context.Background(), cfg, client, p.Tool, p.Arguments)
	if err != nil {
		return nil, mcpError(err)
	}
	return map[string]any{
		"tool":        result.Tool,
		"status":      result.Status,
		"text":        result.Text,
		"bytes":       result.Bytes,
		"truncated":   result.Truncated,
		"duration_ms": result.DurationMs,
		"request": map[string]any{
			"method": result.Request.Method,
			"path":   result.Request.Path,
			"query":  result.Request.Query,
		},
		"read_only": true,
	}, nil
}

// mcpLog returns recent audit rows plus the log file stats.
func (h *Handler) mcpLog(p callParams) (any, *dbxpluginsdk.PluginError) {
	if h.mcpLogger == nil {
		return map[string]any{"calls": []any{}, "count": 0, "storage": "unavailable"}, nil
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	entries, err := h.mcpLogger.Recent(limit)
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32001, err.Error())
	}
	stats, err := h.mcpLogger.Stats()
	if err != nil {
		return nil, dbxpluginsdk.NewError(-32001, err.Error())
	}
	return map[string]any{
		"calls":   entries,
		"count":   len(entries),
		"file":    stats.File,
		"samples": stats.Samples,
		"bytes":   stats.Bytes,
	}, nil
}

// mcpError maps the tool layer's own errors onto the protocol's codes so the UI
// and the AI get a stable contract instead of parsed strings.
func mcpError(err error) *dbxpluginsdk.PluginError {
	switch {
	case errors.Is(err, mcp.ErrUnknownTool):
		return dbxpluginsdk.NewError(-32602, err.Error())
	case errors.Is(err, mcp.ErrInvalidArgs):
		return dbxpluginsdk.NewError(-32602, err.Error())
	case errors.Is(err, permission.ErrReadOnlyViolation):
		return dbxpluginsdk.NewError(-32003, err.Error())
	case errors.Is(err, es.ErrTimeout):
		return dbxpluginsdk.NewError(-32010, err.Error())
	case errors.Is(err, es.ErrResponseTooLarge):
		return dbxpluginsdk.NewError(-32011, err.Error())
	case errors.Is(err, es.ErrUnsafePath):
		return dbxpluginsdk.NewError(-32004, err.Error())
	default:
		return dbxpluginsdk.NewError(-32000, err.Error())
	}
}
