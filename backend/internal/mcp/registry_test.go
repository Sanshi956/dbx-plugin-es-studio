package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogueIsLargeAndReadOnly(t *testing.T) {
	registry := Default()
	if registry.Len() < 40 {
		t.Fatalf("requirement 6.1 asks for 40+ tools, got %d", registry.Len())
	}
	for _, tool := range registry.Tools() {
		if !tool.ReadOnly {
			t.Errorf("%s is not read-only; first release must ship no write tools", tool.Name)
		}
		if !strings.HasPrefix(tool.Name, "es_") {
			t.Errorf("%s must be prefixed with es_", tool.Name)
		}
		if tool.Description == "" || tool.Title == "" || tool.Category == "" {
			t.Errorf("%s is missing title/description/category", tool.Name)
		}
		descriptor := tool.Descriptor()
		annotations, ok := descriptor["annotations"].(map[string]any)
		if !ok || annotations["readOnlyHint"] != true {
			t.Errorf("%s must advertise readOnlyHint=true", tool.Name)
		}
		schema, ok := descriptor["inputSchema"].(map[string]any)
		if !ok || schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("%s schema must be a closed object", tool.Name)
		}
	}
}

func TestEveryToolBuildsAReadOnlyRequest(t *testing.T) {
	// A sample value per declared argument type, so every builder actually runs.
	samples := map[string]any{
		"string":  "products",
		"integer": 1,
		"boolean": false,
		"object":  map[string]any{"term": map[string]any{"env": "prod"}},
		"array":   []any{map[string]any{"index": "products", "query": map[string]any{"match_all": map[string]any{}}}},
	}
	fixes := map[string]map[string]any{
		"es_cat":                    {"api": "indices"},
		"es_get_task":               {"task_id": "node-1:42"},
		"es_call_readonly_api":      {"path": "/_cluster/health", "query": map[string]any{"level": "indices"}},
		"es_search_documents":       {"index": "products"},
		"es_get_document":           {"index": "products", "id": "1"},
		"es_explain_allocation":     {"index": "products", "shard": 0},
		"es_analyze_text":           {"text": "hello world"},
		"es_get_ilm_policy":         {"name": "policy-1"},
		"es_get_data_stream":        {"name": "logs"},
		"es_get_index_template":     {"name": "tpl"},
		"es_get_component_template": {"name": "tpl"},
		"es_list_snapshots":         {"repository": "repo"},
		"es_get_snapshot":           {"repository": "repo", "snapshot": "snap"},
		"es_count_documents":        {"index": "products"},
	}
	for _, tool := range Default().Tools() {
		raw := map[string]any{}
		for _, arg := range tool.Args {
			if sample, ok := fixes[tool.Name][arg.Name]; ok {
				raw[arg.Name] = sample
				continue
			}
			if len(arg.Enum) > 0 {
				raw[arg.Name] = arg.Enum[0]
				continue
			}
			raw[arg.Name] = samples[arg.Type]
		}
		args, err := tool.Normalize(raw)
		if err != nil {
			t.Fatalf("%s: normalize rejects its own arguments: %v", tool.Name, err)
		}
		request, err := tool.Build(args)
		if err != nil {
			t.Fatalf("%s: build failed: %v", tool.Name, err)
		}
		if request.Method != "GET" && request.Method != "POST" && request.Method != "HEAD" {
			t.Errorf("%s uses method %s", tool.Name, request.Method)
		}
		if !strings.HasPrefix(request.Path, "/") || strings.Contains(request.Path, "..") ||
			strings.Contains(request.Path, "://") {
			t.Errorf("%s built an unsafe path %q", tool.Name, request.Path)
		}
		if request.Method == "GET" && len(request.Body) > 0 {
			t.Errorf("%s sends a body on GET", tool.Name)
		}
	}
}

func TestNormalizeRejectsTyposAndFillsDefaults(t *testing.T) {
	tool, ok := Default().Lookup("es_search_documents")
	if !ok {
		t.Fatal("es_search_documents missing")
	}
	if _, err := tool.Normalize(map[string]any{"index": "products", "sizes": 5}); err == nil {
		t.Fatal("a typo'd argument must be rejected, not silently ignored")
	}
	if _, err := tool.Normalize(map[string]any{}); err == nil {
		t.Fatal("missing required index must be rejected")
	}
	args, err := tool.Normalize(map[string]any{"index": "products"})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if args.Int("size", 0) != defaultSearchSize {
		t.Fatalf("size default = %d, want %d", args.Int("size", 0), defaultSearchSize)
	}
	// Size is clamped, not rejected: a client asking for 10000 gets 100.
	args, err = tool.Normalize(map[string]any{"index": "products", "size": 10000})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if args.Int("size", 0) != maxSearchSize {
		t.Fatalf("size = %d, want clamped %d", args.Int("size", 0), maxSearchSize)
	}
}

func TestNormalizeRejectsPathTraversalInIndexName(t *testing.T) {
	tool, _ := Default().Lookup("es_get_index_mapping")
	for _, bad := range []string{"../secret", "logs/../../_cat", "a?b=1", "a#b", "a b"} {
		if _, err := tool.Normalize(map[string]any{"index": bad}); err != nil {
			continue
		}
		args, _ := tool.Normalize(map[string]any{"index": bad})
		if _, err := tool.Build(args); err == nil {
			t.Errorf("index %q should be rejected at build time", bad)
		}
	}
}

func TestSchemaMarksRequiredArgs(t *testing.T) {
	tool, _ := Default().Lookup("es_get_index_mapping")
	schema := tool.Schema()
	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "index" {
		t.Fatalf("required = %v", schema["required"])
	}
	properties := schema["properties"].(map[string]any)
	prop := properties["index"].(map[string]any)
	if prop["type"] != "string" || prop["description"] == "" {
		t.Fatalf("index property = %v", prop)
	}
}

func TestDescriptorsAreJSONSerialisable(t *testing.T) {
	for _, tool := range Default().Tools() {
		if _, err := json.Marshal(tool.Descriptor()); err != nil {
			t.Fatalf("%s descriptor is not JSON serialisable: %v", tool.Name, err)
		}
	}
}
