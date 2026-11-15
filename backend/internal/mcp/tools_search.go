package mcp

import (
	"encoding/json"
	"strings"
)

// searchTools read documents. `size` is clamped here (not in the doc) so a
// client cannot ask for one million hits and blow up both the Sidecar and the
// AI context window (requirement 10.3).
const (
	defaultSearchSize = 10
	maxSearchSize     = 100
)

func searchTools() []Tool {
	return []Tool{
		{
			Name:     "es_search_documents",
			Title:    "查询文档",
			Category: "doc",
			Description: "执行 Query DSL 查询并返回命中文档（默认 10 条，最多 100 条）。对应 POST /{index}/_search。" +
				"fields 是 _source 过滤列表，留空返回完整文档。",
			ReadOnly: true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名 / 模式 / 逗号列表"},
				{Name: "query", Type: "object", Description: "ES Query DSL 的 query 部分，如 {\"match\":{\"name\":\"keyboard\"}}；留空=match_all"},
				{Name: "size", Type: "integer", Description: "返回条数（默认 10，上限 100）", Default: defaultSearchSize, Min: 1, Max: maxSearchSize},
				{Name: "sort", Type: "string", Description: "排序，如 price:desc 或 created:asc"},
				{Name: "fields", Type: "string", Description: "逗号分隔的 _source 字段白名单"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				payload := map[string]any{
					"size": a.Int("size", defaultSearchSize),
				}
				if object := a.Object("query"); len(object) > 0 {
					payload["query"] = object
				}
				if a.Has("sort") {
					sort, err := keyword(a.String("sort"), "sort")
					if err != nil {
						return Request{}, err
					}
					payload["sort"] = []string{sort}
				}
				if a.Has("fields") {
					fields := strings.Split(a.String("fields"), ",")
					for _, field := range fields {
						if _, err := keyword(field, "fields"); err != nil {
							return Request{}, err
						}
					}
					payload["_source"] = fields
				}
				encoded, err := json.Marshal(payload)
				if err != nil {
					return Request{}, invalid("query is not valid JSON")
				}
				return Request{Method: "POST", Path: "/" + index + "/_search", Body: encoded}, nil
			},
		},
		{
			Name:        "es_get_document",
			Title:       "按 id 读取文档",
			Category:    "doc",
			Description: "按 _id 读取单个文档。对应 GET /{index}/_doc/{id}。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名"},
				{Name: "id", Type: "string", Required: true, Description: "文档 _id"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				id, err := docID(a.String("id"))
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/" + index + "/_doc/" + id}, nil
			},
		},
		{
			Name:     "es_msearch",
			Title:    "批量查询",
			Category: "doc",
			Description: "一次提交多个查询（NDJSON 语义，由工具拼装）。对应 POST /_msearch。" +
				"searches 里每项是 {index, query, size}。",
			ReadOnly: true,
			Args: []Arg{
				{Name: "searches", Type: "array", Required: true, Description: "查询列表，最多 20 项"},
			},
			Build: func(a Args) (Request, error) {
				raw, _ := a["searches"].([]any)
				if len(raw) == 0 {
					return Request{}, invalid("searches must not be empty")
				}
				if len(raw) > 20 {
					return Request{}, invalid("searches accepts at most 20 items")
				}
				var payload []byte
				for _, entry := range raw {
					item, ok := entry.(map[string]any)
					if !ok {
						return Request{}, invalid("each search must be an object")
					}
					header := map[string]any{}
					if value, ok := item["index"].(string); ok && value != "" {
						index, err := name(value, "index")
						if err != nil {
							return Request{}, err
						}
						header["index"] = index
					}
					bodyObject := map[string]any{}
					if query, ok := item["query"].(map[string]any); ok && len(query) > 0 {
						bodyObject["query"] = query
					}
					size := defaultSearchSize
					if value, ok := item["size"].(float64); ok && value > 0 {
						size = int(value)
					}
					if size > maxSearchSize {
						size = maxSearchSize
					}
					bodyObject["size"] = size
					headerJSON, err := json.Marshal(header)
					if err != nil {
						return Request{}, invalid("invalid search header")
					}
					bodyJSON, err := json.Marshal(bodyObject)
					if err != nil {
						return Request{}, invalid("invalid search body")
					}
					payload = append(payload, headerJSON...)
					payload = append(payload, '\n')
					payload = append(payload, bodyJSON...)
					payload = append(payload, '\n')
				}
				return Request{Method: "POST", Path: "/_msearch", Body: payload}, nil
			},
		},
		{
			Name:     "es_analyze_text",
			Title:    "分词分析",
			Category: "doc",
			Description: "用指定分词器/字段分析文本，查看切出的词项。对应 POST /{index}/_analyze。" +
				"排查「为什么搜不到」时用它验证分词结果。",
			ReadOnly: true,
			Args: []Arg{
				{Name: "index", Type: "string", Description: "索引名（用字段分析器时必填）"},
				{Name: "text", Type: "string", Required: true, Description: "待分析文本"},
				{Name: "analyzer", Type: "string", Description: "分词器名，如 standard / ik_max_word"},
				{Name: "field", Type: "string", Description: "改用该字段的分词器（与 analyzer 二选一）"},
			},
			Build: func(a Args) (Request, error) {
				payload := map[string]any{"text": a.String("text")}
				if a.Has("analyzer") {
					analyzer, err := name(a.String("analyzer"), "analyzer")
					if err != nil {
						return Request{}, err
					}
					payload["analyzer"] = analyzer
				}
				if a.Has("field") {
					field, err := keyword(a.String("field"), "field")
					if err != nil {
						return Request{}, err
					}
					payload["field"] = field
				}
				if !a.Has("analyzer") && !a.Has("field") {
					payload["analyzer"] = "standard"
				}
				encoded, err := json.Marshal(payload)
				if err != nil {
					return Request{}, invalid("analyze payload is not valid JSON")
				}
				path := "/_analyze"
				if a.Has("index") {
					index, err := name(a.String("index"), "index")
					if err != nil {
						return Request{}, err
					}
					path = "/" + index + "/_analyze"
				}
				return Request{Method: "POST", Path: path, Body: encoded}, nil
			},
		},
	}
}
