package mcp

import "strings"

// indexTools answer "what data exists and how is it laid out". Index arguments
// accept patterns (`logs-*`) and comma lists, matching ES semantics.
func indexTools() []Tool {
	return []Tool{
		{
			Name:     "es_list_indices",
			Title:    "索引列表",
			Category: "index",
			Description: "列出索引：健康、状态(open/close)、主/副本分片数、文档数、存储大小。" +
				"对应 GET /_cat/indices?format=json。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return catPath("indices",
					"health,status,index,pri,rep,docs.count,store.size,pri.store.size", nil), nil
			},
		},
		{
			Name:        "es_get_index",
			Title:       "索引详情",
			Category:    "index",
			Description: "读取索引的别名、映射、设置汇总。对应 GET /{index}。index 支持逗号列表与通配符。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名 / 模式 / 逗号列表"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/" + index}, nil
			},
		},
		{
			Name:        "es_get_index_mapping",
			Title:       "索引映射",
			Category:    "index",
			Description: "读取字段映射（类型、分词器、子字段）。对应 GET /{index}/_mapping。查字段名必须先看它。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名 / 模式 / 逗号列表"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/" + index + "/_mapping"}, nil
			},
		},
		{
			Name:        "es_get_index_settings",
			Title:       "索引设置",
			Category:    "index",
			Description: "读取索引设置（扁平键名，如 index.number_of_replicas）。对应 GET /{index}/_settings?flat_settings=true。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名 / 模式 / 逗号列表"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/" + index + "/_settings",
					Query: map[string]string{"flat_settings": "true"}}, nil
			},
		},
		{
			Name:        "es_get_index_stats",
			Title:       "索引统计",
			Category:    "index",
			Description: "读取文档数、存储、段、索引/查询速率等统计。对应 GET /{index}/_stats。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名 / 模式 / 逗号列表"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/" + index + "/_stats"}, nil
			},
		},
		{
			Name:        "es_get_index_recovery",
			Title:       "索引恢复状态",
			Category:    "index",
			Description: "读取分片恢复进度（RECOVERING 分片卡住时看它）。对应 GET /{index}/_recovery。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名 / 模式 / 逗号列表"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/" + index + "/_recovery"}, nil
			},
		},
		{
			Name:        "es_get_index_segments",
			Title:       "索引段信息",
			Category:    "index",
			Description: "读取 Lucene 段信息（段数暴涨通常意味着需要 force merge）。对应 GET /{index}/_segments。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名 / 模式 / 逗号列表"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/" + index + "/_segments"}, nil
			},
		},
		{
			Name:     "es_list_aliases",
			Title:    "别名列表",
			Category: "index",
			Description: "列出全部别名及其索引、过滤条件、routing、写入索引、隐藏标记。" +
				"对应 GET /_alias（比 _cat/aliases 多 is_hidden 与 filter 对象）。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_alias"}, nil
			},
		},
		{
			Name:        "es_get_index_aliases",
			Title:       "索引别名",
			Category:    "index",
			Description: "读取指定索引上的别名。对应 GET /{index}/_alias。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名 / 模式 / 逗号列表"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/" + index + "/_alias"}, nil
			},
		},
		{
			Name:     "es_list_shards",
			Title:    "分片列表",
			Category: "index",
			Description: "列出分片：索引、编号、主/副本、状态、未分配原因、文档数、存储、所在节点。" +
				"对应 GET /_cat/shards?format=json（显式带上 unassigned.reason）。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return catPath("shards",
					"index,shard,prirep,state,unassigned.reason,docs,store,node", nil), nil
			},
		},
		{
			Name:        "es_get_field_caps",
			Title:       "字段能力",
			Category:    "index",
			Description: "列出字段是否可搜索/可聚合及其类型，跨索引一次查清。对应 GET /{index}/_field_caps?fields=*。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名 / 模式 / 逗号列表"},
				{Name: "fields", Type: "string", Description: "字段模式，默认 *"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				fields := "*"
				if a.Has("fields") {
					if fields, err = name(a.String("fields"), "fields"); err != nil {
						return Request{}, err
					}
				}
				return Request{Method: "GET", Path: "/" + index + "/_field_caps",
					Query: map[string]string{"fields": fields}}, nil
			},
		},
		{
			Name:        "es_count_documents",
			Title:       "文档计数",
			Category:    "index",
			Description: "按查询统计命中数（不返回文档）。对应 POST /{index}/_count。query 留空则统计全部。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Required: true, Description: "索引名 / 模式 / 逗号列表"},
				{Name: "query", Type: "object", Description: "ES Query DSL 的 query 部分，如 {\"term\":{\"env\":\"prod\"}}"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				object := a.Object("query")
				var payload []byte
				if len(object) > 0 {
					payload, err = body(Args{"query": object}, "query")
					if err != nil {
						return Request{}, err
					}
				}
				return Request{Method: "POST", Path: "/" + index + "/_count", Body: payload}, nil
			},
		},
		{
			Name:        "es_get_all_mappings",
			Title:       "全部映射",
			Category:    "index",
			Description: "一次性读取所有索引的映射。对应 GET /_mapping。索引很多时响应很大，优先用 es_get_index_mapping。",
			ReadOnly:    true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_mapping"}, nil
			},
		},
		{
			Name:     "es_get_all_settings",
			Title:    "全部索引设置",
			Category: "index",
			Description: "读取所有索引的设置，可用 keys 只取关心的项。对应 GET /_all/_settings?flat_settings=true。" +
				"注意 keys 必须是扁平键名（index.number_of_replicas），不能用 filter_path（点号会被当路径）。",
			ReadOnly: true,
			Args: []Arg{
				{Name: "keys", Type: "string", Description: "逗号分隔的扁平设置键，如 index.number_of_replicas,index.refresh_interval"},
			},
			Build: func(a Args) (Request, error) {
				query := map[string]string{"flat_settings": "true"}
				if a.Has("keys") {
					keys := strings.TrimSpace(a.String("keys"))
					for _, key := range strings.Split(keys, ",") {
						if _, err := keyword(key, "keys"); err != nil {
							return Request{}, err
						}
					}
					query["name"] = keys
				}
				return Request{Method: "GET", Path: "/_all/_settings", Query: query}, nil
			},
		},
	}
}
