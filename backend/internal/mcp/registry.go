package mcp

import (
	"fmt"
	"sort"
	"strings"
)

// Arg describes one tool argument. It is intentionally tiny: MCP clients only
// need a JSON Schema, and the executor needs enough to validate without a
// schema library.
type Arg struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // string | integer | boolean | object | array
	Description string   `json:"description"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
	Min         int      `json:"min,omitempty"`
	Max         int      `json:"max,omitempty"`
}

// Args is the decoded argument object. Callers use the typed helpers so a
// missing or mistyped value can never reach a path builder.
type Args map[string]any

// Build turns validated arguments into one ES request. It must be pure: same
// args ⇒ same request, and it may only return GET or a read-only POST.
type Build func(a Args) (Request, error)

// Request is the subset of es.Request the tool layer is allowed to produce.
// Keeping our own type (instead of es.Request) makes "no body on GET, no
// arbitrary headers" a compile-time property of the registry.
type Request struct {
	Method string
	Path   string
	Query  map[string]string
	Body   []byte
}

// Tool is one callable capability exposed to MCP clients.
type Tool struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Category    string `json:"category"`
	Description string `json:"description"`
	// ReadOnly is always true in this release (requirement 14.4: no write tools).
	ReadOnly bool  `json:"read_only"`
	Args     []Arg `json:"args"`
	Build    Build `json:"-"`
	// MaxBytes caps the rendered result for this tool (0 = package default).
	MaxBytes int `json:"-"`
}

// Registry is an immutable, ordered tool catalogue.
type Registry struct {
	tools  []Tool
	byName map[string]Tool
}

// NewRegistry validates the catalogue: unique names, es_ prefix, read-only
// declared, and every argument typed. It panics at init time rather than
// serving a half-broken tool to a client.
func NewRegistry(tools []Tool) *Registry {
	seen := map[string]bool{}
	for _, tool := range tools {
		switch {
		case tool.Name == "":
			panic("mcp: tool without name")
		case !strings.HasPrefix(tool.Name, "es_"):
			panic("mcp: tool name must start with es_: " + tool.Name)
		case seen[tool.Name]:
			panic("mcp: duplicate tool " + tool.Name)
		case !tool.ReadOnly:
			panic("mcp: first release ships read-only tools only: " + tool.Name)
		case tool.Build == nil:
			panic("mcp: tool without builder: " + tool.Name)
		}
		seen[tool.Name] = true
	}
	sorted := make([]Tool, len(tools))
	copy(sorted, tools)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	byName := make(map[string]Tool, len(sorted))
	for _, tool := range sorted {
		byName[tool.Name] = tool
	}
	return &Registry{tools: sorted, byName: byName}
}

// Tools returns a copy of the catalogue in stable (name) order.
func (r *Registry) Tools() []Tool {
	out := make([]Tool, len(r.tools))
	copy(out, r.tools)
	return out
}

// Lookup resolves a tool name.
func (r *Registry) Lookup(name string) (Tool, bool) {
	tool, ok := r.byName[name]
	return tool, ok
}

// Len reports the catalogue size (asserted in tests to stay above 40).
func (r *Registry) Len() int { return len(r.tools) }

// Schema renders the JSON Schema MCP clients expect for one tool.
func (t Tool) Schema() map[string]any {
	properties := map[string]any{}
	required := make([]string, 0, len(t.Args))
	for _, arg := range t.Args {
		prop := map[string]any{"type": arg.Type, "description": arg.Description}
		if len(arg.Enum) > 0 {
			prop["enum"] = arg.Enum
		}
		if arg.Default != nil {
			prop["default"] = arg.Default
		}
		if arg.Min != 0 {
			prop["minimum"] = arg.Min
		}
		if arg.Max != 0 {
			prop["maximum"] = arg.Max
		}
		properties[arg.Name] = prop
		if arg.Required {
			required = append(required, arg.Name)
		}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// Summary is the UI-facing view: flat, Chinese-labelled, and without the JSON
// Schema ceremony. Kept separate from Descriptor so the MCP wire format stays
// exactly what clients expect.
func (t Tool) Summary() map[string]any {
	args := make([]map[string]any, 0, len(t.Args))
	for _, arg := range t.Args {
		item := map[string]any{
			"name": arg.Name, "type": arg.Type, "description": arg.Description,
			"required": arg.Required,
		}
		if len(arg.Enum) > 0 {
			item["enum"] = arg.Enum
		}
		if arg.Default != nil {
			item["default"] = arg.Default
		}
		args = append(args, item)
	}
	return map[string]any{
		"name": t.Name, "title": t.Title, "category": t.Category,
		"description": t.Description, "read_only": t.ReadOnly, "args": args,
	}
}

// Descriptor is the MCP-facing view of a tool (tools/list entry).
func (t Tool) Descriptor() map[string]any {
	return map[string]any{
		"name":        t.Name,
		"title":       t.Title,
		"description": t.Description,
		"inputSchema": t.Schema(),
		"annotations": map[string]any{
			"title":           t.Title,
			"readOnlyHint":    true,
			"destructiveHint": false,
			"idempotentHint":  true,
			"openWorldHint":   false,
		},
	}
}

// ---------------------------------------------------------------------------
// Argument validation
// ---------------------------------------------------------------------------

// ErrInvalidArgs is wrapped by every argument problem so the adapter can map it
// to a tool error (MCP) or -32602 (Sidecar) without string matching.
var ErrInvalidArgs = errInvalidArgs{}

type errInvalidArgs struct{}

func (errInvalidArgs) Error() string { return "invalid tool arguments" }

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidArgs, fmt.Sprintf(format, a...))
}

// Normalize validates the raw argument map against the tool declaration and
// fills defaults. Unknown keys are rejected: a typo'd argument that is silently
// ignored would make the model believe it filtered something it did not.
func (t Tool) Normalize(raw map[string]any) (Args, error) {
	out := Args{}
	declared := make(map[string]Arg, len(t.Args))
	for _, arg := range t.Args {
		declared[arg.Name] = arg
	}
	for key := range raw {
		if _, ok := declared[key]; !ok {
			return nil, invalid("unknown argument %q for %s", key, t.Name)
		}
	}
	for _, arg := range t.Args {
		value, present := raw[arg.Name]
		if !present || value == nil {
			if arg.Required {
				return nil, invalid("%s requires %q", t.Name, arg.Name)
			}
			if arg.Default != nil {
				out[arg.Name] = arg.Default
			}
			continue
		}
		switch arg.Type {
		case "string":
			text, ok := value.(string)
			if !ok {
				return nil, invalid("%q must be a string", arg.Name)
			}
			text = strings.TrimSpace(text)
			if arg.Required && text == "" {
				return nil, invalid("%q must not be empty", arg.Name)
			}
			if len(arg.Enum) > 0 && text != "" && !contains(arg.Enum, text) {
				return nil, invalid("%q must be one of %s", arg.Name, strings.Join(arg.Enum, ", "))
			}
			out[arg.Name] = text
		case "integer":
			number, ok := asInt(value)
			if !ok {
				return nil, invalid("%q must be an integer", arg.Name)
			}
			if arg.Min != 0 && number < arg.Min {
				number = arg.Min
			}
			if arg.Max != 0 && number > arg.Max {
				number = arg.Max
			}
			out[arg.Name] = number
		case "boolean":
			flag, ok := value.(bool)
			if !ok {
				return nil, invalid("%q must be a boolean", arg.Name)
			}
			out[arg.Name] = flag
		case "object":
			object, ok := value.(map[string]any)
			if !ok {
				return nil, invalid("%q must be an object", arg.Name)
			}
			out[arg.Name] = object
		case "array":
			items, ok := value.([]any)
			if !ok {
				return nil, invalid("%q must be an array", arg.Name)
			}
			out[arg.Name] = items
		default:
			return nil, invalid("%q has unsupported type %q", arg.Name, arg.Type)
		}
	}
	return out, nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func asInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		if typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), true
	case float32:
		if typed != float32(int(typed)) {
			return 0, false
		}
		return int(typed), true
	default:
		return 0, false
	}
}

// Arg helpers used by builders.
func (a Args) String(name string) string {
	text, _ := a[name].(string)
	return text
}

func (a Args) Int(name string, fallback int) int {
	if number, ok := asInt(a[name]); ok {
		return number
	}
	return fallback
}

func (a Args) Bool(name string) bool {
	flag, _ := a[name].(bool)
	return flag
}

func (a Args) Object(name string) map[string]any {
	object, _ := a[name].(map[string]any)
	return object
}

func (a Args) Has(name string) bool {
	value, ok := a[name]
	return ok && value != nil && value != ""
}
