// Package mcpserver is the MCP (Model Context Protocol) stdio adapter: the
// second transport over the same tool layer (docs/00-SPEC.md §2.1).
//
// It speaks newline-delimited JSON-RPC 2.0, which is what MCP clients write to a
// stdio server's stdin, and implements exactly the subset an MCP client needs:
// initialize, tools/list, tools/call, ping. Every tool call goes through
// mcp.Caller, so permissions, size limits and the audit log are identical to the
// DBX built-in AI path.
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xihonglei/es-studio/backend/internal/core/connection"
	"github.com/xihonglei/es-studio/backend/internal/core/es"
	"github.com/xihonglei/es-studio/backend/internal/mcp"
)

// Protocol versions this server understands, newest first. When a client asks
// for one of these we echo it; otherwise we answer with the newest, which is
// what the MCP spec asks for.
var supportedVersions = []string{"2024-11-05", "2024-10-07"}

// Server serves one authorised connection.
type Server struct {
	Caller  *mcp.Caller
	Grant   mcp.Grant
	Version string
	In      io.Reader
	Out     io.Writer
	// Logf receives diagnostics; stdout is owned by the protocol.
	Logf func(format string, a ...any)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads requests until stdin closes.
func (s *Server) Serve() error {
	if s.In == nil || s.Out == nil {
		return errors.New("mcpserver: reader and writer are required")
	}
	conn := s.Grant.Config()
	client, err := es.NewClient(es.Config{
		BaseURL:   conn.BaseURL(),
		Auth:      s.Grant.ESAuth(),
		Timeout:   conn.Timeout,
		TLSVerify: s.Grant.TLSVerify,
	})
	if err != nil {
		return fmt.Errorf("mcpserver: invalid endpoint in grant: %w", err)
	}

	scanner := bufio.NewScanner(s.In)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		response := s.handleLine(context.Background(), conn, client, []byte(line))
		if response != nil {
			if _, err := s.Out.Write(append(response, '\n')); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// handleLine returns the encoded response, or nil for notifications.
func (s *Server) handleLine(ctx context.Context, conn connection.Config, client *es.Client, line []byte) []byte {
	var request rpcRequest
	if err := json.Unmarshal(line, &request); err != nil {
		return encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
	}
	// Notifications carry no id and must never be answered.
	notification := len(request.ID) == 0

	switch request.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(request.Params, &params)
		return encode(rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{
			"protocolVersion": negotiate(params.ProtocolVersion),
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":    "es-studio-mcp",
				"version": s.Version,
			},
			"instructions": s.instructions(),
		}})
	case "notifications/initialized", "notifications/cancelled", "initialized":
		if s.Logf != nil {
			s.Logf("client notification: %s", request.Method)
		}
		return nil
	case "ping":
		return encode(rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{}})
	case "tools/list":
		tools := make([]map[string]any, 0, s.Caller.Registry().Len())
		for _, tool := range s.Caller.Registry().Tools() {
			tools = append(tools, tool.Descriptor())
		}
		return encode(rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{"tools": tools}})
	case "tools/call":
		return s.callTool(ctx, conn, client, request)
	default:
		if notification {
			return nil
		}
		return encode(rpcResponse{JSONRPC: "2.0", ID: request.ID,
			Error: &rpcError{Code: -32601, Message: "method not found: " + request.Method}})
	}
}

func (s *Server) callTool(ctx context.Context, conn connection.Config, client *es.Client, request rpcRequest) []byte {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil || params.Name == "" {
		return encode(rpcResponse{JSONRPC: "2.0", ID: request.ID,
			Error: &rpcError{Code: -32602, Message: "tools/call requires a tool name"}})
	}
	result, err := s.Caller.Call(ctx, conn, client, params.Name, params.Arguments)
	if err != nil {
		// MCP convention: a failed *tool* is a tool result with isError, not a
		// protocol error, so the model can read why and try something else.
		return encode(rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
		}})
	}
	content := []map[string]any{{"type": "text", "text": result.Text}}
	return encode(rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{
		"isError": result.Status >= 400,
		"content": content,
		"structuredContent": map[string]any{
			"tool":        result.Tool,
			"status":      result.Status,
			"bytes":       result.Bytes,
			"truncated":   result.Truncated,
			"duration_ms": result.DurationMs,
			"request":     result.Request.Describe(),
		},
	}})
}

func (s *Server) instructions() string {
	return fmt.Sprintf(
		"You are connected to Elasticsearch cluster %q at %s through a read-only ES Studio bridge. "+
			"Every tool is read-only: there are no write tools, and any attempt to write is refused. "+
			"Start with es_get_cluster_health, then drill into indices and shards. "+
			"Large responses are truncated with an explicit marker.",
		s.Grant.Label, s.Grant.Host)
}

func negotiate(requested string) string {
	for _, version := range supportedVersions {
		if version == requested {
			return version
		}
	}
	return supportedVersions[0]
}

func encode(response rpcResponse) []byte {
	response.JSONRPC = "2.0"
	encoded, err := json.Marshal(response)
	if err != nil {
		fallback, _ := json.Marshal(rpcResponse{JSONRPC: "2.0",
			Error: &rpcError{Code: -32603, Message: "internal error"}})
		return fallback
	}
	return encoded
}
