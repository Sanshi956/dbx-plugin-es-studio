package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/core/connection"
	"github.com/xihonglei/es-studio/backend/internal/core/es"
	"github.com/xihonglei/es-studio/backend/internal/core/permission"
)

// Defaults for every call, independent of the client.
const (
	// DefaultMaxText caps the JSON handed to the model (~128 KiB). Larger
	// responses are truncated with an explicit marker instead of silently
	// dropped: the model must know it is not seeing everything.
	DefaultMaxText = 128 << 10
	// MaxTextCeiling is the hard ceiling a caller may raise DefaultMaxText to.
	MaxTextCeiling = 1 << 20
	// DefaultTimeout bounds one tool call.
	DefaultTimeout = 30 * time.Second
)

// Result is one executed tool call, in a transport-neutral shape: the Sidecar
// renders it as JSON, the MCP server renders it as content blocks.
type Result struct {
	Tool       string        `json:"tool"`
	Status     int           `json:"status"`
	Duration   time.Duration `json:"-"`
	DurationMs int           `json:"duration_ms"`
	Text       string        `json:"text"`
	Body       []byte        `json:"-"`
	Bytes      int           `json:"bytes"`
	Truncated  bool          `json:"truncated"`
	Error      string        `json:"error,omitempty"`
	Request    Request       `json:"request"`
}

// ErrUnknownTool is returned for an unknown name so adapters can map it to the
// protocol's own error without matching strings.
var ErrUnknownTool = errors.New("unknown tool")

// Caller executes tools. It owns the read-only enforcement, the timeout and the
// audit log; adapters only translate JSON.
type Caller struct {
	registry *Registry
	logger   *Logger
	maxText  int
	timeout  time.Duration
}

// NewCaller wires the catalogue; logger may be nil (tests).
func NewCaller(registry *Registry, logger *Logger) *Caller {
	if registry == nil {
		registry = Default()
	}
	return &Caller{registry: registry, logger: logger, maxText: DefaultMaxText, timeout: DefaultTimeout}
}

// Registry exposes the catalogue to adapters (tools/list).
func (c *Caller) Registry() *Registry { return c.registry }

// Call validates, executes and logs one tool invocation.
//
// Security order matters: arguments are validated BEFORE a path exists, the
// resulting request is then re-checked against the permission layer in
// read-only mode. A tool can therefore never reach ES with a write method, even
// if its builder is wrong (requirement 10.4 / 14.4).
func (c *Caller) Call(
	ctx context.Context,
	conn connection.Config,
	client *es.Client,
	toolName string,
	rawArgs map[string]any,
) (Result, error) {
	started := time.Now()
	tool, ok := c.registry.Lookup(toolName)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrUnknownTool, toolName)
		return Result{Tool: toolName}, c.reject(toolName, conn, rawArgs, err, started)
	}
	args, err := tool.Normalize(rawArgs)
	if err != nil {
		return Result{Tool: toolName}, c.reject(toolName, conn, rawArgs, err, started)
	}
	built, err := tool.Build(args)
	if err != nil {
		return Result{Tool: toolName}, c.reject(toolName, conn, rawArgs, err, started)
	}

	// Same allow-list the UI write path uses: GET/HEAD plus a fixed set of
	// read-only POSTs (_search/_count/_analyze/...).
	if err := permission.Check(connection.Config{ReadOnly: true},
		es.Request{Method: built.Method, Path: built.Path}); err != nil {
		return Result{Tool: toolName}, c.reject(toolName, conn, rawArgs, err, started)
	}

	query := url.Values{}
	for key, value := range built.Query {
		query.Set(key, value)
	}
	request := es.Request{Method: built.Method, Path: built.Path, Query: query, Body: built.Body}

	timeout := c.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	response, err := client.Do(callCtx, request)
	if err != nil {
		c.record(LogEntry{
			Tool: toolName, Connection: conn.ID, OK: false, Error: err.Error(),
			DurationMs: int(time.Since(started).Milliseconds()), Args: argKeys(rawArgs),
		})
		return Result{Tool: toolName, Request: built, Error: err.Error()}, err
	}

	text, truncated := render(response.Body, c.maxText)
	result := Result{
		Tool:       toolName,
		Status:     response.Status,
		Duration:   response.Duration,
		DurationMs: int(response.Duration.Milliseconds()),
		Text:       text,
		Body:       response.Body,
		Bytes:      len(response.Body),
		Truncated:  truncated,
		Request:    built,
	}
	c.record(LogEntry{
		Tool: toolName, Connection: conn.ID, OK: response.Status < 400, Status: response.Status,
		DurationMs: result.DurationMs, Bytes: len(response.Body), Truncated: truncated,
		Args: argKeys(rawArgs),
	})
	return result, nil
}

// reject audits a call that never reached Elasticsearch. "The model tried to
// write" is exactly the row an operator wants to find later, so refusals are
// logged like failures, not dropped on the floor.
func (c *Caller) reject(toolName string, conn connection.Config, raw map[string]any, err error, started time.Time) error {
	c.record(LogEntry{
		Tool: toolName, Connection: conn.ID, OK: false, Error: err.Error(),
		DurationMs: int(time.Since(started).Milliseconds()), Args: argKeys(raw),
	})
	return err
}

func (c *Caller) record(entry LogEntry) {
	if c.logger == nil {
		return
	}
	c.logger.Append(entry)
}

// render keeps JSON as JSON when it fits, and degrades to a truncated string
// with an explicit marker when it does not.
func render(body []byte, max int) (string, bool) {
	if max <= 0 {
		max = DefaultMaxText
	}
	if len(body) == 0 {
		return "", false
	}
	if len(body) <= max {
		var value any
		if json.Unmarshal(body, &value) == nil {
			if pretty, err := json.MarshalIndent(value, "", "  "); err == nil && len(pretty) >= 0 {
				return string(pretty), false
			}
		}
		return string(body), false
	}
	cut := body[:max]
	var value any
	if json.Unmarshal(cut, &value) == nil {
		if pretty, err := json.MarshalIndent(value, "", "  "); err == nil {
			return string(pretty) + "\n… truncated", true
		}
	}
	return string(cut) + fmt.Sprintf("\n… truncated at %d of %d bytes", max, len(body)), true
}

func argKeys(raw map[string]any) []string {
	if len(raw) == 0 {
		return nil
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Describe renders a one-line request summary for logs / UI. It never contains
// credentials: only the method and the relative path.
func (r Request) Describe() string {
	out := strings.ToUpper(r.Method) + " " + r.Path
	if len(r.Query) > 0 {
		keys := make([]string, 0, len(r.Query))
		for key := range r.Query {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+r.Query[key])
		}
		out += "?" + strings.Join(parts, "&")
	}
	return out
}
